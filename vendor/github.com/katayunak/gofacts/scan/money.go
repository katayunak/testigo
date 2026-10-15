package scan

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/naming"
)

func (p *Program) moneyFindings() []fact.Finding {
	var out []fact.Finding
	for _, pkg := range p.prod {
		ft := p.funcTable(pkg)
		for _, f := range pkg.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				switch t := n.(type) {
				case *ast.TypeSpec:
					if st, ok := t.Type.(*ast.StructType); ok {
						out = append(out, p.checkStruct(pkg, t.Name.Name, st)...)
					}
				case *ast.FuncDecl:
					out = append(out, p.checkSignature(pkg, t)...)
				case *ast.CallExpr:
					if f, ok := p.floatConversion(pkg, ft, t); ok {
						out = append(out, f)
					}
				case *ast.BinaryExpr:
					if f, ok := p.moneyDivision(pkg, ft, t); ok {
						out = append(out, f)
					}
				}
				return true
			})
		}
	}
	return out
}

func (p *Program) floatConversion(pkg *packages.Package, ft funcTable, call *ast.CallExpr) (fact.Finding, bool) {
	if len(call.Args) != 1 {
		return fact.Finding{}, false
	}
	fun, ok := pkg.TypesInfo.Types[call.Fun]
	if !ok || !fun.IsType() || !isFloat(fun.Type) {
		return fact.Finding{}, false
	}
	arg, ok := pkg.TypesInfo.Types[call.Args[0]]
	if !ok || !isInteger(arg.Type) || isBasic(arg.Type) || !isMoneyExpr(pkg, call.Args[0]) {
		return fact.Finding{}, false
	}
	return fact.Finding{
		ID: "MONEY-FLOAT", Severity: fact.SevCritical, Basis: fact.Heuristic,
		Title:  "money converted to a float mid-expression",
		Detail: "An exact integer amount is widened to a float here and will be narrowed back. Binary floating point cannot hold every decimal value, and beyond 2^53 it cannot hold every integer either, so the round trip can return a different number than it was given. Do the arithmetic on the integer type, and negate or compare without leaving it.",
		Ref:    ft.at(p, pkg, call.Pos()), Line: p.line(call.Pos()),
	}, true
}

func (p *Program) moneyDivision(pkg *packages.Package, ft funcTable, b *ast.BinaryExpr) (fact.Finding, bool) {
	if b.Op != token.QUO {
		return fact.Finding{}, false
	}
	tv, ok := pkg.TypesInfo.Types[b.X]
	if !ok || !isInteger(tv.Type) || !isMoneyExpr(pkg, b.X) {
		return fact.Finding{}, false
	}
	return fact.Finding{
		ID: "MONEY-DIV", Severity: fact.SevHigh, Basis: fact.Heuristic,
		Title:  "money divided with no stated rounding rule",
		Detail: "Integer division truncates toward zero. Splitting a charge, prorating or computing a percentage fee this way silently loses the remainder, and the lost minor units show up as a ledger that will not balance. Decide the rounding direction explicitly and give the remainder an owner.",
		Ref:    ft.at(p, pkg, b.Pos()), Line: p.line(b.Pos()),
	}, true
}

func isMoneyExpr(pkg *packages.Package, e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return naming.IsMoneyName(x.Name)
	case *ast.SelectorExpr:
		if !naming.IsMoneyName(x.Sel.Name) {
			return false
		}
		if tv, ok := pkg.TypesInfo.Types[x.X]; ok {
			if n := namedOf(tv.Type); n != nil && naming.IsContainerName(n.Obj().Name()) {
				return false
			}
		}
		return true
	case *ast.ParenExpr:
		return isMoneyExpr(pkg, x.X)
	}
	if tv, ok := pkg.TypesInfo.Types[e]; ok {
		if n, isNamed := types.Unalias(tv.Type).(*types.Named); isNamed {
			return naming.IsMoneyType(n.Obj().Name())
		}
	}
	return false
}

func (p *Program) checkStruct(pkg *packages.Package, typeName string, st *ast.StructType) []fact.Finding {
	if naming.IsContainerName(typeName) {
		return nil
	}
	hasCurrency := false
	for _, fld := range st.Fields.List {
		for _, nm := range fld.Names {
			if naming.IsCurrencyName(nm.Name) {
				hasCurrency = true
			}
		}
	}
	var out []fact.Finding
	for _, fld := range st.Fields.List {
		tv, ok := pkg.TypesInfo.Types[fld.Type]
		if !ok || isDuration(tv.Type) {
			continue
		}
		for _, nm := range fld.Names {
			if !naming.IsMoneyName(nm.Name) {
				continue
			}
			ref := fact.CodeRef{Pkg: pkg.PkgPath, Symbol: "type " + typeName, File: p.rel(fld.Pos()), Line: p.line(fld.Pos())}
			switch {
			case isFloat(tv.Type) && naming.IsMoneyAmountName(nm.Name):
				out = append(out, fact.Finding{
					ID: "MONEY-FLOAT", Severity: fact.SevCritical, Basis: fact.Heuristic,
					Title:  typeName + "." + nm.Name + " stores money in a float",
					Detail: "Binary floating point cannot represent 0.10 exactly. Summing many line items drifts, comparisons that should be equal are not, and the error is invisible in any test that checks a single transaction. Store minor units in an integer or use a fixed-point decimal type.",
					Ref:    ref, Line: ref.Line,
				})
			case isInteger(tv.Type) && !hasCurrency && isBasic(tv.Type):
				out = append(out, fact.Finding{
					ID: "MONEY-NO-CURRENCY", Severity: fact.SevMedium, Basis: fact.Heuristic,
					Title:  typeName + "." + nm.Name + " is a bare integer with no currency alongside it",
					Detail: "An amount without a currency is a number. Nothing stops a caller adding EUR minor units to USD minor units, and the type system will not object. Pair the amount with a currency in one type so the compiler can refuse the mistake.",
					Ref:    ref, Line: ref.Line,
				})
			}
		}
	}
	return out
}

func (p *Program) checkSignature(pkg *packages.Package, fd *ast.FuncDecl) []fact.Finding {
	var out []fact.Finding
	sym := declSymbol(pkg, fd)
	check := func(fl *ast.FieldList, what string) {
		if fl == nil {
			return
		}
		for _, fld := range fl.List {
			tv, ok := pkg.TypesInfo.Types[fld.Type]
			if !ok || !isFloat(tv.Type) {
				continue
			}
			for _, nm := range fld.Names {
				if !naming.IsMoneyAmountName(nm.Name) {
					continue
				}
				line := p.line(fld.Pos())
				out = append(out, fact.Finding{
					ID: "MONEY-FLOAT", Severity: fact.SevCritical, Basis: fact.Heuristic,
					Title:  sym + " takes money as a float in " + what + " " + nm.Name,
					Detail: "Every caller now has to round, and they will not all round the same way. Take minor units as an integer at the boundary and convert once, where the conversion can be tested.",
					Ref:    fact.CodeRef{Pkg: pkg.PkgPath, Symbol: sym, File: p.rel(fld.Pos()), Line: line},
					Line:   line,
				})
			}
		}
	}
	check(fd.Type.Params, "parameter")
	check(fd.Type.Results, "result")
	return out
}

func (p *Program) moneyTypesIn(fn *ssa.Function) []string {
	seen := map[string]bool{}
	consider := func(t types.Type) {
		n := elemNamed(t)
		if n == nil {
			return
		}
		key := types.TypeString(n, relativeTo)
		if naming.IsMoneyType(n.Obj().Name()) {
			seen[key] = true
			return
		}
		if fields, isStruct := n.Underlying().(*types.Struct); isStruct {
			for i := 0; i < fields.NumFields(); i++ {
				fld := fields.Field(i)
				if !fld.Exported() && fld.Pkg() != nil {
					continue
				}
				if naming.IsMoneyName(fld.Name()) || moneyShaped(fld.Type()) {
					seen[key+" (carries "+fld.Name()+")"] = true
					return
				}
			}
		}
	}
	sig := fn.Signature
	for i := 0; i < sig.Params().Len(); i++ {
		param := sig.Params().At(i)
		consider(param.Type())
		if naming.IsMoneyName(param.Name()) {
			seen[param.Name()+" "+types.TypeString(param.Type(), relativeTo)] = true
		}
	}
	for i := 0; i < sig.Results().Len(); i++ {
		consider(sig.Results().At(i).Type())
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func moneyShaped(t types.Type) bool {
	n := elemNamed(t)
	return n != nil && naming.IsMoneyType(n.Obj().Name())
}

func (p *Program) moneyCandidates() fact.Candidates {
	var out fact.Candidates
	for _, pkg := range p.prod {
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := spec.Type.(*ast.StructType)
				if !ok {
					if naming.IsMoneyType(spec.Name.Name) {
						out = append(out, fact.Candidate{
							Name: spec.Name.Name, Owner: pkg.PkgPath,
							Type: types.ExprString(spec.Type), File: p.rel(spec.Pos()), Line: p.line(spec.Pos()),
							Score: 5,
							Proof: []string{"a named type whose name means money", "declared as " + types.ExprString(spec.Type)},
						})
					}
					return true
				}
				if naming.IsContainerName(spec.Name.Name) {
					return true
				}
				out = append(out, p.moneyFieldCandidates(pkg, spec.Name.Name, st)...)
				return true
			})
		}
	}
	out = withoutGenerated(out, p.generated, func(c fact.Candidate) string { return c.File })
	out.Sort()
	return out
}

func (p *Program) moneyFieldCandidates(pkg *packages.Package, owner string, st *ast.StructType) []fact.Candidate {
	hasCurrency := false
	for _, fld := range st.Fields.List {
		for _, nm := range fld.Names {
			if naming.IsCurrencyName(nm.Name) {
				hasCurrency = true
			}
		}
	}
	var out []fact.Candidate
	for _, fld := range st.Fields.List {
		tv, ok := pkg.TypesInfo.Types[fld.Type]
		if !ok || isDuration(tv.Type) {
			continue
		}
		for _, nm := range fld.Names {
			if !naming.IsMoneyName(nm.Name) {
				continue
			}
			c := fact.Candidate{
				Name: nm.Name, Owner: owner, Type: types.ExprString(fld.Type),
				File: p.rel(nm.Pos()), Line: p.line(nm.Pos()),
				Score: 2, Proof: []string{"the field name means an amount"},
			}
			switch {
			case isFloat(tv.Type):
				c.Score--
				c.Against = append(c.Against, "stored in a float, which cannot represent 0.10 exactly — reported as MONEY-FLOAT")
			case isInteger(tv.Type) && !isBasic(tv.Type):
				c.Score += 3
				c.Proof = append(c.Proof, "a named integer type, so minor units with somewhere to hang the rules")
			case isInteger(tv.Type):
				c.Score += 2
				c.Proof = append(c.Proof, "a plain integer, so minor units")
			}
			if hasCurrency {
				c.Score += 2
				c.Proof = append(c.Proof, "a currency field sits beside it in the same struct")
			} else {
				c.Against = append(c.Against, "no currency field in the same struct — an amount alone is a number, not money")
			}
			out = append(out, c)
		}
	}
	return out
}

func withoutGenerated[T any](items []T, gen map[string]bool, fileOf func(T) string) []T {
	if len(gen) == 0 {
		return items
	}
	kept := items[:0:0]
	for _, it := range items {
		if !gen[fileOf(it)] {
			kept = append(kept, it)
		}
	}
	return kept
}
