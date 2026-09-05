package scanningFlow

import (
	"fmt"
	"go/ast"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/katayunak/testigo/internal/codeRef"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"golang.org/x/tools/go/packages"
)

// Options is the scanningFlow's configuration
type Options struct {
	Root    string
	Entries []flowEntity.EntryPoint // where the payment flow starts
}

type Result struct {
	Flow  *flowEntity.Flow
	Index *codeRef.Index
	Local map[string]bool // repository's internal package imports (no dependencies)
}

// the info needed from packages pkg, almost everything possible
const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax |
	packages.NeedTypesInfo | packages.NeedModule

func Scan(opts Options) (*Result, error) {
	// GOWORK=off, always.
	//
	// A go.work outside the repository can change package loading and make the
	// scan produce different results.
	cfg := &packages.Config{
		Mode:  loadMode,
		Dir:   opts.Root,
		Tests: false,
		Env:   append(os.Environ(), "GOWORK=off"),
	}

	// getting all comprehensive AST info we need
	pkgs, err := packages.Load(cfg, []string{"./..."}...)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}

	// Real repositories often have a package that will not build for reasons unrelated to the payment flow,
	// and refusing to analyze anything because of it would make testigo useless exactly where it is most needed.
	var hardErrs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			// collecting errors instead of returning at the first one
			hardErrs = append(hardErrs, fmt.Sprintf("%s: %s", p.PkgPath, e))
		}
	})
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages matched %v in %s",
			[]string{"./..."}, opts.Root)
	}

	local := map[string]bool{}
	moduleName := ""
	for _, p := range pkgs {
		local[p.PkgPath] = true // ./... is passed as patterns when getting all packages, and means all in this repo
		// so these are all internal packages
		// this map is used later for separation
		if p.Module != nil && moduleName == "" {
			moduleName = p.Module.Path
		}
	}

	flow := flowEntity.NewFlow(moduleName)
	flow.GeneratedAt = time.Now().Local().Format(time.RFC3339)
	flow.Entries = opts.Entries

	// populating the index
	ix := codeRef.NewIndex()
	decls := map[string]*declRef{}

	// entering every package
	for _, p := range pkgs {
		// entering every file
		for _, file := range p.Syntax {
			// entering every declaration
			for _, declaration := range file.Decls {
				// we're in the heart of the declaration
				functionDeclaration, ok := declaration.(*ast.FuncDecl)
				if !ok || functionDeclaration.Body == nil {
					continue
				}

				// creating new codeRef for each node(function)
				newCodeRef := codeRef.NewCodeRef(p.PkgPath, opts.Root, p.Fset, functionDeclaration)
				_, astNodesCounts := codeRef.StructuralHash(functionDeclaration)
				ix.Add(newCodeRef, astNodesCounts)
				decls[newCodeRef.ID()] = &declRef{codeRef: newCodeRef, decl: functionDeclaration, pkg: p}
			}
		}
	}

	g, err := buildGraph(pkgs, local)
	if err != nil {
		return nil, err
	}

	if err := g.discover(opts.Root, opts.Entries, flow); err != nil {
		return nil, err
	}

	// Machine-written files keep their place in the graph but lose the right to
	// raise findings or contribute patterns. See generated.go for why.
	gen := generatedFiles(pkgs, opts.Root)

	flow.Infra = Infra(opts.Root)
	flow.Docs = FindDocs(opts.Root)
	// States FIRST. The candidate scorers ask which structs carry a lifecycle
	// state, and a struct that carries one is the entity the flow moves — the
	// single strongest signal either scorer has. Computing candidates first left
	// that set empty and silently threw the signal away.
	flow.States = extractStateMachines(pkgs, opts.Root, local, gen)
	linkStateWritesToNodes(flow)

	flow.IdempotencyKeys = idempotencyCandidates(pkgs, flow, opts.Root, local)
	flow.MoneyTypes = moneyCandidates(pkgs, flow, opts.Root, local)

	// A candidate from a .pb.go is a protobuf request field, not a decision this
	// repository made. Dropping them here rather than at the finding keeps them
	// out of the round-1 prompts too, which is where they would have cost money.
	fileOfCandidate := func(c flowEntity.Candidate) string { return c.File }
	flow.IdempotencyKeys, _ = withoutGenerated(flow.IdempotencyKeys, gen, fileOfCandidate)
	flow.MoneyTypes, _ = withoutGenerated(flow.MoneyTypes, gen, fileOfCandidate)

	flow.Findings = append(flow.Findings, stateFindings(flow.States)...)
	flow.Findings = append(flow.Findings, moneyFindings(pkgs, opts.Root)...)
	flow.Findings = append(flow.Findings, structuralFindings(flow)...)
	flow.Findings = append(flow.Findings, infraFindings(pkgs, flow, opts.Root)...)

	flow.Findings, flow.GeneratedFindings = withoutGenerated(
		flow.Findings, gen,
		func(f flowEntity.Finding) string { return f.Ref.File },
	)
	flow.GeneratedFiles = len(gen)

	for _, e := range hardErrs {
		flow.Findings = append(flow.Findings, flowEntity.Finding{
			ID: "LOAD-ERROR", Severity: flowEntity.SevInfo,
			Title:  "package did not type-check",
			Detail: e + " — results for this package are incomplete",
		})
	}

	sortFindings(flow.Findings)
	return &Result{Flow: flow, Index: ix, Local: local}, nil
}

type declRef struct {
	codeRef codeRef.CodeRef
	decl    *ast.FuncDecl
	pkg     *packages.Package
}

// linkStateWritesToNodes joins the state machine back onto the call graph.
//
// The two are extracted separately: the graph walk knows which functions the
// flow reaches, and the state extractor knows where the status is assigned.
// Neither knows about the other, so until they are joined, flowEntity.Facts.WritesStatus
// and flowEntity.StateWrite.InTx sit empty and the report cannot answer the question that
// matters most about a payment state machine:
//
//	is the status change committed in the same transaction as the money?
//
// A status write in a function that never opened a transaction means the row
// and the ledger can disagree if the process dies between them. That is the
// shape of a real production incident, and it is visible here for free.
func linkStateWritesToNodes(f *flowEntity.Flow) {
	for mi := range f.States {
		for wi := range f.States[mi].Writes {
			write := &f.States[mi].Writes[wi]
			node, ok := f.Nodes[write.In.ID()]
			if !ok {
				continue // the status is written outside any reachable flow
			}
			write.InTx = node.Facts.OpensTx
			node.Facts.WritesStatus = appendUnique(node.Facts.WritesStatus, write.To)
		}
	}
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

// structuralFindings are conclusions drawn from the assembled graph rather than
// from any single expression. They are still deterministic.
func structuralFindings(f *flowEntity.Flow) []flowEntity.Finding {
	var out []flowEntity.Finding

	// A seam that is not behind an interface cannot be made to fail on demand.
	// Since almost every serious payment bug only shows up when something
	// downstream fails, an uninjectable seam is a hole in the test plan, and
	// saying so is more useful than pretending we can test around it.
	byFn := map[string][]flowEntity.Seam{}
	for _, s := range f.Seams {
		if !s.Injectable {
			byFn[s.In.ID()] = append(byFn[s.In.ID()], s)
		}
	}
	for id, seams := range byFn {
		kinds := map[flowEntity.SeamKind]bool{}
		for _, s := range seams {
			kinds[s.Kind] = true
		}
		var ks []string
		for k := range kinds {
			ks = append(ks, string(k))
		}
		sort.Strings(ks)
		out = append(out, flowEntity.Finding{
			ID: "SEAM-CONCRETE", Severity: flowEntity.SevHigh,
			Title:  "I/O called on a concrete type, so faults cannot be injected",
			Detail: fmt.Sprintf("%d call(s) to %s go straight to a concrete type. A test cannot make them time out, fail, or return a duplicate, which is where payment bugs live.", len(seams), strings.Join(ks, ", ")),
			Ref:    seams[0].In,
			Line:   seams[0].Line,
		})
		_ = id
	}

	// Opening a transaction with no rollback anywhere in the same function is a
	// leak on the error path
	for _, n := range f.Nodes {
		if n.Facts.OpensTx && !n.Facts.RollsBackTx {
			out = append(out, flowEntity.Finding{
				ID: "TX-NO-ROLLBACK", Severity: flowEntity.SevHigh,
				Title:  "transaction opened with no rollback in the same function",
				Detail: "An early return between BEGIN and COMMIT leaves the transaction open. Idiomatic Go pairs the Begin with an immediate `defer tx.Rollback()`, which is a no-op after a successful commit.",
				Ref:    n.Ref, Line: n.Ref.Line,
			})
		}
		if n.Facts.OpensTx && n.Facts.TouchesNet {
			out = append(out, flowEntity.Finding{
				ID: "TX-NET-CALL", Severity: flowEntity.SevCritical,
				Title:  "network call inside a database transaction",
				Detail: "Holding a transaction open across a network round trip pins locks for the duration of someone else's timeout. Under load this turns a slow provider into a database outage, and a retry of the outer request can double-apply the effect.",
				Ref:    n.Ref, Line: n.Ref.Line,
			})
		}
	}
	return out
}

// stateFindings reports states the code declares but never enters.
//
// This is a genuinely useful deterministic result. A declared-but-unwritten
// state is one of three things, and all three are worth a human looking at:
// dead code left over from a removed feature, a state that is only ever set by
// a SQL migration or an operations runbook (so it exists in the database but
// not in the code that has to handle it), or a transition someone forgot to
// implement. The tool cannot tell which, and says so rather than guessing.
func stateFindings(ms []flowEntity.StateMachine) []flowEntity.Finding {
	var out []flowEntity.Finding
	for _, m := range ms {
		for _, st := range m.NeverAssigned {
			out = append(out, flowEntity.Finding{
				ID: "STATE-NEVER-SET", Severity: flowEntity.SevMedium,
				Title:  m.Type + "." + st + " is declared but nothing in this module produces it",
				Detail: "No assignment, struct literal, return statement or call argument anywhere in the scanned packages produces this state. Either it is dead, or something outside this code — a migration, a manual fix, another service — puts payments into it. If the second, every read path has to handle a state no write path here produces, and no test currently covers that.",
				Ref:    codeRef.CodeRef{Pkg: pkgOf(m.Type), Symbol: "const " + st},
			})
		}
	}
	return out
}

func pkgOf(qualified string) string {
	if i := strings.LastIndex(qualified, "."); i >= 0 {
		return qualified[:i]
	}
	return qualified
}

// infraFindings compares what the Go code assumes against what the database
// actually enforces.
//
// This is the cheapest high-value check in testigo, and it needs no agent. If a
// struct has an idempotency key field and no migration makes that column unique,
// then whatever the application code does to prevent duplicates is a check
// followed by a write, and two concurrent requests can both pass the check
// before either writes. That is a real double-spend window, provable from two
// files neither of which mentions the other.
func infraFindings(pkgs []*packages.Package, f *flowEntity.Flow, root string) []flowEntity.Finding {
	var out []flowEntity.Finding
	if len(f.Infra.MigrationDirs) == 0 {
		return nil // no migrations to compare against; say nothing rather than guess
	}

	for _, p := range pkgs {
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
						// Ask the scorer, not the name.
						//
						// This used to be a regex on nm.Name, which made the
						// loudest finding in the tool disagree with the most
						// careful analysis in it: TraceID scored -3 ("generated
						// in this process, so a retry produces a different
						// value") and was still reported as a critical missing
						// unique index. Two detectors, one opinion each, and
						// the wrong one had the megaphone.
						//
						// Now a finding needs the same proof a decision
						// needs. A field that only matched the vocabulary
						// scores 1 and says nothing.
						cand, scored := f.IdempotencyKeys.Find(spec.Name.Name, nm.Name)
						if !scored || !cand.Credible() {
							continue
						}
						col := columnOf(fld, nm.Name)
						if _, covered := f.Infra.CoversColumn("", col); covered {
							continue
						}
						pos := p.Fset.Position(fld.Pos())
						out = append(out, flowEntity.Finding{
							ID: "IDEM-KEY-NOT-UNIQUE", Severity: flowEntity.SevCritical,
							Title:  spec.Name.Name + "." + nm.Name + " is an idempotency key with no unique constraint behind it",
							Detail: "The migrations in " + strings.Join(f.Infra.MigrationDirs, ", ") + " create no UNIQUE index covering column \"" + col + "\". Whatever prevents duplicates in Go is therefore a read followed by a write, and two requests arriving together can both pass the read before either writes. Add a unique index and let the database refuse the second one.\n\nWhy this field: " + strings.Join(cand.Proof, "; ") + ".",
							Ref:    flowEntity.CodeRefOf(p.PkgPath, spec.Name.Name, relPath(p.Fset, fld.Pos(), root), pos.Line),
							Line:   pos.Line,
						})
					}
				}
				return true
			})
		}
	}
	return out
}

// columnOf reads the db column name from a struct tag, falling back to the
// snake_case of the field name, which is what every Go ORM defaults to.
func columnOf(fld *ast.Field, fieldName string) string {
	if fld.Tag != nil {
		tag := strings.Trim(fld.Tag.Value, "`")
		for _, key := range []string{"db", "sql", "pg", "gorm", "json"} {
			if v := reflect.StructTag(tag).Get(key); v != "" {
				name := strings.Split(v, ",")[0]
				name = strings.TrimPrefix(name, "column:")
				if name != "" && name != "-" {
					return name
				}
			}
		}
	}
	return snakeCase(fieldName)
}

// snakeCase converts a Go field name to the column name an ORM would default to.
//
// The naive version put an underscore before every capital, which turns OrderID
// into order_i_d and silently breaks every lookup against the migrations. Runs of
// capitals are acronyms — ID, URL, HTTP — and only the boundary between the
// acronym and the next word gets a separator.
func snakeCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		upper := r >= 'A' && r <= 'Z'
		if upper && i > 0 {
			prevLower := runes[i-1] >= 'a' && runes[i-1] <= 'z'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			// aB -> a_b   (word boundary)
			// ABc -> a_bc (end of an acronym, start of a word)
			if prevLower || nextLower {
				b.WriteByte('_')
			}
		}
		if upper {
			r += 32
		}
		b.WriteRune(r)
	}
	return b.String()
}

var sevRank = map[flowEntity.Severity]int{
	flowEntity.SevCritical: 0, flowEntity.SevHigh: 1, flowEntity.SevMedium: 2, flowEntity.SevInfo: 3,
}

func sortFindings(fs []flowEntity.Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if sevRank[fs[i].Severity] != sevRank[fs[j].Severity] {
			return sevRank[fs[i].Severity] < sevRank[fs[j].Severity]
		}
		if fs[i].ID != fs[j].ID {
			return fs[i].ID < fs[j].ID
		}
		if fs[i].Ref.ID() != fs[j].Ref.ID() {
			return fs[i].Ref.ID() < fs[j].Ref.ID()
		}
		return fs[i].Line < fs[j].Line
	})
}
