package scan

import (
	"go/types"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/naming"
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

const (
	ksEffects = ksDB | ksHTTP | ksQueue | ksCache
	ksNet     = ksHTTP | ksQueue
)

var kindOrder = []struct {
	bit  kindSet
	kind fact.SeamKind
}{
	{ksDB, fact.SeamDB}, {ksHTTP, fact.SeamHTTP}, {ksQueue, fact.SeamQueue},
	{ksCache, fact.SeamCache}, {ksClock, fact.SeamClock}, {ksRandom, fact.SeamRandom},
}

func (k kindSet) list() []fact.SeamKind {
	var out []fact.SeamKind
	for _, e := range kindOrder {
		if k&e.bit != 0 {
			out = append(out, e.kind)
		}
	}
	return out
}

func fromSeamKind(k fact.SeamKind) kindSet {
	for _, e := range kindOrder {
		if e.kind == k {
			return e.bit
		}
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

// directKind classifies a library function by what it actually is, from its
// SSA identity: http.Client.Get is an HTTP round trip, http.Header.Get is not.
func directKind(fn *ssa.Function) kindSet {
	path := pkgPathOf(fn)
	if path == "" {
		return 0
	}
	switch path {
	case "net/http":
		if netHTTPClient[fn.String()] {
			return ksHTTP
		}
		return 0
	case "time":
		if timeSeamFuncs[fn.Name()] {
			return ksClock
		}
		return 0
	}
	k := kindsOfPackage(path)
	if k&ksDB != 0 && isCursorNoise(fn.Name(), fn.String()) {
		return 0
	}
	return k
}

func isCursorNoise(name, fullName string) bool {
	return cursorNoise[name] && !closesATransaction(fullName)
}

// computeReach propagates boundary kinds backwards through module code to a
// fixed point, so a function with no I/O of its own still knows what it can
// eventually cause.
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
			callers[e.Callee.Func] = append(callers[e.Callee.Func], fn)
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

func relativeTo(p *types.Package) string { return p.Path() }

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

// isSink is a genuine round trip: a socket, TLS, a database/sql statement, the
// net/http client, a subprocess.
func isSink(fn *ssa.Function) bool {
	name, recv := fn.Name(), receiverName(fn)
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
		return netHTTPClient[fn.String()]
	case "os/exec":
		return recv == "Cmd" && (name == "Run" || name == "Start" || name == "Output" || name == "CombinedOutput")
	}
	return false
}

// plumbingEdge is an edge the sink walk refuses: calls through plumbing
// interfaces and anonymous interfaces resolve to every implementation in the
// program, socket reads included, and calls through function values of a
// generic shape link to every function of that shape.
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

// computeSinks marks every function that can reach a genuine round trip. A
// library's own I/O cannot depend on callbacks the application registers, so
// edges from library code back into the module are not followed.
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

var asyncVerbs = map[string]bool{
	"publish": true, "produce": true, "emit": true, "push": true, "enqueue": true,
	"dispatch": true, "send": true, "deliver": true, "notify": true, "request": true,
	"flush": true, "subscribe": true, "ack": true, "nack": true, "reply": true,
}

// asyncEffect keeps sends that hand their write to a background goroutine:
// NATS Publish reaches its socket on no static path at all.
func asyncEffect(name string) bool {
	w := naming.Words(name)
	return len(w) > 0 && asyncVerbs[w[0]]
}

// effectKind is a library function's kind, kept only if it can reach a real
// round trip or is an asynchronous send.
func (p *Program) effectKind(fn *ssa.Function) kindSet {
	k := directKind(fn)
	if k&ksEffects == 0 || p.sink[fn] || asyncEffect(fn.Name()) {
		return k
	}
	return k &^ ksEffects
}

func (p *Program) nearestKind(fn *ssa.Function) kindSet {
	key := fn.String()
	if k, ok := p.importKinds[key]; ok {
		return k
	}
	var out kindSet
	seen := map[*ssa.Function]bool{fn: true}
	queue := []*ssa.Function{fn}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		node := p.cha.Nodes[cur]
		if node == nil {
			continue
		}
		for _, e := range node.Out {
			callee := e.Callee.Func
			if callee == nil || seen[callee] {
				continue
			}
			seen[callee] = true
			if p.isLocal(callee) {
				queue = append(queue, callee)
				continue
			}
			out |= p.effectKind(callee)
		}
	}
	p.importKinds[key] = out
	return out
}

func (p *Program) importKind(fn *ssa.Function) kindSet {
	path := pkgPathOf(fn)
	if path == "" {
		return 0
	}
	key := "pkg:" + path
	if k, ok := p.importKinds[key]; ok {
		return k
	}
	var k kindSet
	for dep := range p.directImports[path] {
		if p.local[dep] {
			continue
		}
		if dep == "net/http" {
			k |= ksHTTP
		}
		k |= kindsOfPackage(dep)
	}
	p.importKinds[key] = k
	return k
}

// possibleKind is what a module function behind an interface can cause,
// limited to the kinds its own package imports.
func (p *Program) possibleKind(fn *ssa.Function) kindSet {
	reached := p.nearestKind(fn)
	if allowed := p.importKind(fn); allowed != 0 {
		return reached & allowed
	}
	return reached
}

func ifacePkg(m *types.Func) string {
	if m == nil || m.Pkg() == nil {
		return ""
	}
	return m.Pkg().Path()
}

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

// classification is what one call site is, as a boundary.
type classification struct {
	target     string
	kinds      kindSet
	injectable bool
	iface      string
	guessed    bool
}

// classify decides whether a call leaves the process. An interface call is a
// seam when the interface is the module's own or one of its implementations
// is; a static call is a seam when it lands in a boundary library.
func (p *Program) classify(c *ssa.CallCommon, callees []*ssa.Function) classification {
	if c.IsInvoke() {
		if isPlumbingIface(c.Value.Type()) {
			return classification{}
		}
		iface := types.TypeString(c.Value.Type(), relativeTo)
		ownIface := p.local[ifacePkg(c.Method)]
		anyLocal := false
		var kinds kindSet
		for _, callee := range callees {
			if p.isLocal(callee) {
				anyLocal = true
				kinds |= p.effectKind(callee) | p.possibleKind(callee)
			} else {
				kinds |= p.effectKind(callee)
			}
		}
		if !ownIface && !anyLocal {
			return classification{}
		}
		guessed := false
		if len(callees) == 0 || (kinds == 0 && asyncEffect(c.Method.Name())) {
			kinds, guessed = guessFromName(iface, c.Method.Name()), true
		}
		if kinds == 0 {
			return classification{}
		}
		return classification{target: c.Method.FullName(), kinds: kinds, injectable: true, iface: iface, guessed: guessed}
	}
	callee := c.StaticCallee()
	if callee == nil || p.isLocal(callee) {
		return classification{}
	}
	kinds := p.effectKind(callee)
	if kinds == 0 {
		return classification{}
	}
	return classification{target: callee.String(), kinds: kinds}
}

func directImportsOf(pkgs []*packages.Package) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	var walk func(p *packages.Package)
	walk = func(p *packages.Package) {
		if p == nil {
			return
		}
		if _, seen := out[p.PkgPath]; seen {
			return
		}
		direct := map[string]bool{}
		out[p.PkgPath] = direct
		for path, imported := range p.Imports {
			direct[path] = true
			walk(imported)
		}
	}
	for _, p := range pkgs {
		walk(p)
	}
	return out
}

var (
	txOpen       = map[string]bool{"Begin": true, "BeginTx": true, "Beginx": true, "BeginTxx": true}
	txRunWrapper = map[string]bool{
		"RunInTransaction": true, "RunInTransactionWithOptions": true, "Transaction": true,
		"RunInTx": true, "BeginFunc": true, "BeginTxFunc": true,
	}
	reTxCloseReceiver = regexp.MustCompile(`\.([A-Za-z_][A-Za-z0-9_]*)\)\.Close$`)
)

// closesATransaction: go-pg and database/sql roll back an uncommitted
// transaction on Close, so defer tx.Close() is a rollback. Closing a pool or a
// file is not.
func closesATransaction(target string) bool {
	m := reTxCloseReceiver.FindStringSubmatch(target)
	if m == nil {
		return false
	}
	r := m[1]
	return r == "Tx" || strings.HasSuffix(r, "Tx") || strings.HasSuffix(r, "Transaction")
}

func methodName(target string) string {
	name := target
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return strings.TrimSuffix(name, ")")
}

func applyTxFacts(f *fact.Facts, target string) {
	switch name := methodName(target); {
	case txRunWrapper[name]:
		f.OpensTx, f.CommitsTx, f.RollsBackTx = true, true, true
	case txOpen[name]:
		f.OpensTx = true
	case name == "Commit":
		f.CommitsTx = true
	case name == "Rollback":
		f.RollsBackTx = true
	case name == "Close" && closesATransaction(target):
		f.RollsBackTx = true
	}
}

// analyzeFunctions computes per-function facts and seams for every function
// of the module, closures included, against the CHA graph the seam filters are
// tuned to.
func (p *Program) analyzeFunctions() {
	isLocal := p.isLocal
	p.directImports = directImportsOf(p.ld.Packages)
	p.sink = computeSinks(p.cha, isLocal)
	p.reach = computeReach(p.cha, isLocal, p.effectKind)

	ids := make([]string, 0, len(p.canon))
	for id := range p.canon {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fn := p.canon[id]
		f := p.funcs[id]
		facts, seams := p.factsFor(fn, f.Ref)
		f.Facts = facts
		p.seams = append(p.seams, seams...)
	}
	sort.SliceStable(p.seams, func(i, j int) bool {
		if p.seams[i].In.ID() != p.seams[j].In.ID() {
			return p.seams[i].In.ID() < p.seams[j].In.ID()
		}
		if p.seams[i].Line != p.seams[j].Line {
			return p.seams[i].Line < p.seams[j].Line
		}
		return p.seams[i].Target < p.seams[j].Target
	})
}

func withClosures(fn *ssa.Function, visit func(*ssa.Function)) {
	visit(fn)
	for _, anon := range fn.AnonFuncs {
		withClosures(anon, visit)
	}
}

func (p *Program) factsFor(fn *ssa.Function, ref fact.CodeRef) (fact.Facts, []fact.Seam) {
	var facts fact.Facts
	var seams []fact.Seam
	var reaches kindSet
	withClosures(fn, func(f *ssa.Function) {
		reaches |= p.reach[f]
		for _, b := range f.Blocks {
			for _, instr := range b.Instrs {
				ci, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				if _, isGo := instr.(*ssa.Go); isGo {
					facts.SpawnsGoroutine = true
				}
				cl := p.classify(ci.Common(), p.chaSites[ci])
				if cl.target == "" {
					continue
				}
				applyTxFacts(&facts, cl.target)
				for _, k := range cl.kinds.list() {
					switch k {
					case fact.SeamDB:
						facts.TouchesDB = true
					case fact.SeamHTTP, fact.SeamQueue:
						facts.TouchesNet = true
					case fact.SeamClock:
						facts.ReadsClock = true
					case fact.SeamRandom:
						facts.Randomness = true
					}
					seams = append(seams, fact.Seam{
						In: ref, Kind: k, Target: cl.target, Line: p.line(instr.Pos()),
						Injectable: cl.injectable, Iface: cl.iface,
					})
				}
			}
		}
	})
	facts.Reaches = (reaches & ksEffects).list()
	facts.MoneyTypes = p.moneyTypesIn(fn)
	facts.HandlesMoney = len(facts.MoneyTypes) > 0
	return facts, dedupeSeams(seams)
}

func dedupeSeams(in []fact.Seam) []fact.Seam {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		k := string(s.Kind) + "|" + s.Target + "|" + strconv.Itoa(s.Line)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

func itoa(i int) string { return strconv.Itoa(i) }
