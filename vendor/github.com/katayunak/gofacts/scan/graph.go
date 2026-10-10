package scan

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/katayunak/gofacts/fact"
)

func (p *Program) buildSSA() error {
	prog, _ := ssautil.AllPackages(p.ld.Packages, ssa.InstantiateGenerics)
	if prog == nil {
		return errorf("could not build SSA for %s", p.root)
	}
	prog.Build()
	p.ssa = prog
	return nil
}

// buildCallGraph keeps two views of every dynamic call. CHA links an
// interface call to every implementation and a function-value call to every
// function of its signature; seam classification and sink reachability are
// tuned to that over-approximation. VTA keeps only the targets whose values
// are seen flowing into the call. A site VTA resolves is taken at its word;
// a site it cannot resolve — nothing in the loaded code ever stores a value
// there — falls back to CHA rather than to nothing.
//
// testigo pruned CHA edges by import closure instead. That dropped every
// implementation behind an interface declared where it is consumed, the usual
// Go layout: service declares Store, repo implements it, main wires them, and
// service never imports repo.
//
// VTA is given the module's own functions only. Its answers are used only at
// the module's call sites, and analysing the standard library and every
// dependency is most of its cost. What that gives up: an interface value that
// reaches the module only through library code is not seen, so a site fed
// partly that way can come back narrower than it is.
func (p *Program) buildCallGraph(noVTA bool) {
	p.all = ssautil.AllFunctions(p.ssa)
	p.addGenericMethods(p.all)
	chaGraph := cha.CallGraph(p.ssa)
	if !noVTA {
		vtaGraph := vta.CallGraph(p.localOnly(), chaGraph)
		vtaGraph.DeleteSyntheticNodes()
		p.vtaSites = sitesOf(vtaGraph)
	}
	chaGraph.DeleteSyntheticNodes()
	p.cha = chaGraph
	p.chaSites = sitesOf(chaGraph)
}

func sitesOf(g *callgraph.Graph) map[ssa.CallInstruction][]*ssa.Function {
	out := map[ssa.CallInstruction][]*ssa.Function{}
	for _, n := range g.Nodes {
		for _, e := range n.Out {
			if e.Site != nil && e.Callee.Func != nil {
				out[e.Site] = append(out[e.Site], e.Callee.Func)
			}
		}
	}
	return out
}

// resolve returns the callees of a call site and how sure that is.
func (p *Program) resolve(site ssa.CallInstruction) ([]*ssa.Function, fact.Certainty, fact.CallKind) {
	common := site.Common()
	if callee := common.StaticCallee(); callee != nil {
		if targets := p.chaSites[site]; len(targets) > 0 {
			return targets, fact.Certain, fact.CallStatic
		}
		return []*ssa.Function{callee}, fact.Certain, fact.CallStatic
	}
	kind := fact.CallDynamic
	if common.IsInvoke() {
		kind = fact.CallDispatch
	}
	if targets := p.vtaSites[site]; len(targets) > 0 {
		return targets, fact.Likely, kind
	}
	return p.chaSites[site], fact.Possible, kind
}

func genericOrigin(fn *ssa.Function) *ssa.Function {
	if o := fn.Origin(); o != nil {
		return o
	}
	return fn
}

// owner is the declared function a closure belongs to.
func owner(fn *ssa.Function) *ssa.Function {
	for fn != nil && fn.Parent() != nil {
		fn = fn.Parent()
	}
	if fn != nil {
		fn = genericOrigin(fn)
	}
	return fn
}

func (p *Program) isLocal(fn *ssa.Function) bool {
	o := owner(fn)
	if o == nil {
		return false
	}
	if obj := o.Object(); obj != nil && obj.Pkg() != nil {
		return p.local[obj.Pkg().Path()]
	}
	return o.Pkg != nil && p.local[o.Pkg.Pkg.Path()]
}

// symbolOf names a function from its type, the form testigo has always used:
// Pay, Service.Pay, (*Service).Pay. Generic receivers lose their type
// arguments: (*Repo[T]).Save is (*Repo).Save.
func symbolOf(obj *types.Func) string {
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return obj.Name()
	}
	t := sig.Recv().Type()
	ptr := false
	if pt, ok := types.Unalias(t).(*types.Pointer); ok {
		ptr, t = true, pt.Elem()
	}
	name := "?"
	switch rt := types.Unalias(t).(type) {
	case *types.Named:
		name = rt.Obj().Name()
	case *types.Interface:
		name = "interface"
	}
	if ptr {
		return "(*" + name + ")." + obj.Name()
	}
	return name + "." + obj.Name()
}

func (p *Program) identify(fn *ssa.Function) (string, *ast.FuncDecl, bool) {
	fn = genericOrigin(fn)
	if fn.Synthetic != "" || fn.Parent() != nil {
		return "", nil, false
	}
	obj, ok := fn.Object().(*types.Func)
	if !ok || obj == nil || obj.Pkg() == nil {
		return "", nil, false
	}
	decl, ok := fn.Syntax().(*ast.FuncDecl)
	if !ok {
		return "", nil, false
	}
	return obj.Pkg().Path() + "#" + symbolOf(obj), decl, true
}

// fromVariant reports whether fn is a test variant's copy of a function that
// also exists in the plain package.
func (p *Program) fromVariant(fn *ssa.Function) bool {
	fn = genericOrigin(fn)
	return fn.Pkg != nil && p.variant[fn.Pkg.Pkg] && !isTestFile(p.rel(fn.Pos()))
}

func (p *Program) indexFunctions() {
	p.idOf = map[*ssa.Function]string{}
	p.canon = map[string]*ssa.Function{}
	p.funcs = map[string]*fact.Function{}

	all := p.all
	for fn := range all {
		if !p.isLocal(fn) {
			continue
		}
		top := owner(fn)
		id, decl, ok := p.identify(top)
		if !ok {
			continue
		}
		p.idOf[fn] = id
		if fn != top {
			continue
		}
		if cur, seen := p.canon[id]; seen && !p.fromVariant(cur) {
			continue
		}
		p.canon[id] = fn
		obj := fn.Object().(*types.Func)
		file := p.rel(decl.Pos())
		p.funcs[id] = &fact.Function{
			Ref:       fact.CodeRef{Pkg: obj.Pkg().Path(), Symbol: symbolOf(obj), File: file, Line: p.line(decl.Pos())},
			EndLine:   p.line(decl.End()),
			Exported:  obj.Exported(),
			InTest:    isTestFile(file),
			Generated: p.generated[file],
		}
	}
	p.spawnedBy = spawnedClosures(all)
}

// addGenericMethods adds the methods of the module's generic types, which
// AllFunctions omits until something instantiates the type.
func (p *Program) addGenericMethods(all map[*ssa.Function]bool) {
	for _, pkg := range p.prod {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok || named.TypeParams().Len() == 0 {
				continue
			}
			for i := 0; i < named.NumMethods(); i++ {
				if fn := p.ssa.FuncValue(named.Method(i)); fn != nil {
					withClosures(fn, func(f *ssa.Function) { all[f] = true })
				}
			}
		}
	}
}

// spawnedClosures finds closures started with go or defer, so calls made inside
// them can be marked: in `go func() { h.capture() }()` the call to capture does
// not happen before the handler returns.
func spawnedClosures(all map[*ssa.Function]bool) map[*ssa.Function]spawn {
	out := map[*ssa.Function]spawn{}
	for fn := range all {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				var common *ssa.CallCommon
				var s spawn
				switch c := instr.(type) {
				case *ssa.Go:
					common, s = &c.Call, spawn{goroutine: true}
				case *ssa.Defer:
					common, s = &c.Call, spawn{deferred: true}
				default:
					continue
				}
				var target *ssa.Function
				switch v := common.Value.(type) {
				case *ssa.Function:
					target = v
				case *ssa.MakeClosure:
					target, _ = v.Fn.(*ssa.Function)
				}
				if target != nil && target.Parent() == fn {
					cur := out[target]
					out[target] = spawn{cur.goroutine || s.goroutine, cur.deferred || s.deferred}
				}
			}
		}
	}
	return out
}

func (p *Program) localFunctions() []*ssa.Function {
	var out []*ssa.Function
	for fn, id := range p.idOf {
		if p.funcs[id] != nil {
			out = append(out, fn)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pos() != out[j].Pos() {
			return out[i].Pos() < out[j].Pos()
		}
		return out[i].String() < out[j].String()
	})
	return out
}

func (p *Program) buildEdges() {
	seen := map[string]bool{}
	for _, fn := range p.localFunctions() {
		from := p.idOf[fn]
		sp := p.spawnedBy[fn]
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				site, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				_, isGo := instr.(*ssa.Go)
				_, isDefer := instr.(*ssa.Defer)
				callees, certainty, kind := p.resolve(site)
				for _, callee := range callees {
					to, ok := p.idOf[callee]
					if !ok || to == from || p.funcs[to] == nil {
						continue
					}
					e := fact.Edge{
						From: from, To: to, Site: p.pos(site.Pos()), Kind: kind,
						Certainty: certainty, Go: isGo || sp.goroutine, Defer: isDefer || sp.deferred,
					}
					key := e.From + ">" + e.To + "@" + e.Site.String() + ":" + itoa(e.Site.Col)
					if seen[key] {
						continue
					}
					seen[key] = true
					p.edges = append(p.edges, e)
				}
			}
		}
	}
	sort.Slice(p.edges, func(i, j int) bool {
		a, b := p.edges[i], p.edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.Site != b.Site {
			return a.Site.Before(b.Site)
		}
		return a.To < b.To
	})
	p.outEdges = map[string][]fact.Edge{}
	p.inEdges = map[string][]fact.Edge{}
	for _, e := range p.edges {
		p.outEdges[e.From] = append(p.outEdges[e.From], e)
		p.inEdges[e.To] = append(p.inEdges[e.To], e)
	}
}

func (p *Program) Functions() []fact.Function {
	out := make([]fact.Function, 0, len(p.funcs))
	for _, f := range p.funcs {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.ID() < out[j].Ref.ID() })
	return out
}

func (p *Program) Function(id string) (fact.Function, bool) {
	f, ok := p.funcs[id]
	if !ok {
		return fact.Function{}, false
	}
	return *f, true
}

// FunctionAt returns the declaration enclosing file:line, which is how a
// changed hunk is mapped to what it changes.
func (p *Program) FunctionAt(file string, line int) (fact.Function, bool) {
	var best *fact.Function
	for _, f := range p.funcs {
		if f.Contains(file, line) && (best == nil || f.EndLine-f.Ref.Line < best.EndLine-best.Ref.Line) {
			best = f
		}
	}
	if best == nil {
		return fact.Function{}, false
	}
	return *best, true
}

func (p *Program) Callees(id string) []fact.Edge { return append([]fact.Edge(nil), p.outEdges[id]...) }
func (p *Program) Callers(id string) []fact.Edge { return append([]fact.Edge(nil), p.inEdges[id]...) }
func (p *Program) Edges() []fact.Edge            { return append([]fact.Edge(nil), p.edges...) }

// TestsFor walks callers outwards from id and returns the tests it meets,
// nearest first. The certainty of a link is that of its weakest edge, so a
// test that reaches the function only through an interface dispatch CHA could
// not narrow says so.
func (p *Program) TestsFor(id string, maxHops int) []fact.TestLink {
	if maxHops <= 0 {
		maxHops = 6
	}
	type state struct {
		hops      int
		certainty fact.Certainty
	}
	best := map[string]state{id: {0, fact.Certain}}
	frontier := []string{id}
	for hop := 1; hop <= maxHops && len(frontier) > 0; hop++ {
		var next []string
		for _, cur := range frontier {
			for _, e := range p.inEdges[cur] {
				c := fact.Weakest(best[cur].certainty, e.Certainty)
				if old, seen := best[e.From]; seen && (old.hops < hop || old.certainty.Rank() <= c.Rank()) {
					continue
				}
				best[e.From] = state{hop, c}
				next = append(next, e.From)
			}
		}
		frontier = next
	}
	var out []fact.TestLink
	for fid, st := range best {
		f := p.funcs[fid]
		if fid == id || f == nil || !f.InTest || !isTestName(f.Ref.Symbol) {
			continue
		}
		out = append(out, fact.TestLink{Test: f.Ref, Target: id, Hops: st.hops, Certainty: st.certainty})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hops != out[j].Hops {
			return out[i].Hops < out[j].Hops
		}
		return out[i].Test.ID() < out[j].Test.ID()
	})
	return out
}

func isTestName(symbol string) bool {
	if i := strings.LastIndex(symbol, "."); i >= 0 {
		symbol = symbol[i+1:]
	}
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(symbol, prefix) {
			return true
		}
	}
	return false
}

func (p *Program) localOnly() map[*ssa.Function]bool {
	out := map[*ssa.Function]bool{}
	for fn := range p.all {
		if p.isLocal(fn) {
			out[fn] = true
		}
	}
	return out
}
