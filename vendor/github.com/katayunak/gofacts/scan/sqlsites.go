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

// sqlSite is a call that passes a SQL statement. The statement is found in any
// argument, not only the first: database/sql's ExecContext and every pgx call
// take a context first, and testigo's first-argument rule never saw them.
type sqlSite struct {
	pkg   *packages.Package
	in    fact.CodeRef
	line  int
	call  *ast.CallExpr
	arg   int
	stmts []sqlstmt.Statement
	db    bool
	binds []fieldBinding
}

// fieldBinding ties a struct field to the column a statement stores it in or
// compares it with, through the placeholder the field is passed for.
type fieldBinding struct {
	field  *types.Var
	table  string
	column string
	where  bool
	kind   sqlstmt.Kind
}

func (p *Program) sqlSites() []sqlSite {
	var out []sqlSite
	for _, pkg := range p.prod {
		info := pkg.TypesInfo
		for _, span := range p.funcTable(pkg) {
			defs := localDefs(info, span.decl.Body)
			ast.Inspect(span.decl.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				for i, arg := range call.Args {
					text, ok := stringConst(info, defs, arg)
					if !ok || !sqlstmt.Looks(text) {
						continue
					}
					site := sqlSite{
						pkg: pkg, in: span.ref, line: p.line(call.Pos()), call: call, arg: i,
						stmts: sqlstmt.Parse(text), db: callsDB(info, call),
					}
					if !call.Ellipsis.IsValid() {
						site.binds = bindFields(info, call, i, site.stmts)
					}
					out = append(out, site)
					break
				}
				return true
			})
		}
	}
	return out
}

// stringConst is the constant string value of e, looking through one local
// variable assigned once.
func stringConst(info *types.Info, defs map[*types.Var]ast.Expr, e ast.Expr) (string, bool) {
	if tv, ok := info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		return constant.StringVal(tv.Value), true
	}
	if v := localVar(info, e); v != nil {
		if def, ok := defs[v]; ok {
			if tv, ok := info.Types[def]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
				return constant.StringVal(tv.Value), true
			}
		}
	}
	return "", false
}

func calleeFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		fn, _ := info.Uses[f.Sel].(*types.Func)
		return fn
	case *ast.Ident:
		fn, _ := info.Uses[f].(*types.Func)
		return fn
	}
	return nil
}

func callsDB(info *types.Info, call *ast.CallExpr) bool {
	fn := calleeFunc(info, call)
	return fn != nil && fn.Pkg() != nil && isDBPackage(fn.Pkg().Path())
}

func bindFields(info *types.Info, call *ast.CallExpr, sqlArg int, stmts []sqlstmt.Statement) []fieldBinding {
	var out []fieldBinding
	add := func(binds map[string]int, st sqlstmt.Statement, where bool) {
		cols := make([]string, 0, len(binds))
		for c := range binds {
			cols = append(cols, c)
		}
		sort.Strings(cols)
		for _, col := range cols {
			idx := sqlArg + binds[col]
			if idx >= len(call.Args) {
				continue
			}
			if v := fieldVar(info, call.Args[idx]); v != nil {
				out = append(out, fieldBinding{field: v, table: st.Table, column: col, where: where, kind: st.Kind})
			}
		}
	}
	for _, st := range stmts {
		add(st.Binds, st, false)
		add(st.WhereBinds, st, true)
	}
	return out
}

// whereColumns lists every (table, column) the program compares in a WHERE
// clause: the columns it looks rows up by.
func (p *Program) whereColumns() map[[2]string]bool {
	out := map[[2]string]bool{}
	for _, s := range p.sql {
		for _, st := range s.stmts {
			for _, c := range st.Where {
				out[[2]string{st.Table, c}] = true
			}
		}
	}
	return out
}
