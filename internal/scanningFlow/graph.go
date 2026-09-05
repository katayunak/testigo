package scanningFlow

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/codeRef"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

type orderedCall struct {
	to string
	at token.Pos
}

type graph struct {
	prog      *ssa.Program
	callGraph *callgraph.Graph
	local     map[string]bool

	byID map[string]*ssa.Function
	idOf map[*ssa.Function]string

	reach map[*ssa.Function]kindSet
	refs  map[string]codeRef.CodeRef

	imports map[string]map[string]bool
}

func buildGraph(pkgs []*packages.Package, local map[string]bool) (*graph, error) {

	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	if prog == nil {
		return nil, fmt.Errorf("could not build SSA program")
	}
	prog.Build()

	cg := cha.CallGraph(prog)

	cg.DeleteSyntheticNodes()

	g := &graph{
		prog: prog, callGraph: cg, local: local,
		byID:    map[string]*ssa.Function{},
		idOf:    map[*ssa.Function]string{},
		refs:    map[string]codeRef.CodeRef{},
		imports: importClosure(pkgs),
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

func (g *graph) discover(root string, entries []flowEntity.EntryPoint, flow *flowEntity.Flow) error {
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
			return fmt.Errorf("entry point %q not found", id)
		}

		seeds = append(seeds, fn)
	}

	seen := map[*ssa.Function]bool{}
	worklist := append([]*ssa.Function{}, seeds...)
	for _, f := range seeds {
		seen[f] = true
	}

	entrySet := map[string]bool{}
	for _, f := range seeds {
		entrySet[g.idOf[f]] = true
	}

	calls := map[string][]orderedCall{}
	seenEdge := map[string]bool{}
	for len(worklist) > 0 {
		fn := worklist[0]
		worklist = worklist[1:]
		from := g.idOf[owner(fn)]
		n := g.callGraph.Nodes[fn]
		if n == nil {
			continue
		}

		for _, e := range n.Out {
			callee := e.Callee.Func
			if !g.isLocal(callee) {

				continue
			}

			to := g.idOf[owner(callee)]

			if to != "" && !g.canCall(pkgOfID(from), pkgOfID(to)) {
				continue
			}

			if from != "" && to != "" && from != to && !seenEdge[from+">"+to] {
				seenEdge[from+">"+to] = true
				pos := token.NoPos
				if e.Site != nil {
					pos = e.Site.Pos()
				}
				calls[from] = append(calls[from], orderedCall{to: to, at: pos})
			}

			if !seen[callee] {
				seen[callee] = true
				worklist = append(worklist, callee)
			}
		}
	}

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

		kind := flowEntity.NodePositionInternal
		switch {
		case entrySet[id]:
			kind = flowEntity.NodePositionEntry

		case len(calls[id]) == 0:
			kind = flowEntity.NodePositionLeaf
		}

		ordered := calls[id]
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].at != ordered[j].at {
				return ordered[i].at < ordered[j].at
			}
			return ordered[i].to < ordered[j].to
		})
		out := make([]string, 0, len(ordered))
		for _, c := range ordered {
			out = append(out, c.to)
		}
		flow.Nodes[id] = &flowEntity.Node{Ref: a, Position: kind, Calls: out, Facts: facts}
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

func pkgOfID(id string) string {
	if i := strings.Index(id, "#"); i >= 0 {
		return id[:i]
	}
	return id
}
