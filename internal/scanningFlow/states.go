package scanningFlow

import (
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/scanningFlow/patterns"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/katayunak/testigo/internal/codeRef"
)

func extractStateMachines(pkgs []*packages.Package, root string, local, gen map[string]bool) []flowEntity.StateMachine {
	var out []flowEntity.StateMachine
	type cand struct {
		named  *types.Named
		states map[string]bool
	}
	cands := map[string]*cand{}

	for _, p := range pkgs {
		if !local[p.PkgPath] || p.TypesInfo == nil || p.Types == nil {
			continue
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}

			if !patterns.IsStateType(name) && !patterns.IsWeakStateType(name) {
				continue
			}

			if gen[relPath(p.Fset, tn.Pos(), root)] {
				continue
			}

			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			u := named.Underlying()
			if b, ok := u.(*types.Basic); !ok || b.Info()&(types.IsString|types.IsInteger) == 0 {
				continue
			}
			cands[types.TypeString(named, relativeTo)] = &cand{named: named, states: map[string]bool{}}
		}
	}
	if len(cands) == 0 {
		return nil
	}

	for _, p := range pkgs {
		if p.Types == nil {
			continue
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			c, ok := scope.Lookup(name).(*types.Const)
			if !ok {
				continue
			}
			key := types.TypeString(c.Type(), relativeTo)
			if cd, ok := cands[key]; ok {
				cd.states[c.Name()] = true
			}
		}
	}

	writes := map[string][]flowEntity.StateWrite{}
	fields := map[string]string{}
	isLifecycleType := func(key string) bool { _, ok := cands[key]; return ok }
	for _, p := range pkgs {
		if !local[p.PkgPath] || p.TypesInfo == nil {
			continue
		}
		fns := funcIndex(p, root)
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				switch t := n.(type) {
				case *ast.StructType:
					for _, fld := range t.Fields.List {
						tv, ok := p.TypesInfo.Types[fld.Type]
						if !ok {
							continue
						}
						key := types.TypeString(tv.Type, relativeTo)
						if _, isCand := cands[key]; isCand && len(fld.Names) > 0 {
							fields[key] = fld.Names[0].Name
						}
					}
				case *ast.CompositeLit:

					for _, el := range t.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						tv, ok := p.TypesInfo.Types[kv.Value]
						if !ok {
							continue
						}
						key := types.TypeString(tv.Type, relativeTo)
						if _, isCand := cands[key]; !isCand {
							continue
						}
						to := "<dynamic>"
						if tv.Value != nil {
							to = constName(p, kv.Value, tv.Value.String())
						}
						pos := p.Fset.Position(kv.Pos())
						writes[key] = append(writes[key], flowEntity.StateWrite{
							In: siteOf(p, fns, root, kv.Pos()), To: to, Line: pos.Line,
						})
					}
				case *ast.ReturnStmt:

					for _, r := range t.Results {
						addWrite(p, fns, isLifecycleType, writes, relativeTo, root, r)
					}
				case *ast.AssignStmt:
					for i, lhs := range t.Lhs {
						if i >= len(t.Rhs) {
							continue
						}

						tv, ok := p.TypesInfo.Types[lhs]
						if !ok || !isLifecycleType(types.TypeString(tv.Type, relativeTo)) {
							addWrite(p, fns, isLifecycleType, writes, relativeTo, root, t.Rhs[i])
							continue
						}
						key := types.TypeString(tv.Type, relativeTo)
						to := "<dynamic>"
						if rv, ok := p.TypesInfo.Types[t.Rhs[i]]; ok && rv.Value != nil {
							to = constName(p, t.Rhs[i], rv.Value.String())
						}
						pos := p.Fset.Position(t.Pos())
						writes[key] = append(writes[key], flowEntity.StateWrite{
							In: siteOf(p, fns, root, t.Pos()), To: to, Line: pos.Line,
						})
					}
				}
				return true
			})
		}
	}

	for key, cd := range cands {
		if len(cd.states) == 0 {
			continue
		}
		if why, ok := isLifecycle(key, cd.named.Obj().Name(), fields[key], writes[key]); !ok {
			_ = why
			continue
		}
		states := make([]string, 0, len(cd.states))
		for s := range cd.states {
			states = append(states, s)
		}
		sort.Strings(states)
		w := writes[key]
		sort.Slice(w, func(i, j int) bool {
			if w[i].In.ID() != w[j].In.ID() {
				return w[i].In.ID() < w[j].In.ID()
			}
			return w[i].Line < w[j].Line
		})
		written := map[string]bool{}
		for _, x := range w {
			written[x.To] = true
		}
		var never []string
		for _, st := range states {
			if !written[st] {
				never = append(never, st)
			}
		}
		out = append(out, flowEntity.StateMachine{
			Type: key, Field: fields[key], States: states, Writes: w, NeverAssigned: never,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

func isLifecycle(qualified, simple, field string, writes []flowEntity.StateWrite) (string, bool) {
	distinct := map[string]bool{}
	for _, w := range writes {
		distinct[w.In.ID()] = true
	}

	if patterns.IsStateType(simple) {
		if len(writes) == 0 {
			return "named like a lifecycle, but nothing in this module ever assigns it, so nothing moves through it", false
		}
		return "", true
	}

	switch {
	case field == "":
		return "never stored in a struct field, so it does not survive between requests", false
	case len(distinct) < 2:
		return "assigned in fewer than two places, so nothing moves through it", false
	}
	return "", true
}

func constName(p *packages.Package, e ast.Expr, fallback string) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return strings.Trim(fallback, `"`)
}

type funcTable []funcSpan

type funcSpan struct {
	a          codeRef.CodeRef
	start, end token.Pos
}

func funcIndex(p *packages.Package, root string) funcTable {
	var ft funcTable
	for _, f := range p.Syntax {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			h, _ := codeRef.StructuralHash(fd)
			ft = append(ft, funcSpan{
				a: codeRef.CodeRef{
					Pkg:      p.PkgPath,
					Symbol:   codeRef.Symbol(fd),
					File:     relPath(p.Fset, fd.Pos(), root),
					Line:     p.Fset.Position(fd.Pos()).Line,
					BodyHash: h,
				},
				start: fd.Pos(), end: fd.End(),
			})
		}
	}
	sort.Slice(ft, func(i, j int) bool { return ft[i].start < ft[j].start })
	return ft
}

func (ft funcTable) at(pos token.Pos) codeRef.CodeRef {
	i := sort.Search(len(ft), func(i int) bool { return ft[i].end >= pos })
	if i < len(ft) && ft[i].start <= pos && pos < ft[i].end {
		return ft[i].a
	}
	return codeRef.CodeRef{}
}

func relPath(fset *token.FileSet, pos token.Pos, root string) string {
	p := fset.Position(pos)
	if p.Filename == "" {
		return ""
	}
	if rel, err := filepath.Rel(root, p.Filename); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(p.Filename)
}

func addWrite(
	p *packages.Package,
	fns funcTable,
	isLifecycleType func(string) bool,
	writes map[string][]flowEntity.StateWrite,
	relativeTo types.Qualifier,
	root string,
	e ast.Expr,
) {
	tv, ok := p.TypesInfo.Types[e]
	if !ok {
		return
	}
	key := types.TypeString(tv.Type, relativeTo)
	if !isLifecycleType(key) {
		return
	}

	to := "<dynamic>"
	if tv.Value != nil {
		to = constName(p, e, tv.Value.String())
	}

	pos := p.Fset.Position(e.Pos())
	writes[key] = append(writes[key], flowEntity.StateWrite{
		In: siteOf(p, fns, root, e.Pos()), To: to, Line: pos.Line,
	})
}

func siteOf(p *packages.Package, fns funcTable, root string, pos token.Pos) codeRef.CodeRef {
	in := fns.at(pos)
	if in.File == "" {
		in.File = relPath(p.Fset, pos, root)
	}
	if in.Pkg == "" {
		in.Pkg = p.PkgPath
	}
	if in.Symbol == "" {
		in.Symbol = "(package level)"
	}
	return in
}
