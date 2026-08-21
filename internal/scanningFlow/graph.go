package scanningFlow

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/katayunak/testigo/internal/codeRef"
)

type graph struct {
	prog      *ssa.Program // SSA representation of the Go program
	callGraph *callgraph.Graph
	local     map[string]bool          // skipping dependencies nodes through this
	byID      map[string]*ssa.Function // reference ID -> function
	idOf      map[*ssa.Function]string
	reach     map[*ssa.Function]kindSet
	refs      map[string]codeRef.CodeRef
}

func buildGraph(pkgs []*packages.Package, local map[string]bool) (*graph, error) {
	// turning packages into an SSA program
	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	if prog == nil {
		return nil, fmt.Errorf("could not build SSA program")
	}
	prog.Build()

	cg := cha.CallGraph(prog)
	// compiler-generated functions are not steps in the business flow.
	// Deleting them REROUTES their edges so the graph keeps its shape
	// while dropping nodes which are not actually a part of the business flow.
	cg.DeleteSyntheticNodes()

	g := &graph{
		prog: prog, callGraph: cg, local: local,
		byID: map[string]*ssa.Function{},
		idOf: map[*ssa.Function]string{},
		refs: map[string]codeRef.CodeRef{},
	}

	for fn := range ssautil.AllFunctions(prog) {
		if id, a, ok := g.identify(fn); ok {
			g.byID[id] = fn
			g.idOf[fn] = id
			g.refs[id] = a
		}
	}
	g.reach = computeReach(cg, g.isLocal)

	return g, nil
}

// SSA functions don't have your identity
// so identify names a declared function using exactly the same rule the codeRef
func (g *graph) identify(fn *ssa.Function) (string, codeRef.CodeRef, bool) {
	decl, ok := fn.Syntax().(*ast.FuncDecl)
	if !ok || fn.Pkg == nil {
		return "", codeRef.CodeRef{}, false
	}

	path := fn.Pkg.Pkg.Path()
	sym := codeRef.Symbol(decl)
	pos := g.prog.Fset.Position(decl.Pos())
	hash, _ := codeRef.StructuralHash(decl)

	return path + "#" + sym, codeRef.CodeRef{Pkg: path, Symbol: sym, Line: pos.Line, BodyHash: hash}, true
}

// owner walks a closure up to the declared function that contains it
func owner(fn *ssa.Function) *ssa.Function {
	for fn != nil && fn.Parent() != nil {
		fn = fn.Parent()
	}

	return fn
}

func (g *graph) isLocal(fn *ssa.Function) bool {
	o := owner(fn)
	if o == nil || o.Pkg == nil {
		return false
	}

	return g.local[o.Pkg.Pkg.Path()]
}

// walk performs reachability from the entry points and fills the flow
func (g *graph) walk(root string, entries []EntryPoint, flow *Flow) error {
	if len(entries) == 0 {
		return fmt.Errorf("no entry points configured: a payment flow has several " +
			"(API handler, provider webhook, reconciliation job, queue consumer) and " +
			"following only one hides exactly the bugs worth finding")
	}

	var seeds []*ssa.Function
	for _, e := range entries {
		id := e.Pkg + "#" + e.Symbol
		fn, ok := g.byID[id]
		if !ok {
			return fmt.Errorf("entry point %q not found; %s", id, g.suggest(e))
		}

		seeds = append(seeds, fn)
	}

	seen := map[*ssa.Function]bool{}
	queue := append([]*ssa.Function{}, seeds...)
	for _, f := range seeds {
		seen[f] = true
	}

	entrySet := map[string]bool{}
	for _, f := range seeds {
		entrySet[g.idOf[f]] = true
	}

	calls := map[string]map[string]bool{}
	for len(queue) > 0 {
		fn := queue[0]
		queue = queue[1:]
		from := g.idOf[owner(fn)]
		n := g.callGraph.Nodes[fn]
		if n == nil {
			continue
		}

		for _, e := range n.Out {
			callee := e.Callee.Func
			if !g.isLocal(callee) {
				// only business calls in the graph
				continue
			}

			to := g.idOf[owner(callee)]
			if from != "" && to != "" && from != to {
				if calls[from] == nil {
					calls[from] = map[string]bool{}
				}

				calls[from][to] = true
			}

			if !seen[callee] {
				seen[callee] = true
				queue = append(queue, callee)
			}
		}
	}

	// One node per declared function that the flow actually reaches.
	touched := map[*ssa.Function]bool{}
	for fn := range seen {
		if o := owner(fn); o != nil && g.idOf[o] != "" {
			touched[o] = true
		}
	}

	for fn := range touched {
		id := g.idOf[fn]
		a := g.refs[id]
		a.File = relFile(g.prog.Fset, fn, root)
		facts, seams := g.factsFor(fn, a)
		kind := NodePositionInternal
		switch {
		case entrySet[id]:
			kind = NodePositionEntry

		case len(calls[id]) == 0:
			kind = NodePositionLeaf
		}

		var out []string
		for to := range calls[id] {
			out = append(out, to)
		}

		sort.Strings(out)
		flow.Nodes[id] = &Node{Ref: a, Kind: kind, Calls: out, Facts: facts}
		flow.Seams = append(flow.Seams, seams...)
	}

	sort.Slice(flow.Seams, func(i, j int) bool {
		if flow.Seams[i].In.ID() != flow.Seams[j].In.ID() {
			return flow.Seams[i].In.ID() < flow.Seams[j].In.ID()
		}
		return flow.Seams[i].Line < flow.Seams[j].Line
	})

	return nil
}

// suggest turns a missed entry point into an actionable message instead of a
// bare "not found". Getting the receiver syntax wrong is the common mistake.
func (g *graph) suggest(e EntryPoint) string {
	var same []string
	for id := range g.byID {
		pkg, sym, _ := strings.Cut(id, "#")
		if pkg == e.Pkg {
			same = append(same, sym)
		}
	}

	if len(same) == 0 {
		return fmt.Sprintf("package %q has no functions in the loaded set (is it inside the module?)", e.Pkg)
	}

	sort.Strings(same)
	if len(same) > 12 {
		same = append(same[:12], "...")
	}

	return fmt.Sprintf("package %q declares: %s (methods use the form (*Type).Method)", e.Pkg, strings.Join(same, ", "))
}

func relFile(fset *token.FileSet, fn *ssa.Function, root string) string {
	pos := fset.Position(fn.Pos())
	if pos.Filename == "" {
		return ""
	}

	if rel, err := filepath.Rel(root, pos.Filename); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}

	return filepath.ToSlash(pos.Filename)
}
