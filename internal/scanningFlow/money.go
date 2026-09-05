package scanningFlow

import (
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/scanningFlow/patterns"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"

	"github.com/katayunak/testigo/internal/codeRef"
)

func isFloat(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsFloat != 0
}

func isInteger(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

// moneyFindings runs the checks that need no agent, no call graph and no
// running code
func moneyFindings(pkgs []*packages.Package, root string) []flowEntity.Finding {
	var out []flowEntity.Finding
	for _, p := range pkgs {
		fns := funcIndex(p, root)
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				switch t := n.(type) {
				case *ast.TypeSpec:
					st, ok := t.Type.(*ast.StructType)
					if !ok {
						return true
					}
					out = append(out, checkStruct(p, root, t.Name.Name, st)...)

				case *ast.FuncDecl:
					out = append(out, checkSignature(p, root, t)...)

				case *ast.BinaryExpr:
					if t.Op != token.QUO {
						return true
					}

					tv, ok := p.TypesInfo.Types[t.X]
					if !ok || !isMoneyExpr(p, t.X) {
						return true
					}

					if isFloat(tv.Type) {
						return true // already reported as MONEY-FLOAT
					}

					a := fns.at(t.Pos())
					out = append(out, flowEntity.Finding{
						ID: "MONEY-DIV", Severity: flowEntity.SevHigh,
						Title:  "money divided with no stated rounding rule",
						Detail: "Integer division truncates toward zero. Splitting a charge, prorating a subscription or computing a percentage fee this way silently loses the remainder, and the lost cents do not appear in any single test — they show up as a ledger that will not balance at month end. Decide the rounding direction explicitly and give the remainder an owner.",
						Ref:    a, Line: p.Fset.Position(t.Pos()).Line,
					})
				}
				return true
			})
		}
	}

	return out
}

func isMoneyExpr(p *packages.Package, e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return patterns.MoneyField.MatchString(x.Name)

	case *ast.SelectorExpr:
		return patterns.MoneyField.MatchString(x.Sel.Name)
	}

	if tv, ok := p.TypesInfo.Types[e]; ok {
		if n, isNamed := tv.Type.(*types.Named); isNamed {
			return patterns.MoneyType.MatchString(n.Obj().Name())
		}
	}

	return false
}

func checkStruct(p *packages.Package, root, typeName string, st *ast.StructType) []flowEntity.Finding {
	var out []flowEntity.Finding
	hasCurrency := false
	for _, fld := range st.Fields.List {
		for _, nm := range fld.Names {
			if strings.Contains(strings.ToLower(nm.Name), "currency") {
				hasCurrency = true
			}
		}
	}

	for _, fld := range st.Fields.List {
		tv, ok := p.TypesInfo.Types[fld.Type]
		if !ok {
			continue
		}

		for _, nm := range fld.Names {
			if !patterns.MoneyField.MatchString(nm.Name) {
				continue
			}
			a := codeRef.CodeRef{Pkg: p.PkgPath, Symbol: "type " + typeName, File: relPath(p.Fset, fld.Pos(), root), Line: p.Fset.Position(fld.Pos()).Line}
			if isFloat(tv.Type) {
				out = append(out, flowEntity.Finding{
					ID: "MONEY-FLOAT", Severity: flowEntity.SevCritical,
					Title:  typeName + "." + nm.Name + " stores money in a float",
					Detail: "Binary floating point cannot represent 0.10 exactly. Summing a thousand line items drifts, comparisons that should be equal are not, and the error is invisible in any test that checks a single transaction. Store minor units in an integer (int64 cents) or use a fixed-point decimal type.",
					Ref:    a, Line: a.Line,
				})

			} else if isInteger(tv.Type) && !hasCurrency && isBasicNamed(tv.Type) {
				out = append(out, flowEntity.Finding{
					ID: "MONEY-NO-CURRENCY", Severity: flowEntity.SevMedium,
					Title:  typeName + "." + nm.Name + " is a bare integer with no currency alongside it",
					Detail: "An amount without a currency is not money, it is a number. Nothing stops a caller adding EUR minor units to USD minor units, and the type system will not object. Pair the amount with a currency in one type so the compiler can refuse the mistake.",
					Ref:    a, Line: a.Line,
				})
			}
		}
	}
	return out
}

// isBasicNamed reports whether the type is an unnamed basic type, i.e. a plain
// int64 rather than a domain type like Cents. A named type at least gives the
// codebase somewhere to hang the currency rules.
func isBasicNamed(t types.Type) bool {
	_, isBasic := t.(*types.Basic)
	return isBasic
}

func checkSignature(p *packages.Package, root string, fd *ast.FuncDecl) []flowEntity.Finding {
	var out []flowEntity.Finding
	check := func(fl *ast.FieldList, what string) {
		if fl == nil {
			return
		}
		for _, fld := range fl.List {
			tv, ok := p.TypesInfo.Types[fld.Type]
			if !ok || !isFloat(tv.Type) {
				continue
			}
			for _, nm := range fld.Names {
				if !patterns.MoneyField.MatchString(nm.Name) {
					continue
				}
				out = append(out, flowEntity.Finding{
					ID: "MONEY-FLOAT", Severity: flowEntity.SevCritical,
					Title:  codeRef.Symbol(fd) + " takes money as a float in " + what + " " + nm.Name,
					Detail: "Every caller now has to round, and they will not all round the same way. Take minor units as an integer at the boundary and convert once, where the conversion can be tested.",
					Ref:    codeRef.CodeRef{Pkg: p.PkgPath, Symbol: codeRef.Symbol(fd), File: relPath(p.Fset, fld.Pos(), root), Line: p.Fset.Position(fld.Pos()).Line},
					Line:   p.Fset.Position(fld.Pos()).Line,
				})
			}
		}
	}
	check(fd.Type.Params, "parameter")
	check(fd.Type.Results, "result")
	return out
}

// moneyTypesIn reports which money-shaped types flow through a function. It is
// how the report can say "these are the steps that actually move money" rather
// than listing every function in the call graph with equal weight.
func (g *graph) moneyTypesIn(fn *ssa.Function) []string {
	seen := map[string]bool{}
	sig := fn.Signature
	consider := func(t types.Type) {
		for {
			switch x := t.(type) {
			case *types.Pointer:
				t = x.Elem()
				continue
			case *types.Slice:
				t = x.Elem()
				continue
			}
			break
		}
		n, ok := t.(*types.Named)
		if !ok {
			return
		}
		if patterns.MoneyType.MatchString(n.Obj().Name()) {
			seen[types.TypeString(t, relativeTo)] = true
			return
		}
		// A struct that CARRIES money is money for this purpose.
		//
		// Only the signature used to be read, and only for type names in the
		// money vocabulary. On a real recharge service that found nothing at
		// all: money there never travels as a bare Price argument, it travels
		// inside entity.Order, whose name says nothing about money. The scanner
		// then reported "no money-shaped type flows through the reachable
		// functions" and dropped SEVEN scenarios — every conservation, split,
		// currency and round-trip test — on a repository whose main entity has
		// four money fields.
		//
		// The proof was already in the same program. moneyCandidates had
		// scored Order.Price, Order.Fee, Order.BasePrice and Order.Discount.
		// This function simply never asked it.
		if fields, isStruct := n.Underlying().(*types.Struct); isStruct {
			for i := 0; i < fields.NumFields(); i++ {
				fld := fields.Field(i)
				if !fld.Exported() && fld.Pkg() != nil {
					continue
				}
				if patterns.MoneyField.MatchString(fld.Name()) || moneyShapedType(fld.Type()) {
					seen[types.TypeString(t, relativeTo)+" (carries "+fld.Name()+")"] = true
					return
				}
			}
		}
	}
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		consider(p.Type())
		if patterns.MoneyField.MatchString(p.Name()) {
			seen[p.Name()+" "+types.TypeString(p.Type(), relativeTo)] = true
		}
	}
	for i := 0; i < sig.Results().Len(); i++ {
		consider(sig.Results().At(i).Type())
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// moneyShapedType reports whether a type NAME is in the money vocabulary,
// following pointers and slices to the thing itself.
func moneyShapedType(t types.Type) bool {
	for {
		switch x := t.(type) {
		case *types.Pointer:
			t = x.Elem()
			continue
		case *types.Slice:
			t = x.Elem()
			continue
		}
		break
	}
	n, ok := t.(*types.Named)
	return ok && patterns.MoneyType.MatchString(n.Obj().Name())
}
