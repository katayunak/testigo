package scan

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/naming"
)

type origin int

const (
	originUnknown origin = iota
	originExternal
	originGenerated
)

// keyEvidence is everything known about one struct field, keyed by the field
// itself rather than by its name: testigo pooled every field called
// ReferenceID, so `ReferenceID: uuid()` in one struct disqualified every other.
type keyEvidence struct {
	origin  origin
	queried bool
	column  string
	table   string
	via     string
}

// idempotencyCandidates scores struct fields as idempotency keys. A key is a
// behaviour, not a name: it arrives from outside, the code looks rows up by it,
// and the database refuses a duplicate of it.
func (p *Program) idempotencyCandidates() (fact.Candidates, map[string]string) {
	evidence := p.keyEvidence()
	entities := p.mainEntities()
	where := p.whereColumns()
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
					return true
				}
				owner, _ := pkg.TypesInfo.Defs[spec.Name].(*types.TypeName)
				for _, fld := range st.Fields.List {
					for _, nm := range fld.Names {
						if c, ok := p.scoreKey(pkg, owner, spec.Name.Name, nm, fld, evidence, entities, where); ok {
							out = append(out, c)
						}
					}
				}
				return true
			})
		}
	}
	out = withoutGenerated(out, p.generated, func(c fact.Candidate) string { return c.File })
	out.Sort()

	byName := map[string]string{}
	for obj, state := range entities {
		byName[obj.Name()] = state
	}
	return out, byName
}

func (p *Program) scoreKey(pkg *packages.Package, owner *types.TypeName, ownerName string, nm *ast.Ident, fld *ast.Field,
	evidence map[*types.Var]*keyEvidence, entities map[*types.TypeName]string, where map[[2]string]bool) (fact.Candidate, bool) {

	if !canHoldAKey(pkg.TypesInfo, fld) {
		return fact.Candidate{}, false
	}
	v, _ := pkg.TypesInfo.Defs[nm].(*types.Var)
	ev := evidence[v]
	if ev == nil {
		ev = &keyEvidence{}
	}

	table, col := ev.table, ev.column
	if table == "" && owner != nil {
		if named, ok := owner.Type().(*types.Named); ok {
			table = p.tableOf(named)
		}
	}
	if col == "" {
		col = columnOf(fld, nm.Name)
	}
	cov, unique := p.uniqueness(table, col)
	if !ev.queried && where[[2]string{table, col}] {
		ev.queried, ev.via = true, "a WHERE clause on "+table+"."+col
	}

	nameMatch := naming.IsIdempotencyName(nm.Name)
	if !nameMatch && ev.origin != originExternal && !unique {
		return fact.Candidate{}, false
	}

	c := fact.Candidate{
		Name: nm.Name, Owner: ownerName, Type: types.ExprString(fld.Type),
		File: p.rel(nm.Pos()), Line: p.line(nm.Pos()),
		Table: table, Column: col,
	}
	if nameMatch {
		c.Score++
		c.Declarative = true
		c.Proof = append(c.Proof, "the name is in the idempotency-key vocabulary")
	}
	state := ""
	if owner != nil {
		state = entities[owner]
	}
	switch {
	case state != "" && c.Declarative:
		c.Score += 4
		c.Proof = append(c.Proof, "the field is on "+ownerName+", which carries this flow's state machine, so it is the entity a retry has to match")
	case state != "":
		c.Against = append(c.Against, "it sits on the flow's entity but nothing names it a key, so it reads as an attribute rather than an identifier")
	default:
		c.Score -= 2
		c.Against = append(c.Against, ownerName+" carries no lifecycle state, so it is a message or a config rather than the entity the flow moves")
	}
	switch ev.origin {
	case originExternal:
		c.Score += 3
		c.Proof = append(c.Proof, "the value arrives from outside this process (header, query or request body), which is what a retry can repeat")
	case originGenerated:
		c.Score -= 4
		c.Against = append(c.Against, "the value is generated in this process by a UUID or random source, so a retry produces a different value and it cannot deduplicate anything")
	}
	if ev.queried {
		c.Score += 3
		c.Proof = append(c.Proof, "the code looks rows up by it ("+ev.via+")")
	}
	switch {
	case unique && cov.Partial:
		c.Score += 1
		c.Declarative = true
		c.Proof = append(c.Proof, "a partial unique index covers column \""+col+"\"")
		c.Against = append(c.Against, "uniqueness holds only where "+cov.Constraint.Where+"; a duplicate outside that predicate is accepted")
	case unique && len(cov.Scope) > 0:
		c.Score += 2
		c.Declarative = true
		c.Proof = append(c.Proof, "a unique constraint covers \""+col+"\" together with "+strings.Join(cov.Scope, ", ")+", so it is unique per "+strings.Join(cov.Scope, ", "))
	case unique:
		c.Score += 2
		c.Declarative = true
		c.Proof = append(c.Proof, "a unique constraint on column \""+col+"\" makes the database refuse a duplicate")
	case nameMatch:
		c.Against = append(c.Against, "no constraint makes column \""+col+"\" unique, so nothing stops two concurrent inserts")
	}
	return c, true
}

// uniqueness checks table.column, or any table holding the column when the
// table is not known.
func (p *Program) uniqueness(table, col string) (fact.Coverage, bool) {
	if p.infra.HasTable(table) {
		return p.infra.UniqueOn(table, col)
	}
	for _, t := range p.infra.Tables {
		if cov, ok := p.infra.UniqueOn(t.Name, col); ok {
			return cov, true
		}
	}
	return fact.Coverage{}, false
}

func canHoldAKey(info *types.Info, fld *ast.Field) bool {
	t := info.TypeOf(fld.Type)
	if t == nil {
		return true
	}
	if ptr, ok := t.Underlying().(*types.Pointer); ok {
		t = ptr.Elem()
	}
	basic, ok := t.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	switch basic.Kind() {
	case types.Bool, types.UntypedBool, types.UnsafePointer:
		return false
	}
	return basic.Info()&(types.IsString|types.IsInteger) != 0
}

func (p *Program) keyEvidence() map[*types.Var]*keyEvidence {
	out := map[*types.Var]*keyEvidence{}
	get := func(v *types.Var) *keyEvidence {
		if out[v] == nil {
			out[v] = &keyEvidence{}
		}
		return out[v]
	}
	note := func(v *types.Var, o origin) {
		if v == nil || o == originUnknown {
			return
		}
		if ev := get(v); ev.origin != originGenerated {
			ev.origin = o
		}
	}

	sqlCalls := map[*ast.CallExpr]bool{}
	for _, s := range p.sql {
		sqlCalls[s.call] = true
	}
	for _, pkg := range p.prod {
		info := pkg.TypesInfo
		for _, span := range p.funcTable(pkg) {
			defs := localDefs(info, span.decl.Body)
			originOf := func(e ast.Expr) origin {
				if v := localVar(info, e); v != nil {
					if def, ok := defs[v]; ok {
						return valueOrigin(def)
					}
				}
				return valueOrigin(e)
			}
			ast.Inspect(span.decl.Body, func(n ast.Node) bool {
				switch t := n.(type) {
				case *ast.KeyValueExpr:
					note(fieldVar(info, t.Key), originOf(t.Value))
				case *ast.AssignStmt:
					if len(t.Lhs) == len(t.Rhs) {
						for i, lhs := range t.Lhs {
							note(fieldVar(info, lhs), originOf(t.Rhs[i]))
						}
					}
				case *ast.CallExpr:
					// A call carrying SQL is judged by its WHERE clause below:
					// an INSERT that stores the key does not look anything up.
					if sqlCalls[t] || !callsDB(info, t) && !queryMethod(calleeName(t.Fun)) {
						return true
					}
					for _, arg := range t.Args {
						if v := fieldVar(info, arg); v != nil {
							if ev := get(v); !ev.queried {
								ev.queried, ev.via = true, "it is passed to "+calleeName(t.Fun)
							}
						}
					}
				}
				return true
			})
		}
	}

	for _, s := range p.sql {
		for _, b := range s.binds {
			ev := get(b.field)
			if b.where {
				if !ev.queried {
					ev.queried, ev.via = true, "a WHERE clause binds it to "+b.table+"."+b.column
				}
				continue
			}
			if ev.column == "" {
				ev.column, ev.table = b.column, b.table
			}
		}
	}
	return out
}

func valueOrigin(e ast.Expr) origin {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		if sel, ok := e.(*ast.SelectorExpr); ok && looksLikeRequest(sel.X) {
			return originExternal
		}
		return originUnknown
	}
	if id, ok := call.Fun.(*ast.Ident); ok {
		if generatesIdentity(id.Name) {
			return originGenerated
		}
		return originUnknown
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return originUnknown
	}
	method := sel.Sel.Name
	pkg := ""
	if id, ok := sel.X.(*ast.Ident); ok {
		pkg = strings.ToLower(id.Name)
	}
	switch {
	case pkg == "uuid" || pkg == "ulid" || pkg == "xid" || pkg == "ksuid" || pkg == "snowflake" || pkg == "sonyflake":
		return originGenerated
	case pkg == "rand" && (method == "Int" || method == "Intn" || method == "Int63" || method == "Read"):
		return originGenerated
	case generatesIdentity(method):
		return originGenerated
	}
	switch method {
	case "Get", "FormValue", "PostFormValue", "Query", "Param", "GetHeader", "Header", "Values":
		if looksLikeRequest(sel.X) || headerish(sel.X) {
			return originExternal
		}
	}
	return originUnknown
}

func generatesIdentity(name string) bool {
	n := strings.ToLower(name)
	for _, w := range []string{"uuid", "ulid", "ksuid", "xid", "nanoid", "snowflake", "objectid"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	for _, prefix := range []string{"new", "gen", "generate", "make", "create", "random", "rand"} {
		if strings.HasPrefix(n, prefix) &&
			(strings.Contains(n, "id") || strings.Contains(n, "key") ||
				strings.Contains(n, "ref") || strings.Contains(n, "token") || n == prefix) {
			return true
		}
	}
	return false
}

func looksLikeRequest(e ast.Expr) bool {
	name := ""
	switch x := e.(type) {
	case *ast.Ident:
		name = x.Name
	case *ast.SelectorExpr:
		name = x.Sel.Name
	case *ast.CallExpr:
		if s, ok := x.Fun.(*ast.SelectorExpr); ok {
			name = s.Sel.Name
		}
	}
	n := strings.ToLower(name)
	switch n {
	case "r", "req", "request", "c", "ctx", "in", "input", "cmd", "dto", "payload", "body":
		return true
	}
	return strings.Contains(n, "request") || strings.Contains(n, "header") || strings.Contains(n, "query")
}

func headerish(e ast.Expr) bool {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		switch strings.ToLower(sel.Sel.Name) {
		case "header", "url", "form", "params":
			return true
		}
	}
	return false
}

func queryMethod(name string) bool {
	switch name {
	case "Query", "QueryRow", "QueryContext", "QueryRowContext", "Exec", "ExecContext",
		"Get", "GetContext", "Select", "SelectContext", "Find", "First", "Where", "Model", "One", "Scan":
		return true
	}
	return false
}

// mainEntities maps each struct that carries a lifecycle field to the
// lifecycle type, by type identity.
func (p *Program) mainEntities() map[*types.TypeName]string {
	lifecycle := map[string]bool{}
	for _, m := range p.states {
		lifecycle[m.Type] = true
	}
	out := map[*types.TypeName]string{}
	if len(lifecycle) == 0 {
		return out
	}
	for _, pkg := range p.prod {
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := spec.Type.(*ast.StructType)
				if !ok {
					return true
				}
				obj, _ := pkg.TypesInfo.Defs[spec.Name].(*types.TypeName)
				for _, fld := range st.Fields.List {
					t := pkg.TypesInfo.TypeOf(fld.Type)
					if t == nil {
						continue
					}
					if key := types.TypeString(t, relativeTo); lifecycle[key] && obj != nil {
						out[obj] = key
						return false
					}
				}
				return true
			})
		}
	}
	return out
}

func (p *Program) infraFindings() []fact.Finding {
	if len(p.infra.Tables) == 0 {
		return nil
	}
	var out []fact.Finding
	for _, c := range p.idem {
		if !c.Credible() || !p.infra.HasTable(c.Table) {
			continue
		}
		if _, unique := p.infra.UniqueOn(c.Table, c.Column); unique {
			continue
		}
		// A field the table does not store cannot be made unique there; saying
		// it lacks a constraint would be a claim about a column that is not real.
		if !p.infra.HasColumn(c.Table, c.Column) {
			continue
		}
		detail := "No constraint in the schema makes " + c.Table + "." + c.Column + " unique. Whatever prevents duplicates in Go is therefore a read followed by a write, and two requests arriving together can both pass the read before either writes. Add a unique index and let the database refuse the second one."
		out = append(out, fact.Finding{
			ID: "IDEM-KEY-NOT-UNIQUE", Severity: fact.SevCritical, Basis: fact.Heuristic,
			Title:  c.Owner + "." + c.Name + " is an idempotency key with no unique constraint behind it",
			Detail: detail + "\n\nWhy this field: " + strings.Join(c.Proof, "; ") + ".",
			Ref:    fact.CodeRef{Pkg: p.pkgOfFile(c.File), Symbol: c.Owner, File: c.File, Line: c.Line},
			Line:   c.Line,
		})
	}
	return out
}

func (p *Program) pkgOfFile(file string) string {
	for _, pkg := range p.prod {
		for _, f := range pkg.Syntax {
			if p.rel(f.Pos()) == file {
				return pkg.PkgPath
			}
		}
	}
	return ""
}
