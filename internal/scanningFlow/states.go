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

// extractStateMachines recovers the payment state machine from source.
//
// This is the highest-leverage deterministic extraction in testigo, and it is
// worth being precise about why. A named status type's declared constants are
// the COMPLETE state set — the compiler guarantees there are no others. Every
// assignment of that type is a write site, and go/types has already folded the
// constants for us, so we know which state each write moves to.
//
// What we cannot know statically is which transitions are LEGAL. That is a
// business rule, not a fact about the code. So this function produces the
// states and the writes, and phase 2 asks an agent one narrow question — "which
// of these transitions should be impossible?" — instead of the open-ended and
// far less reliable "explain this code to me". Every illegal transition the
// agent names becomes a generated test.
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
			// Both lists are collected. The weak ones have to earn their place
			// further down, on evidence rather than on their name.
			if !patterns.IsStateType(name) && !patterns.IsWeakStateType(name) {
				continue
			}
			// A protobuf enum is not a lifecycle. ActionType, EventType,
			// SchedulingType and PushState all pass the name test and all come
			// out of a .proto, where the states are a wire format rather than
			// something a payment moves through. On a real gRPC service six of
			// the ten machines found this way were generated, and each one would
			// have bought a round-1 prompt asking which transitions are legal.
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

	// The declared constants are the state set.
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
					// Real code initialises state in a struct literal as often
					// as it assigns it: &Payment{Status: StatusPending}. Only
					// walking AssignStmt would miss the state a payment is
					// CREATED in, which is the one the whole machine starts from.
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
							In: fns.at(kv.Pos()), To: to, Line: pos.Line,
						})
					}
				case *ast.ReturnStmt:
					// `return StatusFailed, nil` produces a state just as
					// surely as `p.Status = StatusFailed` does — the caller
					// stores what it is handed. Walking only AssignStmt and
					// CompositeLit made every status that a function RETURNS
					// look dead, and returning a status is ordinary Go.
					//
					// Call ARGUMENTS are deliberately not here, though they
					// look like the same case. A status passed to a function is
					// ambiguous: it is as likely to be a query filter as a
					// write. The fixture proves it —
					//
					//   QueryContext(ctx, "... WHERE status = $1", StatusPending)
					//
					// reads payments in a state; it does not put one there.
					// Counting that would not just inflate the count, it would
					// record the WRONG FUNCTION as the write site, and the
					// transitions prompt hands those write sites to an agent as
					// fact. A missed write costs one noisy finding. A false
					// write teaches the agent something untrue.
					for _, r := range t.Results {
						addWrite(p, fns, isLifecycleType, writes, relativeTo, r)
					}
				case *ast.AssignStmt:
					for i, lhs := range t.Lhs {
						if i >= len(t.Rhs) {
							continue
						}

						// The left side of a short declaration is a DEFINITION,
						// so go/types files it under Defs and TypesInfo.Types
						// has nothing for it. That made `kind := StatusPending`
						// invisible while `p.Status = StatusPending` was seen —
						// same statement, two spellings, one of them ignored.
						//
						// Falling back to the right-hand side covers both, and
						// covers assignment through an alias as a bonus.
						tv, ok := p.TypesInfo.Types[lhs]
						if !ok || !isLifecycleType(types.TypeString(tv.Type, relativeTo)) {
							addWrite(p, fns, isLifecycleType, writes, relativeTo, t.Rhs[i])
							continue
						}
						key := types.TypeString(tv.Type, relativeTo)
						to := "<dynamic>"
						if rv, ok := p.TypesInfo.Types[t.Rhs[i]]; ok && rv.Value != nil {
							to = constName(p, t.Rhs[i], rv.Value.String())
						}
						pos := p.Fset.Position(t.Pos())
						writes[key] = append(writes[key], flowEntity.StateWrite{
							In: fns.at(t.Pos()), To: to, Line: pos.Line,
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
			_ = why // reported through the weak list, not as a machine
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

// isLifecycle decides whether an enum is a STATE MACHINE or just an enum.
//
// A named string or integer type with declared constants is how Go writes an
// enum, and payment lifecycles are written that way. So is everything else:
// ErrorCode, TransactionKind, SettlementMode. Asking an agent which transitions
// of an ErrorCode are illegal is nonsense that costs money, and never asking
// about SettlementMode misses a real machine in some repositories.
//
// The name alone cannot separate them, so the name is only the tiebreaker. What
// actually separates a lifecycle from an enum is BEHAVIOUR:
//
//   - a lifecycle is PERSISTED. It lives in a struct field, because the whole
//     point is that it survives between requests.
//   - a lifecycle is REASSIGNED, in more than one place. A payment moves from
//     pending to authorised in one function and to captured in another. An
//     ErrorCode is returned and compared, rarely stored and updated.
//
// A strong name passes on its own, because "PaymentStatus" is not ambiguous and
// demanding evidence would drop real machines in repositories that keep their
// transitions in one place. A weak name has to show both behaviours.
func isLifecycle(qualified, simple, field string, writes []flowEntity.StateWrite) (string, bool) {
	distinct := map[string]bool{}
	for _, w := range writes {
		distinct[w.In.ID()] = true
	}

	// A strong name skips the storage and two-writer rules, because a type
	// called PaymentStatus is a lifecycle even in a package that only sets it
	// once. It does NOT skip this one.
	//
	// A type that nothing anywhere ever assigns is not a lifecycle whatever it
	// is called. testigo's own `Phase` is the example: two constants, a
	// lifecycle-sounding name, and the only thing the code does with them is
	// print them. Admitting it produced a state machine with zero write sites,
	// a STATE-NEVER-SET finding for each constant, and a round-1 prompt asking
	// an agent which transitions between phases are legal — which is the
	// ErrorCode mistake this package exists to avoid, arriving through the one
	// door that skips the checks.
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

// constName prefers the constant's identifier over its literal value, because
// PaymentCaptured reads better in a report than "captured".
func constName(p *packages.Package, e ast.Expr, fallback string) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return strings.Trim(fallback, `"`)
}

// funcTable maps a position back to the function that contains it, so a finding
// deep inside a body can be anchored to a symbol rather than to a bare line
// number. A line number goes stale the moment someone adds an import; a symbol
// does not.
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

// at finds the enclosing function by binary search over the sorted spans.
func (ft funcTable) at(pos token.Pos) codeRef.CodeRef {
	i := sort.Search(len(ft), func(i int) bool { return ft[i].end >= pos })
	if i < len(ft) && ft[i].start <= pos && pos < ft[i].end {
		return ft[i].a
	}
	return codeRef.CodeRef{} // package-level declaration, not inside any function
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

// addWrite records one expression as a site that produces a lifecycle state.
//
// Shared by the return, call-argument and composite-literal cases so all three
// agree on what counts and how it is named. An expression whose value the type
// checker cannot fold to a constant is recorded as <dynamic>: the state was
// produced, we just cannot say which one, and that is still not "never".
func addWrite(
	p *packages.Package,
	fns funcTable,
	isLifecycleType func(string) bool,
	writes map[string][]flowEntity.StateWrite,
	relativeTo types.Qualifier,
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
		In: fns.at(e.Pos()), To: to, Line: pos.Line,
	})
}
