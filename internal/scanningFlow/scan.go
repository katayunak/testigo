package scanningFlow

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"
	"time"

	"github.com/katayunak/testigo/internal/codeRef"
	"golang.org/x/tools/go/packages"
)

// Options is how should we do scanningFlow? the scanningFlow's configuration
type Options struct {
	Root     string
	Patterns []string     // package patterns, default ./...
	Entries  []EntryPoint // where the payment flow starts

	// Env controls the environment used when loading the repository.
	// Leave nil to inherit the current process environment.

	// In particular, GOWORK=off can be used to prevent Go from picking up an
	// unrelated go.work file above the repository.
	// Without this, `go list` may load the target repository as part of the wrong workspace and silently
	// exclude packages, making valid entry points appear to be missing.
	Env []string
}

// Result is the scanningFlow output plus the reference index, which the caller needs in
// order to merge the previous run's notes
type Result struct {
	Flow  *Flow
	Index *codeRef.Index
	// Loaded package paths belonging to the target module. Used to decide what
	// counts as "our code" versus a dependency.
	// That's important when the graph encounters dependencies.
	Local map[string]bool
}

// Don't just give me filenames; Give me enough information to perform serious static analysis:
const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax |
	packages.NeedTypesInfo | packages.NeedModule

func Scan(opts Options) (*Result, error) {
	if len(opts.Patterns) == 0 {
		opts.Patterns = []string{"./..."} // meaning everything underneath is the part of repo
	}

	if len(opts.Env) == 0 {
		opts.Env = []string{
			"GOWORK=off",
		}
	}

	cfg := &packages.Config{Mode: loadMode, Dir: opts.Root, Tests: false, Env: opts.Env}

	// getting all comprehensive AST info we need
	pkgs, err := packages.Load(cfg, opts.Patterns...)
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
		return nil, fmt.Errorf("no packages matched %v in %s "+
			"(if a go.work file sits above this directory, try Env with GOWORK=off)",
			opts.Patterns, opts.Root)
	}

	local := map[string]bool{}
	moduleName := ""
	for _, p := range pkgs {
		local[p.PkgPath] = true
		if p.Module != nil && moduleName == "" {
			moduleName = p.Module.Path
		}
	}

	flow := NewFlow(moduleName)
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
	if err := g.walk(opts.Root, opts.Entries, flow); err != nil {
		return nil, err
	}

	flow.Machines = extractStateMachines(pkgs, opts.Root, local)
	flow.Findings = append(flow.Findings, stateFindings(flow.Machines)...)
	flow.Findings = append(flow.Findings, moneyFindings(pkgs, opts.Root)...)
	flow.Findings = append(flow.Findings, structuralFindings(flow)...)

	for _, e := range hardErrs {
		flow.Findings = append(flow.Findings, Finding{
			ID: "LOAD-ERROR", Severity: SevInfo,
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

// structuralFindings are conclusions drawn from the assembled graph rather than
// from any single expression. They are still deterministic.
func structuralFindings(f *Flow) []Finding {
	var out []Finding

	// A seam that is not behind an interface cannot be made to fail on demand.
	// Since almost every serious payment bug only shows up when something
	// downstream fails, an uninjectable seam is a hole in the test plan, and
	// saying so is more useful than pretending we can test around it.
	byFn := map[string][]Seam{}
	for _, s := range f.Seams {
		if !s.Injectable {
			byFn[s.In.ID()] = append(byFn[s.In.ID()], s)
		}
	}
	for id, seams := range byFn {
		kinds := map[SeamKind]bool{}
		for _, s := range seams {
			kinds[s.Kind] = true
		}
		var ks []string
		for k := range kinds {
			ks = append(ks, string(k))
		}
		sort.Strings(ks)
		out = append(out, Finding{
			ID: "SEAM-CONCRETE", Severity: SevHigh,
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
			out = append(out, Finding{
				ID: "TX-NO-ROLLBACK", Severity: SevHigh,
				Title:  "transaction opened with no rollback in the same function",
				Detail: "An early return between BEGIN and COMMIT leaves the transaction open. Idiomatic Go pairs the Begin with an immediate `defer tx.Rollback()`, which is a no-op after a successful commit.",
				Ref:    n.Ref, Line: n.Ref.Line,
			})
		}
		if n.Facts.OpensTx && n.Facts.TouchesNet {
			out = append(out, Finding{
				ID: "TX-NET-CALL", Severity: SevCritical,
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
func stateFindings(ms []StateMachine) []Finding {
	var out []Finding
	for _, m := range ms {
		for _, st := range m.Terminal {
			out = append(out, Finding{
				ID: "STATE-NEVER-SET", Severity: SevMedium,
				Title:  m.Type + "." + st + " is declared but never assigned in this module",
				Detail: "Either it is dead, or something outside this code — a migration, a manual fix, another service — puts payments into it. If the second, every read path has to handle a state no write path here produces, and no test currently covers that.",
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

var sevRank = map[Severity]int{
	SevCritical: 0, SevHigh: 1, SevMedium: 2, SevInfo: 3,
}

func sortFindings(fs []Finding) {
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
