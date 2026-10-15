package scan

import (
	"go/ast"
	"go/constant"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/sqlstmt"
)

var ormWriteVerb = map[string]string{
	"Insert": "insert", "Create": "insert", "Update": "update", "Updates": "update",
	"UpdateColumn": "update", "UpdateColumns": "update", "Save": "update",
	"Delete": "delete", "ForceDelete": "delete",
}

// tableWrites classifies how each table is written, from raw SQL at any
// argument position and from ORM call chains whose methods belong to a
// database library. testigo matched ORM verbs by name on any receiver, so
// list.Insert counted as a write.
func (p *Program) tableWrites() []fact.TableWrites {
	known := map[string]bool{}
	for _, t := range p.infra.Tables {
		known[t.Name] = true
	}
	accept := func(table string) bool { return table != "" && (len(known) == 0 || known[table]) }

	acc := map[string]*fact.TableWrites{}
	at := func(table string) *fact.TableWrites {
		if w, ok := acc[table]; ok {
			return w
		}
		w := &fact.TableWrites{Table: table}
		acc[table] = w
		return w
	}

	locked := map[string]bool{}
	for _, pkg := range p.prod {
		for _, span := range p.funcTable(pkg) {
			if p.locksRows(pkg, span.decl.Body) {
				locked[span.ref.ID()] = true
			}
		}
	}

	for _, site := range p.sql {
		for _, st := range site.stmts {
			if !st.Writes() || !accept(st.Table) {
				continue
			}
			w := at(st.Table)
			switch st.Kind {
			case sqlstmt.Insert:
				w.Inserts++
				w.Upsert = w.Upsert || st.Upsert
			case sqlstmt.Update:
				w.Updates++
				if len(st.SelfArith) > 0 {
					w.InDatabaseMath = true
				} else {
					w.ReadModifyWrite = true
				}
			case sqlstmt.Delete:
				w.Deletes++
			}
			w.RowLock = w.RowLock || st.Locks || locked[site.in.ID()]
			w.Where = appendRef(w.Where, site.in)
		}
		for _, st := range site.stmts {
			if st.Kind == sqlstmt.Select && st.Locks && accept(st.Table) {
				at(st.Table).RowLock = true
			}
		}
	}

	for _, pkg := range p.prod {
		for _, span := range p.funcTable(pkg) {
			p.ormWrites(pkg, span, accept, at, locked[span.ref.ID()])
		}
	}

	out := make([]fact.TableWrites, 0, len(acc))
	for _, w := range acc {
		w.Pattern = fact.ClassifyWrites(*w)
		out = append(out, *w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Table < out[j].Table })
	return out
}

func (p *Program) ormWrites(pkg *packages.Package, span funcSpan, accept func(string) bool, at func(string) *fact.TableWrites, fnLock bool) {
	info := pkg.TypesInfo
	fallback := ""
	ast.Inspect(span.decl.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && calleeName(call.Fun) == "Model" && callsDB(info, call) && fallback == "" && len(call.Args) > 0 {
			fallback = p.tableOfExpr(info, call.Args[0])
		}
		return true
	})
	ast.Inspect(span.decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		verb, ok := ormWriteVerb[calleeName(call.Fun)]
		if !ok || !callsDB(info, call) {
			return true
		}
		chain := chainCalls(call)
		table := p.tableInChain(info, chain)
		if table == "" && verb == "insert" && len(call.Args) > 0 {
			table = p.tableOfExpr(info, call.Args[len(call.Args)-1])
		}
		if table == "" {
			table = fallback
		}
		if !accept(table) {
			return true
		}
		w := at(table)
		switch verb {
		case "insert":
			w.Inserts++
		case "update":
			w.Updates++
			sets := setArgs(info, chain)
			if len(sets) == 0 {
				w.ReadModifyWrite = true
			}
			for _, s := range sets {
				if hasSelfArith(s) {
					w.InDatabaseMath = true
				} else {
					w.ReadModifyWrite = true
				}
			}
		case "delete":
			w.Deletes++
		}
		if chainHas(chain, "OnConflict") || chainHas(chain, "Clauses") && onConflictClause(info, chain) {
			w.Upsert = true
		}
		if fnLock || chainHas(chain, "ForUpdate") || chainHas(chain, "ForShare") || chainHas(chain, "For") {
			w.RowLock = true
		}
		w.Where = appendRef(w.Where, span.ref)
		return true
	})
}

func onConflictClause(info *types.Info, chain []*ast.CallExpr) bool {
	for _, c := range chain {
		if calleeName(c.Fun) != "Clauses" {
			continue
		}
		for _, a := range c.Args {
			if lit, ok := a.(*ast.CompositeLit); ok {
				if n := namedOf(info.TypeOf(lit)); n != nil && n.Obj().Name() == "OnConflict" {
					return true
				}
			}
		}
	}
	return false
}

func (p *Program) locksRows(pkg *packages.Package, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			switch calleeName(call.Fun) {
			case "TryXactLock", "Lock", "RunInTransaction":
				found = found || callsDB(pkg.TypesInfo, call)
			}
		}
		return !found
	})
	return found
}

func (p *Program) tableOfExpr(info *types.Info, e ast.Expr) string {
	n := elemNamed(info.TypeOf(e))
	if n == nil {
		return ""
	}
	if _, isStruct := n.Underlying().(*types.Struct); !isStruct {
		return ""
	}
	return p.tableOf(n)
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

func (p *Program) tableInChain(info *types.Info, chain []*ast.CallExpr) string {
	for _, c := range chain {
		switch calleeName(c.Fun) {
		case "Model":
			if len(c.Args) > 0 {
				if t := p.tableOfExpr(info, c.Args[0]); t != "" {
					return t
				}
			}
		case "Table", "TableExpr":
			if len(c.Args) > 0 {
				if tv, ok := info.Types[c.Args[0]]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
					return cleanSQLIdent(constant.StringVal(tv.Value))
				}
			}
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

func setArgs(info *types.Info, chain []*ast.CallExpr) []string {
	var out []string
	for _, c := range chain {
		if calleeName(c.Fun) != "Set" || len(c.Args) == 0 {
			continue
		}
		if tv, ok := info.Types[c.Args[0]]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
			out = append(out, constant.StringVal(tv.Value))
		}
	}
	return out
}

func hasSelfArith(s string) bool {
	st := sqlstmt.Parse("UPDATE t SET " + s)
	return len(st) == 1 && len(st[0].SelfArith) > 0
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
