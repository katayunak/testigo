package scanningFlow

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"

	"github.com/katayunak/testigo/internal/codeRef"
)

var moneyName = regexp.MustCompile(`(?i)(amount|balance|subtotal|total|price|fee|cost|tax|discount|refund|payout|credit|debit|charge|money|cents|principal|premium|commission)`)

var moneyTypeName = regexp.MustCompile(`(?i)^(money|amount|currency|decimal|cents|minor(units)?)$`)

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
func moneyFindings(pkgs []*packages.Package, root string) []Finding {
	var out []Finding
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
					out = append(out, Finding{
						ID: "MONEY-DIV", Severity: SevHigh,
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
		return moneyName.MatchString(x.Name)

	case *ast.SelectorExpr:
		return moneyName.MatchString(x.Sel.Name)
	}

	if tv, ok := p.TypesInfo.Types[e]; ok {
		if n, isNamed := tv.Type.(*types.Named); isNamed {
			return moneyTypeName.MatchString(n.Obj().Name())
		}
	}

	return false
}

func checkStruct(p *packages.Package, root, typeName string, st *ast.StructType) []Finding {
	var out []Finding
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
			if !moneyName.MatchString(nm.Name) {
				continue
			}
			a := codeRef.CodeRef{Pkg: p.PkgPath, Symbol: "type " + typeName, File: relPath(p.Fset, fld.Pos(), root), Line: p.Fset.Position(fld.Pos()).Line}
			if isFloat(tv.Type) {
				out = append(out, Finding{
					ID: "MONEY-FLOAT", Severity: SevCritical,
					Title:  typeName + "." + nm.Name + " stores money in a float",
					Detail: "Binary floating point cannot represent 0.10 exactly. Summing a thousand line items drifts, comparisons that should be equal are not, and the error is invisible in any test that checks a single transaction. Store minor units in an integer (int64 cents) or use a fixed-point decimal type.",
					Ref:    a, Line: a.Line,
				})

			} else if isInteger(tv.Type) && !hasCurrency && isBasicNamed(tv.Type) {
				out = append(out, Finding{
					ID: "MONEY-NO-CURRENCY", Severity: SevMedium,
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

func checkSignature(p *packages.Package, root string, fd *ast.FuncDecl) []Finding {
	var out []Finding
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
				if !moneyName.MatchString(nm.Name) {
					continue
				}
				out = append(out, Finding{
					ID: "MONEY-FLOAT", Severity: SevCritical,
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
		if n, ok := t.(*types.Named); ok && moneyTypeName.MatchString(n.Obj().Name()) {
			seen[types.TypeString(t, relativeTo)] = true
		}
	}
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		consider(p.Type())
		if moneyName.MatchString(p.Name()) {
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
