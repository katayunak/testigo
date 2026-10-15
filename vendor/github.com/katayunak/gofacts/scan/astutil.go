package scan

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/katayunak/gofacts/fact"
)

type funcSpan struct {
	ref        fact.CodeRef
	start, end token.Pos
	decl       *ast.FuncDecl
}

type funcTable []funcSpan

func (p *Program) funcTable(pkg *packages.Package) funcTable {
	var ft funcTable
	for _, f := range pkg.Syntax {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ft = append(ft, funcSpan{
				ref:   fact.CodeRef{Pkg: pkg.PkgPath, Symbol: declSymbol(pkg, fd), File: p.rel(fd.Pos()), Line: p.line(fd.Pos())},
				start: fd.Pos(), end: fd.End(), decl: fd,
			})
		}
	}
	sort.Slice(ft, func(i, j int) bool { return ft[i].start < ft[j].start })
	return ft
}

func (ft funcTable) span(pos token.Pos) (funcSpan, bool) {
	i := sort.Search(len(ft), func(i int) bool { return ft[i].end >= pos })
	if i < len(ft) && ft[i].start <= pos && pos < ft[i].end {
		return ft[i], true
	}
	return funcSpan{}, false
}

// at returns the enclosing declaration, or a package-level reference for code
// outside any function.
func (ft funcTable) at(p *Program, pkg *packages.Package, pos token.Pos) fact.CodeRef {
	if s, ok := ft.span(pos); ok {
		return s.ref
	}
	return fact.CodeRef{Pkg: pkg.PkgPath, Symbol: "(package level)", File: p.rel(pos), Line: p.line(pos)}
}

// declSymbol names a declaration the same way symbolOf names its SSA function.
func declSymbol(pkg *packages.Package, fd *ast.FuncDecl) string {
	if obj, ok := pkg.TypesInfo.Defs[fd.Name].(*types.Func); ok {
		return symbolOf(obj)
	}
	return fd.Name.Name
}

func isFloat(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsFloat != 0
}

func isInteger(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

func isBasic(t types.Type) bool {
	_, ok := types.Unalias(t).(*types.Basic)
	return ok
}

func isDuration(t types.Type) bool {
	if named, ok := types.Unalias(t).(*types.Named); ok {
		obj := named.Obj()
		return obj != nil && obj.Name() == "Duration" && obj.Pkg() != nil && obj.Pkg().Path() == "time"
	}
	return false
}

func namedOf(t types.Type) *types.Named {
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	n, _ := t.(*types.Named)
	return n
}

func elemNamed(t types.Type) *types.Named {
	for {
		switch x := types.Unalias(t).(type) {
		case *types.Pointer:
			t = x.Elem()
			continue
		case *types.Slice:
			t = x.Elem()
			continue
		case *types.Array:
			t = x.Elem()
			continue
		case *types.Named:
			return x
		}
		return nil
	}
}

// fieldVar resolves a selector or composite-literal key to the struct field it
// names, so evidence about Payment.ReferenceID is not mistaken for evidence
// about Refund.ReferenceID just because the names match.
func fieldVar(info *types.Info, e ast.Expr) *types.Var {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[x]; ok && sel.Kind() == types.FieldVal {
			v, _ := sel.Obj().(*types.Var)
			return v
		}
		if v, ok := info.Uses[x.Sel].(*types.Var); ok && v.IsField() {
			return v
		}
	case *ast.Ident:
		if v, ok := info.Uses[x].(*types.Var); ok && v.IsField() {
			return v
		}
		if v, ok := info.Defs[x].(*types.Var); ok && v.IsField() {
			return v
		}
	case *ast.ParenExpr:
		return fieldVar(info, x.X)
	case *ast.StarExpr:
		return fieldVar(info, x.X)
	}
	return nil
}

// localDefs maps each local variable of a function body assigned exactly once
// to the expression it was assigned, which is enough to see through
// `key := r.Header.Get("Idempotency-Key")` and `q := "INSERT ..."`.
func localDefs(info *types.Info, body *ast.BlockStmt) map[*types.Var]ast.Expr {
	count := map[*types.Var]int{}
	val := map[*types.Var]ast.Expr{}
	note := func(id *ast.Ident, e ast.Expr) {
		v, ok := info.Defs[id].(*types.Var)
		if !ok {
			v, ok = info.Uses[id].(*types.Var)
		}
		if !ok || v.IsField() {
			return
		}
		count[v]++
		val[v] = e
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if len(s.Lhs) != len(s.Rhs) {
				for _, l := range s.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						note(id, nil)
					}
				}
				return true
			}
			for i, l := range s.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					note(id, s.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, id := range s.Names {
				if i < len(s.Values) && len(s.Values) == len(s.Names) {
					note(id, s.Values[i])
				} else {
					note(id, nil)
				}
			}
		}
		return true
	})
	out := map[*types.Var]ast.Expr{}
	for v, n := range count {
		if n == 1 && val[v] != nil {
			out[v] = val[v]
		}
	}
	return out
}

func localVar(info *types.Info, e ast.Expr) *types.Var {
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := info.Uses[id].(*types.Var)
	if !ok || v.IsField() {
		return nil
	}
	return v
}

func tagOf(fld *ast.Field) reflect.StructTag {
	if fld.Tag == nil {
		return ""
	}
	return reflect.StructTag(strings.Trim(fld.Tag.Value, "`"))
}

func snakeCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		upper := r >= 'A' && r <= 'Z'
		if upper && i > 0 {
			prevLower := runes[i-1] >= 'a' && runes[i-1] <= 'z' || runes[i-1] >= '0' && runes[i-1] <= '9'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			if prevLower || nextLower {
				b.WriteByte('_')
			}
		}
		if upper {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func pluralize(s string) string {
	switch {
	case s == "":
		return s
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	case strings.HasSuffix(s, "y") && len(s) > 1 && !strings.ContainsRune("aeiou", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func appendRef(list []fact.CodeRef, ref fact.CodeRef) []fact.CodeRef {
	for _, r := range list {
		if r.ID() == ref.ID() {
			return list
		}
	}
	if len(list) >= 8 {
		return list
	}
	return append(list, ref)
}
