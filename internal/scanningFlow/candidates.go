package scanningFlow

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/scanningFlow/patterns"
)

// origin is where a field's value comes from. It is the strongest single signal
// available about what a field IS, and it is completely invisible to a name.
type origin int

const (
	originUnknown origin = iota
	// originExternal: the value arrived from outside this process — a header, a
	// query parameter, a form value, a request body field.
	originExternal
	// originGenerated: the value was created here, by a UUID library or a random
	// source.
	originGenerated
)

// idempotencyCandidates ranks the fields that might be the idempotency key.
//
// Name matching alone cannot do this, and the failing case is ordinary rather
// than exotic. An Order carrying ID, ReferenceID, TraceID and IdempotencyKey has
// four fields in the vocabulary, and picking by name is a coin flip.
//
// What separates them is BEHAVIOUR, and three behaviours are visible in source:
//
//  1. Where the value comes from. An idempotency key is supplied by the CLIENT.
//     A field assigned from uuid.New() in this process cannot be one — the whole
//     point is that a retry sends the same value, and a value generated per
//     attempt is different every attempt. This single check eliminates most
//     false candidates, and it is the one a name can never give you.
//  2. Whether the code looks it up before doing the work. A key that is stored
//     but never read before the money moves prevents nothing.
//  3. Whether the database enforces uniqueness on it. Already known from the
//     migrations, at no cost.
func idempotencyCandidates(pkgs []*packages.Package, f *flowEntity.Flow, root string, local map[string]bool) flowEntity.Candidates {
	origins := fieldOrigins(pkgs, local)
	queried := fieldsPassedToQueries(pkgs, local)

	var out flowEntity.Candidates
	for _, p := range pkgs {
		if !local[p.PkgPath] {
			continue
		}
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := spec.Type.(*ast.StructType)
				if !ok {
					return true
				}
				for _, fld := range st.Fields.List {
					for _, nm := range fld.Names {
						c, ok := scoreIdempotency(p, root, spec.Name.Name, nm, fld, origins, queried, f)
						if ok {
							out = append(out, c)
						}
					}
				}
				return true
			})
		}
	}
	out.Sort()
	return out
}

func scoreIdempotency(p *packages.Package, root, owner string, nm *ast.Ident, fld *ast.Field,
	origins map[string]origin, queried map[string]bool, f *flowEntity.Flow) (flowEntity.Candidate, bool) {

	nameMatch := patterns.IdempotencyField.MatchString(nm.Name)
	key := owner + "." + nm.Name
	org := origins[key]
	if org == originUnknown {
		org = origins[nm.Name] // same field name, assigned somewhere we could not tie to the type
	}

	// A dedup key has to CARRY a value that a retry can repeat, so the type has
	// to be able to hold one. `AcceptsDedupKey bool` and `DedupKeyArgument
	// []string` both match the name vocabulary and neither is a key. Filtering
	// here rather than at the finding keeps them out of the prompts too.
	if !canHoldAKey(p, fld) {
		return flowEntity.Candidate{}, false
	}

	// A field is only worth scoring if SOMETHING points at it. A name match, an
	// external origin, or a unique index each qualify; nothing at all does not.
	col := columnOf(fld, nm.Name)
	_, unique := f.Infra.CoversColumn("", col)
	if !nameMatch && org != originExternal && !unique {
		return flowEntity.Candidate{}, false
	}

	pos := p.Fset.Position(nm.Pos())
	c := flowEntity.Candidate{
		Name: nm.Name, Owner: owner,
		Type: types.ExprString(fld.Type),
		File: relPath(p.Fset, nm.Pos(), root), Line: pos.Line,
	}

	if nameMatch {
		c.Score++
		c.Evidence = append(c.Evidence, "the name is in the idempotency-key vocabulary")
	}
	switch org {
	case originExternal:
		c.Score += 3
		c.Evidence = append(c.Evidence, "the value arrives from outside this process (header, query or request body), which is what a retry can repeat")
	case originGenerated:
		c.Score -= 4
		c.Against = append(c.Against, "the value is generated in this process by a UUID or random source, so a retry produces a DIFFERENT value and it cannot deduplicate anything")
	}
	if queried[key] || queried[nm.Name] {
		c.Score += 3
		c.Evidence = append(c.Evidence, "it is passed to a database call, so the code looks it up rather than only storing it")
	}
	if unique {
		c.Score += 2
		c.Evidence = append(c.Evidence, "a migration puts a UNIQUE constraint on column \""+col+"\", so the database refuses a duplicate")
	} else if nameMatch {
		c.Against = append(c.Against, "no migration makes column \""+col+"\" unique, so nothing stops two concurrent inserts")
	}
	return c, true
}

// fieldOrigins walks every assignment and struct literal in the module and
// records where each field's value came from.
func fieldOrigins(pkgs []*packages.Package, local map[string]bool) map[string]origin {
	out := map[string]origin{}
	note := func(field string, o origin) {
		// Generated beats external: if a field is ever assigned from a UUID
		// source, it cannot be a client-supplied key, whatever else assigns it.
		if out[field] == originGenerated {
			return
		}
		out[field] = o
	}
	for _, p := range pkgs {
		if !local[p.PkgPath] {
			continue
		}
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				switch t := n.(type) {
				case *ast.KeyValueExpr:
					if k, ok := t.Key.(*ast.Ident); ok {
						if o := originOf(t.Value); o != originUnknown {
							note(k.Name, o)
						}
					}
				case *ast.AssignStmt:
					for i, lhs := range t.Lhs {
						if i >= len(t.Rhs) {
							break
						}
						name := ""
						switch l := lhs.(type) {
						case *ast.SelectorExpr:
							name = l.Sel.Name
						case *ast.Ident:
							name = l.Name
						}
						if name == "" {
							continue
						}
						if o := originOf(t.Rhs[i]); o != originUnknown {
							note(name, o)
						}
					}
				}
				return true
			})
		}
	}
	return out
}

// originOf classifies one expression.
func originOf(e ast.Expr) origin {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		// r.Header, req.IdempotencyKey and friends: a field read off something
		// that looks like an inbound request.
		if sel, ok := e.(*ast.SelectorExpr); ok && looksLikeRequest(sel.X) {
			return originExternal
		}
		return originUnknown
	}
	// A bare call: uuid(), newID(), generateRef(). Local helpers wrapping a
	// generator are extremely common, and missing them was a real bug — an ID
	// assigned from a local uuid() helper scored as high as the genuine key.
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
	case "Get", "FormValue", "PostFormValue", "Query", "Param", "GetHeader", "Header":
		if looksLikeRequest(sel.X) || headerish(sel.X) {
			return originExternal
		}
	}
	return originUnknown
}

// generatesIdentity matches a function that MAKES an identifier rather than
// receiving one. A value produced here is different on every attempt, so it can
// never deduplicate a retry, whatever it is named.
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
	for _, want := range []string{"r", "req", "request", "c", "ctx", "in", "input", "cmd", "dto", "payload", "body"} {
		if n == want {
			return true
		}
	}
	return strings.Contains(n, "request") || strings.Contains(n, "header") || strings.Contains(n, "query")
}

func headerish(e ast.Expr) bool {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		n := strings.ToLower(sel.Sel.Name)
		return n == "header" || n == "url" || n == "form" || n == "params"
	}
	return false
}

// fieldsPassedToQueries records fields handed to a database call. A key that is
// only ever stored, never looked up, prevents nothing.
func fieldsPassedToQueries(pkgs []*packages.Package, local map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, p := range pkgs {
		if !local[p.PkgPath] {
			continue
		}
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !queryMethod(sel.Sel.Name) {
					return true
				}
				for _, arg := range call.Args {
					switch a := arg.(type) {
					case *ast.SelectorExpr:
						out[a.Sel.Name] = true
					case *ast.Ident:
						out[a.Name] = true
					}
				}
				return true
			})
		}
	}
	return out
}

func queryMethod(name string) bool {
	switch name {
	case "Query", "QueryRow", "QueryContext", "QueryRowContext", "Exec", "ExecContext",
		"Get", "Select", "Find", "First", "Where", "Model", "One", "Scan":
		return true
	}
	return false
}

// moneyCandidates ranks the types that carry an amount of money.
//
// Round one used to ask an agent "which type is money?" while this package was
// already computing the answer for its own findings. That was paying for
// something we had. Now phase 1 ranks the candidates and the prompt only asks
// when the ranking is genuinely close.
//
// Representation is not asked at all any more. Whether an amount is an integer,
// a float or a decimal is a fact in the type, and the type checker knows it.
func moneyCandidates(pkgs []*packages.Package, f *flowEntity.Flow, root string, local map[string]bool) flowEntity.Candidates {
	var out flowEntity.Candidates
	for _, p := range pkgs {
		if !local[p.PkgPath] {
			continue
		}
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := spec.Type.(*ast.StructType)
				if !ok {
					// A named scalar type called Money, Amount, Cents: money by
					// construction, and the strongest candidate there is.
					if patterns.MoneyType.MatchString(spec.Name.Name) {
						pos := p.Fset.Position(spec.Pos())
						out = append(out, flowEntity.Candidate{
							Name: spec.Name.Name, Owner: p.PkgPath,
							Type:  types.ExprString(spec.Type),
							File:  relPath(p.Fset, spec.Pos(), root),
							Line:  pos.Line,
							Score: 5,
							Evidence: []string{
								"a named type whose name means money",
								"declared as " + types.ExprString(spec.Type),
							},
						})
					}
					return true
				}
				hasCurrency := false
				for _, fld := range st.Fields.List {
					for _, nm := range fld.Names {
						if patterns.CurrencyField.MatchString(nm.Name) {
							hasCurrency = true
						}
					}
				}
				for _, fld := range st.Fields.List {
					for _, nm := range fld.Names {
						if !patterns.MoneyField.MatchString(nm.Name) {
							continue
						}
						tv, ok := p.TypesInfo.Types[fld.Type]
						if !ok {
							continue
						}
						pos := p.Fset.Position(nm.Pos())
						c := flowEntity.Candidate{
							Name: nm.Name, Owner: spec.Name.Name,
							Type: types.ExprString(fld.Type),
							File: relPath(p.Fset, nm.Pos(), root), Line: pos.Line,
							Score:    2,
							Evidence: []string{"the field name means an amount"},
						}
						switch {
						case isFloat(tv.Type):
							c.Score -= 1
							c.Against = append(c.Against, "stored in a float, which cannot represent 0.10 exactly — this is already reported as MONEY-FLOAT")
						case isInteger(tv.Type) && !isBasicNamed(tv.Type):
							c.Score += 3
							c.Evidence = append(c.Evidence, "a named integer type, so minor units with somewhere to hang the rules")
						case isInteger(tv.Type):
							c.Score += 2
							c.Evidence = append(c.Evidence, "a plain integer, so minor units")
						}
						if hasCurrency {
							c.Score += 2
							c.Evidence = append(c.Evidence, "a currency field sits beside it in the same struct")
						} else {
							c.Against = append(c.Against, "no currency field in the same struct — an amount alone is a number, not money")
						}
						out = append(out, c)
					}
				}
				return true
			})
		}
	}
	out.Sort()
	return out
}

// canHoldAKey reports whether a field's type could carry a deduplication key.
//
// Strings and integers can. Everything else — bools, slices, maps, structs,
// funcs, channels — cannot, whatever the field is called. Named types are
// followed to their underlying type so a `type IdempotencyKey string` still
// counts.
func canHoldAKey(p *packages.Package, fld *ast.Field) bool {
	if p.TypesInfo == nil {
		return true // no type information: do not filter on a guess
	}
	tv, ok := p.TypesInfo.Types[fld.Type]
	if !ok {
		return true
	}

	t := tv.Type
	if ptr, isPtr := t.Underlying().(*types.Pointer); isPtr {
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
