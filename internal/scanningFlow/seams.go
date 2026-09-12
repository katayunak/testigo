package scanningFlow

import (
	"go/types"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/scanningFlow/patterns"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

type kindSet uint8

const (
	ksDB kindSet = 1 << iota
	ksHTTP
	ksQueue
	ksCache
	ksClock
	ksRandom
)

var kindOrder = []struct {
	bit  kindSet
	kind flowEntity.SeamKind
}{
	{ksDB, flowEntity.SeamDB}, {ksHTTP, flowEntity.SeamHTTP}, {ksQueue, flowEntity.SeamQueue},
	{ksCache, flowEntity.SeamCache}, {ksClock, flowEntity.SeamClock}, {ksRandom, flowEntity.SeamRandom},
}

func (k kindSet) list() []flowEntity.SeamKind {
	var out []flowEntity.SeamKind
	for _, e := range kindOrder {
		if k&e.bit != 0 {
			out = append(out, e.kind)
		}
	}
	return out
}

func fromSeamKind(k flowEntity.SeamKind) kindSet {
	switch k {
	case flowEntity.SeamDB:
		return ksDB
	case flowEntity.SeamHTTP:
		return ksHTTP
	case flowEntity.SeamQueue:
		return ksQueue
	case flowEntity.SeamCache:
		return ksCache
	case flowEntity.SeamClock:
		return ksClock
	case flowEntity.SeamRandom:
		return ksRandom
	}
	return 0
}

func pkgPathOf(fn *ssa.Function) string {
	if fn == nil {
		return ""
	}
	if fn.Pkg != nil {
		return fn.Pkg.Pkg.Path()
	}
	if fn.Signature != nil && fn.Signature.Recv() != nil {
		t := fn.Signature.Recv().Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if n, ok := t.(*types.Named); ok && n.Obj().Pkg() != nil {
			return n.Obj().Pkg().Path()
		}
	}
	if o := fn.Object(); o != nil && o.Pkg() != nil {
		return o.Pkg().Path()
	}
	return ""
}

func directKind(fn *ssa.Function) kindSet {
	path := pkgPathOf(fn)
	if path == "" {
		return 0
	}
	name := fn.Name()
	switch path {
	case "net/http":
		if patterns.NetHTTPClient[fn.String()] {
			return ksHTTP
		}
		return 0
	case "time":
		if patterns.TimeSeamFuncs[name] {
			return ksClock
		}
		return 0
	}
	var k kindSet
	for _, e := range patterns.IOPrefixes {
		if strings.HasPrefix(path, e.Prefix) {
			k |= fromSeamKind(e.Kind)
		}
	}
	if k&ksDB != 0 && patterns.CursorNoise[name] {
		return 0
	}
	return k
}

func computeReach(cg *callgraph.Graph, isLocal func(*ssa.Function) bool, kind func(*ssa.Function) kindSet) map[*ssa.Function]kindSet {
	reach := make(map[*ssa.Function]kindSet, len(cg.Nodes))
	callers := make(map[*ssa.Function][]*ssa.Function, len(cg.Nodes))
	var work []*ssa.Function

	for fn, n := range cg.Nodes {
		if k := kind(fn); k != 0 {
			reach[fn] = k
			work = append(work, fn)
		}
		for _, e := range n.Out {
			callee := e.Callee.Func
			callers[callee] = append(callers[callee], fn)
		}
	}

	for len(work) > 0 {
		fn := work[len(work)-1]
		work = work[:len(work)-1]

		k := reach[fn]
		if !isLocal(fn) {
			k = kind(fn)
		}
		if k == 0 {
			continue
		}
		for _, c := range callers[fn] {
			if reach[c]|k != reach[c] {
				reach[c] |= k
				work = append(work, c)
			}
		}
	}
	return reach
}

func (g *graph) factsFor(fn *ssa.Function, a flowEntity.CodeRef) (flowEntity.Facts, []flowEntity.Seam) {
	var facts flowEntity.Facts
	var seams []flowEntity.Seam

	sites := g.sitesOf(fn)
	var visit func(f *ssa.Function)
	visit = func(f *ssa.Function) {
		for _, b := range f.Blocks {
			for _, instr := range b.Instrs {
				ci, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				if _, isGo := instr.(*ssa.Go); isGo {
					facts.SpawnsGoroutine = true
				}
				common := ci.Common()

				target, kinds, injectable, iface := g.classify(common, sites[ci])
				if target == "" {
					continue
				}
				applyTxFacts(&facts, target)
				for _, k := range kinds.list() {
					switch k {
					case flowEntity.SeamDB:
						facts.TouchesDB = true
					case flowEntity.SeamHTTP, flowEntity.SeamQueue:
						facts.TouchesNet = true
					case flowEntity.SeamClock:
						facts.ReadsClock = true
					case flowEntity.SeamRandom:
						facts.Randomness = true
					}
					pos := g.prog.Fset.Position(instr.Pos())
					seams = append(seams, flowEntity.Seam{
						In: a, Kind: k, Target: target, Line: pos.Line,
						Injectable: injectable, Iface: iface,
					})
				}
			}
		}
		for _, anon := range f.AnonFuncs {
			visit(anon)
		}
	}
	visit(fn)

	facts.MoneyTypes = g.moneyTypesIn(fn)
	facts.HandlesMoney = len(facts.MoneyTypes) > 0
	sort.Strings(facts.MoneyTypes)
	return facts, dedupeSeams(seams)
}

func (g *graph) sitesOf(fn *ssa.Function) map[ssa.CallInstruction][]*ssa.Function {
	out := map[ssa.CallInstruction][]*ssa.Function{}
	var add func(f *ssa.Function)
	add = func(f *ssa.Function) {
		if n := g.callGraph.Nodes[f]; n != nil {
			for _, e := range n.Out {
				if e.Site != nil {
					out[e.Site] = append(out[e.Site], e.Callee.Func)
				}
			}
		}
		for _, anon := range f.AnonFuncs {
			add(anon)
		}
	}
	add(fn)
	return out
}

func (g *graph) classify(c *ssa.CallCommon, callees []*ssa.Function) (target string, kinds kindSet, injectable bool, iface string) {
	if c.IsInvoke() {
		if isPlumbingIface(c.Value.Type()) {
			return "", 0, false, ""
		}
		iface = types.TypeString(c.Value.Type(), relativeTo)

		ownIface := g.local[ifacePkg(c.Method)]
		anyLocal := false
		for _, callee := range callees {
			if g.isLocal(callee) {
				anyLocal = true
				kinds |= g.effectKind(callee) | g.reach[callee]
			} else {
				kinds |= g.effectKind(callee)
			}
		}
		if !ownIface && !anyLocal {
			return "", 0, false, ""
		}
		target = c.Method.FullName()
		if len(callees) == 0 || (kinds == 0 && asyncEffect(c.Method.Name())) {
			kinds = guessFromName(iface, c.Method.Name())
		}
		if kinds == 0 {
			return "", 0, false, ""
		}
		return target, kinds, true, iface
	}
	callee := c.StaticCallee()
	if callee == nil {
		return "", 0, false, ""
	}
	if g.isLocal(callee) {
		return "", 0, false, ""
	}

	if kinds = g.effectKind(callee); kinds == 0 {
		return "", 0, false, ""
	}
	return callee.String(), kinds, false, ""
}

const ksEffects = ksDB | ksHTTP | ksQueue | ksCache

func (g *graph) effectKind(fn *ssa.Function) kindSet {
	k := directKind(fn)
	if k&ksEffects == 0 || g.sink[fn] || asyncEffect(fn.Name()) {
		return k
	}
	return k &^ ksEffects
}

var asyncVerbs = map[string]bool{
	"publish": true, "produce": true, "emit": true, "push": true, "enqueue": true,
	"dispatch": true, "send": true, "deliver": true, "notify": true, "request": true,
	"flush": true, "subscribe": true, "ack": true, "nack": true, "reply": true,
}

func asyncEffect(name string) bool {
	w := patterns.Words(name)
	return len(w) > 0 && asyncVerbs[w[0]]
}

var plumbingIfaces = map[string]bool{
	"error": true, "fmt.Stringer": true, "fmt.GoStringer": true, "fmt.Formatter": true,
	"context.Context": true, "sort.Interface": true, "sync.Locker": true,
	"hash.Hash": true, "hash.Hash32": true, "hash.Hash64": true,
	"encoding.TextMarshaler": true, "encoding.TextUnmarshaler": true,
	"encoding.BinaryMarshaler": true, "encoding.BinaryUnmarshaler": true,
	"encoding/json.Marshaler": true, "encoding/json.Unmarshaler": true,
	"io.Reader": true, "io.Writer": true, "io.Closer": true, "io.Seeker": true,
	"io.ReadWriter": true, "io.ReadCloser": true, "io.WriteCloser": true, "io.ReadWriteCloser": true,
	"io.ReadSeeker": true, "io.ReaderAt": true, "io.WriterAt": true, "io.ReaderFrom": true, "io.WriterTo": true,
	"io.ByteReader": true, "io.ByteWriter": true, "io.RuneReader": true, "io.StringWriter": true,
}

func isPlumbingIface(t types.Type) bool { return plumbingIfaces[types.TypeString(t, relativeTo)] }

func receiverName(fn *ssa.Function) string {
	if fn == nil || fn.Signature == nil || fn.Signature.Recv() == nil {
		return ""
	}
	t := fn.Signature.Recv().Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj().Name()
	}
	return ""
}

var netConnTypes = map[string]bool{
	"conn": true, "TCPConn": true, "UDPConn": true, "IPConn": true, "UnixConn": true,
	"Dialer": true, "Resolver": true, "TCPListener": true, "UnixListener": true,
}

var sqlRoundTrips = []string{"Exec", "Query", "Begin", "Commit", "Rollback", "Ping", "Prepare", "Raw"}

func isSink(fn *ssa.Function) bool {
	name := fn.Name()
	recv := receiverName(fn)
	switch pkgPathOf(fn) {
	case "net":
		if netConnTypes[recv] {
			return true
		}
		return recv == "" && (strings.HasPrefix(name, "Dial") || strings.HasPrefix(name, "Listen") || strings.HasPrefix(name, "Lookup"))
	case "crypto/tls":
		return recv == "Conn" || (recv == "" && strings.HasPrefix(name, "Dial"))
	case "database/sql":
		switch recv {
		case "DB", "Tx", "Stmt", "Conn":
			for _, p := range sqlRoundTrips {
				if strings.HasPrefix(name, p) {
					return true
				}
			}
		}
	case "net/http":
		return patterns.NetHTTPClient[fn.String()]
	case "os/exec":
		return recv == "Cmd" && (name == "Run" || name == "Start" || name == "Output" || name == "CombinedOutput")
	}
	return false
}

func plumbingEdge(c *ssa.CallCommon) bool {
	if c.IsInvoke() {
		if isPlumbingIface(c.Value.Type()) {
			return true
		}
		_, named := types.Unalias(c.Value.Type()).(*types.Named)
		return !named
	}
	return c.StaticCallee() == nil && genericSignature(c.Signature())
}

var genericPackages = map[string]bool{
	"context": true, "time": true, "fmt": true, "strings": true, "strconv": true, "unicode": true,
	"sort": true, "reflect": true, "errors": true, "bytes": true, "math": true, "sync": true,
}

func genericSignature(sig *types.Signature) bool {
	if sig == nil {
		return true
	}
	for _, tup := range []*types.Tuple{sig.Params(), sig.Results()} {
		for i := 0; i < tup.Len(); i++ {
			if specificType(tup.At(i).Type()) {
				return false
			}
		}
	}
	return true
}

func specificType(t types.Type) bool {
	switch u := types.Unalias(t).(type) {
	case *types.Pointer:
		return specificType(u.Elem())
	case *types.Slice:
		return specificType(u.Elem())
	case *types.Array:
		return specificType(u.Elem())
	case *types.Map:
		return specificType(u.Key()) || specificType(u.Elem())
	case *types.Chan:
		return specificType(u.Elem())
	case *types.Signature:
		return !genericSignature(u)
	case *types.Named:
		obj := u.Obj()
		if obj == nil || obj.Pkg() == nil {
			return false
		}
		return !genericPackages[obj.Pkg().Path()]
	}
	return false
}

func computeSinks(cg *callgraph.Graph, isLocal func(*ssa.Function) bool) map[*ssa.Function]bool {
	reaches := map[*ssa.Function]bool{}
	callers := map[*ssa.Function][]*ssa.Function{}
	var work []*ssa.Function
	for fn, n := range cg.Nodes {
		if fn == nil {
			continue
		}
		if isSink(fn) {
			reaches[fn] = true
			work = append(work, fn)
		}
		for _, e := range n.Out {
			if e.Site != nil && plumbingEdge(e.Site.Common()) {
				continue
			}
			if !isLocal(fn) && isLocal(e.Callee.Func) {
				continue
			}
			callers[e.Callee.Func] = append(callers[e.Callee.Func], fn)
		}
	}
	for len(work) > 0 {
		fn := work[len(work)-1]
		work = work[:len(work)-1]
		for _, c := range callers[fn] {
			if !reaches[c] {
				reaches[c] = true
				work = append(work, c)
			}
		}
	}
	return reaches
}

func ifacePkg(m *types.Func) string {
	if m == nil || m.Pkg() == nil {
		return ""
	}
	return m.Pkg().Path()
}

func relativeTo(p *types.Package) string { return p.Path() }

func guessFromName(iface, method string) kindSet {
	s := strings.ToLower(iface + "." + method)
	switch {
	case strings.Contains(s, "repo"), strings.Contains(s, "storage"),
		strings.Contains(s, "dao"), strings.Contains(s, "persist"):
		return ksDB
	case strings.Contains(s, "client"), strings.Contains(s, "gateway"),
		strings.Contains(s, "provider"), strings.Contains(s, "psp"),
		strings.Contains(s, "acquirer"):
		return ksHTTP
	case strings.Contains(s, "publish"), strings.Contains(s, "producer"),
		strings.Contains(s, "broker"), strings.Contains(s, "emit"):
		return ksQueue
	case strings.Contains(s, "clock"), strings.Contains(s, "timer"):
		return ksClock
	}
	return 0
}

var txOpen = map[string]bool{"Begin": true, "BeginTx": true, "Beginx": true, "Transaction": true}

func applyTxFacts(f *flowEntity.Facts, target string) {
	name := target
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ")")
	switch {
	case txOpen[name]:
		f.OpensTx = true
	case name == "Commit":
		f.CommitsTx = true
	case name == "Rollback":
		f.RollsBackTx = true
	}
}

func dedupeSeams(in []flowEntity.Seam) []flowEntity.Seam {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		k := string(s.Kind) + "|" + s.Target + "|" + itoa(s.Line)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
