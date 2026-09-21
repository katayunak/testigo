package scanningFlow

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"golang.org/x/tools/go/packages"
)

var (
	reSQLInsert = regexp.MustCompile(`(?is)\binsert\s+into\s+([\w."]+)`)
	reSQLUpdate = regexp.MustCompile(`(?is)\bupdate\s+([\w."]+)\s+set\s+(.*?)(?:\bwhere\b|$)`)
	reSQLDelete = regexp.MustCompile(`(?is)\bdelete\s+from\s+([\w."]+)`)
	reSQLUpsert = regexp.MustCompile(`(?is)\bon\s+conflict\b`)

	reAssignArith = regexp.MustCompile(`(?i)([a-z_][a-z0-9_]*)\s*=\s*([a-z_][a-z0-9_]*)\s*[-+]`)
)

func hasSelfArith(s string) bool {
	for _, m := range reAssignArith.FindAllStringSubmatch(s, -1) {
		if strings.EqualFold(m[1], m[2]) {
			return true
		}
	}
	return false
}

var writeVerb = map[string]string{
	"Insert": "insert", "Update": "update", "Delete": "delete",
	"ForceDelete": "delete", "Updates": "update", "Save": "update",
}

func writePatterns(pkgs []*packages.Package, root string, infra flowEntity.Infra) []flowEntity.TableWrites {
	known := map[string]bool{}
	for _, t := range infra.Tables {
		known[t.Name] = true
	}

	acc := map[string]*flowEntity.TableWrites{}
	at := func(table string) *flowEntity.TableWrites {
		if w, ok := acc[table]; ok {
			return w
		}
		w := &flowEntity.TableWrites{Table: table}
		acc[table] = w
		return w
	}

	for _, p := range pkgs {
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}

				pos := p.Fset.Position(fn.Pos())
				ref := flowEntity.CodeRef{
					Pkg: p.PkgPath, Symbol: Symbol(fn),
					File: fileRelTo(pos.Filename, root), Line: pos.Line,
				}

				fallback := ""
				fnLock := false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, isCall := n.(*ast.CallExpr)
					if !isCall {
						return true
					}
					switch calleeName(call.Fun) {
					case "Model":
						if len(call.Args) > 0 && fallback == "" {
							if t := tableOfExpr(p, call.Args[0], known); t != "" {
								fallback = t
							}
						}
					case "TryXactLock", "Lock", "RunInTransaction":
						fnLock = true
					}
					return true
				})

				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, isCall := n.(*ast.CallExpr)
					if !isCall {
						return true
					}

					if s, ok := stringLit(firstArg(call)); ok && len(s) > 12 {
						applyRawSQL(s, at, known, ref)
					}

					verb, isVerb := writeVerb[calleeName(call.Fun)]
					if !isVerb {
						return true
					}
					chain := chainCalls(call)
					table := tableInChain(p, chain, known)
					if table == "" {
						table = fallback
					}
					if table == "" {
						return true
					}

					w := at(table)
					switch verb {
					case "insert":
						w.Inserts++
					case "update":
						w.Updates++
					case "delete":
						w.Deletes++
					}
					if chainHas(chain, "OnConflict") {
						w.Upsert = true
					}
					if fnLock || chainHas(chain, "ForUpdate") || chainHas(chain, "ForShare") {
						w.RowLock = true
					}
					sets := setArgsIn(chain)
					if verb == "update" {
						if len(sets) == 0 {
							w.ReadModifyWrite = true
						}
						for _, arg := range sets {
							if hasSelfArith(arg) {
								w.InDatabaseMath = true
							} else {
								w.ReadModifyWrite = true
							}
						}
					}
					w.Where = appendRef(w.Where, ref)
					return true
				})
			}
		}
	}

	out := make([]flowEntity.TableWrites, 0, len(acc))
	for _, w := range acc {
		w.Pattern = classify(*w)
		out = append(out, *w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Table < out[j].Table })
	return out
}

func applyRawSQL(sql string, at func(string) *flowEntity.TableWrites, known map[string]bool, ref flowEntity.CodeRef) {
	upsert := reSQLUpsert.MatchString(sql)

	for _, m := range reSQLInsert.FindAllStringSubmatch(sql, -1) {
		t := cleanIdent(m[1])
		if !known[t] {
			continue
		}
		w := at(t)
		w.Inserts++
		if upsert {
			w.Upsert = true
		}
		w.Where = appendRef(w.Where, ref)
	}
	for _, m := range reSQLUpdate.FindAllStringSubmatch(sql, -1) {
		t := cleanIdent(m[1])
		if !known[t] {
			continue
		}
		w := at(t)
		w.Updates++
		if hasSelfArith(m[2]) {
			w.InDatabaseMath = true
		} else {
			w.ReadModifyWrite = true
		}
		w.Where = appendRef(w.Where, ref)
	}
	for _, m := range reSQLDelete.FindAllStringSubmatch(sql, -1) {
		t := cleanIdent(m[1])
		if !known[t] {
			continue
		}
		w := at(t)
		w.Deletes++
		w.Where = appendRef(w.Where, ref)
	}
}

func classify(w flowEntity.TableWrites) flowEntity.WritePattern {
	switch {
	case w.Upsert:
		return flowEntity.WriteUpsert
	case w.Updates == 0 && w.Inserts > 0:
		return flowEntity.WriteInsertOnly
	case w.InDatabaseMath && !w.ReadModifyWrite:
		return flowEntity.WriteUpdateInDatabase
	case w.Updates > 0:
		return flowEntity.WriteUpdateInPlace
	}
	return flowEntity.WriteUnknown
}

func tableOfExpr(p *packages.Package, e ast.Expr, known map[string]bool) string {
	tv, ok := p.TypesInfo.Types[e]
	if !ok {
		return ""
	}
	t := tv.Type
	for {
		ptr, isPtr := t.(*types.Pointer)
		if !isPtr {
			break
		}
		t = ptr.Elem()
	}
	if sl, isSlice := t.(*types.Slice); isSlice {
		t = sl.Elem()
		if ptr, isPtr := t.(*types.Pointer); isPtr {
			t = ptr.Elem()
		}
	}
	named, isNamed := t.(*types.Named)
	if !isNamed || named.Obj() == nil {
		return ""
	}
	guess := pluralize(snakeCase(named.Obj().Name()))
	if known[guess] {
		return guess
	}
	return ""
}

func stringLit(e ast.Expr) (string, bool) {
	if e == nil {
		return "", false
	}
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func appendRef(list []flowEntity.CodeRef, ref flowEntity.CodeRef) []flowEntity.CodeRef {
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

func chainCalls(call *ast.CallExpr) []*ast.CallExpr {
	var out []*ast.CallExpr
	cur := call
	for i := 0; i < 40; i++ {
		out = append(out, cur)
		sel, ok := cur.Fun.(*ast.SelectorExpr)
		if !ok {
			break
		}
		next, ok := sel.X.(*ast.CallExpr)
		if !ok {
			break
		}
		cur = next
	}
	return out
}

func tableInChain(p *packages.Package, chain []*ast.CallExpr, known map[string]bool) string {
	for _, c := range chain {
		if calleeName(c.Fun) != "Model" || len(c.Args) == 0 {
			continue
		}
		if t := tableOfExpr(p, c.Args[0], known); t != "" {
			return t
		}
	}
	return ""
}

func chainHas(chain []*ast.CallExpr, name string) bool {
	for _, c := range chain {
		if calleeName(c.Fun) == name {
			return true
		}
	}
	return false
}

func setArgsIn(chain []*ast.CallExpr) []string {
	var out []string
	for _, c := range chain {
		if calleeName(c.Fun) != "Set" {
			continue
		}
		if s, ok := stringLit(firstArg(c)); ok {
			out = append(out, s)
		}
	}
	return out
}

func firstArg(c *ast.CallExpr) ast.Expr {
	if len(c.Args) == 0 {
		return nil
	}
	return c.Args[0]
}
