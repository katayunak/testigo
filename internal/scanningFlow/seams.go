package scanningFlow

import (
	"go/types"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/codeRef"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

// kindSet is a bitmask of seam kinds, used so the reachability fixpoint below
// is a cheap OR over integers rather than map merging on every iteration.
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
	kind SeamKind
}{
	{ksDB, SeamDB}, {ksHTTP, SeamHTTP}, {ksQueue, SeamQueue},
	{ksCache, SeamCache}, {ksClock, SeamClock}, {ksRandom, SeamRandom},
}

func (k kindSet) list() []SeamKind {
	var out []SeamKind
	for _, e := range kindOrder {
		if k&e.bit != 0 {
			out = append(out, e.kind)
		}
	}
	return out
}

// ioPrefixes maps package path prefixes to the kind of boundary they represent.
// This is a lookup table on purpose: it is the piece most likely to need
// extending for a given shop's stack, and a table is easier to review and
// extend than a chain of conditionals buried in the analysis.
var ioPrefixes = []struct {
	prefix string
	kind   kindSet
}{
	{"database/sql", ksDB},
	{"github.com/jackc/pgx", ksDB},
	{"github.com/lib/pq", ksDB},
	{"github.com/jmoiron/sqlx", ksDB},
	{"gorm.io/", ksDB},
	{"entgo.io/", ksDB},
	{"go.mongodb.org/", ksDB},
	{"github.com/uptrace/bun", ksDB},
	{"google.golang.org/grpc", ksHTTP},
	{"github.com/go-resty/resty", ksHTTP},
	{"github.com/segmentio/kafka-go", ksQueue},
	{"github.com/IBM/sarama", ksQueue},
	{"github.com/Shopify/sarama", ksQueue},
	{"github.com/nats-io/", ksQueue},
	{"github.com/rabbitmq/", ksQueue},
	{"github.com/streadway/amqp", ksQueue},
	{"github.com/redis/", ksCache},
	{"github.com/go-redis/", ksCache},
	{"github.com/bradfitz/gomemcache", ksCache},
	{"github.com/google/uuid", ksRandom},
	{"github.com/oklog/ulid", ksRandom},
	{"math/rand", ksRandom},
	{"crypto/rand", ksRandom},
}

// netHTTPClient lists the outbound calls in net/http by FULL name.
//
// Matching on the bare method name was a bug: net/http.Header.Get is not an
// outbound request, and neither is net/http.Error, but both are called "Get"
// and "Error" on a package path of net/http. A payment service is full of
// server-side net/http types, and misclassifying them buries the two provider
// calls that actually matter under fifty that do not.
var netHTTPClient = map[string]bool{
	"net/http.Get": true, "net/http.Post": true, "net/http.PostForm": true, "net/http.Head": true,
	"(*net/http.Client).Do": true, "(*net/http.Client).Get": true, "(*net/http.Client).Post": true,
	"(*net/http.Client).Head": true, "(*net/http.Client).PostForm": true,
	"(*net/http.Transport).RoundTrip": true,
}

// cursorNoise are result-set mechanics rather than boundaries worth injecting
// a fault at. They do perform I/O in some drivers, but a report that lists
// Rows.Next alongside the provider authorization call has buried the signal.
// The query is the seam; iterating its result is not.
var cursorNoise = map[string]bool{
	"Next": true, "Scan": true, "Err": true, "Close": true, "Columns": true,
	"ColumnTypes": true, "NextResultSet": true, "LastInsertId": true, "RowsAffected": true,
}

// timeSeamFuncs are the reads that make behaviour depend on wall-clock time.
// Expiries, idempotency-key windows and retry backoff all hinge on these, and
// a test cannot exercise them without an injectable clock.
var timeSeamFuncs = map[string]bool{
	"Now": true, "Since": true, "Until": true, "Sleep": true, "After": true, "Tick": true,
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

// directKind classifies a call by where it lands, with no reachability needed.
func directKind(fn *ssa.Function) kindSet {
	path := pkgPathOf(fn)
	if path == "" {
		return 0
	}
	name := fn.Name()
	switch path {
	case "net/http":
		if netHTTPClient[fn.String()] {
			return ksHTTP
		}
		return 0
	case "time":
		if timeSeamFuncs[name] {
			return ksClock
		}
		return 0
	}
	var k kindSet
	for _, e := range ioPrefixes {
		if strings.HasPrefix(path, e.prefix) {
			k |= e.kind
		}
	}
	if k&ksDB != 0 && cursorNoise[name] {
		return 0
	}
	return k
}

// computeReach answers "if I call this function, what kinds of I/O can happen?"
//
// It is a fixpoint over the call graph. Without it, an interface call like
// `s.ledger.Post(ctx, e)` is unclassifiable — the interface name tells you
// nothing about whether the implementation opens a transaction or calls a
// provider. With it, testigo can say "this invoke reaches the database" and
// therefore "this is a database seam that IS injectable", which is exactly the
// distinction that decides whether a failure test can be written at all.
func computeReach(cg *callgraph.Graph, isLocal func(*ssa.Function) bool) map[*ssa.Function]kindSet {
	reach := make(map[*ssa.Function]kindSet, len(cg.Nodes))
	callers := make(map[*ssa.Function][]*ssa.Function, len(cg.Nodes))
	var work []*ssa.Function

	for fn, n := range cg.Nodes {
		if k := directKind(fn); k != 0 {
			reach[fn] = k
			work = append(work, fn)
		}
		for _, e := range n.Out {
			callee := e.Callee.Func
			callers[callee] = append(callers[callee], fn)
		}
	}

	// Worklist propagation backwards along the call edges. The naive version is
	// a fixpoint that rescans every node on every pass; on a repo that pulls in
	// net/http and database/sql that is tens of thousands of nodes scanned
	// dozens of times. Pushing only the callers of a node that actually changed
	// makes this linear in the number of edges instead.
	for len(work) > 0 {
		fn := work[len(work)-1]
		work = work[:len(work)-1]

		// Propagation stops at the module boundary. A function we own
		// contributes everything it can reach; a dependency contributes only
		// what it does directly.
		//
		// Without this bound, CHA's over-approximation is fatal to the report:
		// it assumes every implementation of an interface is a possible callee,
		// so (error).Error unions the reach of every error type in the program,
		// and the tool concludes that formatting an error message touches the
		// database, the network and the random source. Correct as a bound,
		// useless as information.
		k := reach[fn]
		if !isLocal(fn) {
			k = directKind(fn)
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

// factsFor extracts everything provable about one declared function, including
// the closures nested inside it.
func (g *graph) factsFor(fn *ssa.Function, a codeRef.CodeRef) (Facts, []Seam) {
	var facts Facts
	var seams []Seam

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
				_, isDefer := instr.(*ssa.Defer)
				common := ci.Common()

				target, kinds, injectable, iface := g.classify(common, sites[ci])
				if target == "" {
					continue
				}
				applyTxFacts(&facts, target, isDefer)
				for _, k := range kinds.list() {
					switch k {
					case SeamDB:
						facts.TouchesDB = true
					case SeamHTTP, SeamQueue:
						facts.TouchesNet = true
					case SeamClock:
						facts.ReadsClock = true
					case SeamRandom:
						facts.Randomness = true
					}
					pos := g.prog.Fset.Position(instr.Pos())
					seams = append(seams, Seam{
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

// sitesOf indexes the call graph edges leaving a function by their call site,
// so an interface call can be resolved to its possible implementations in O(1)
// instead of scanning every edge for every instruction.
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

// classify decides what a single call site is, and — the field that matters —
// whether a test can replace it.
//
// Injectable is not a heuristic. SSA reports an interface method call as an
// "invoke", and an invoke is by definition dispatched through a value the
// caller was handed, which a test can substitute. A static call on a concrete
// *sql.DB or http.DefaultClient cannot be intercepted at all without editing
// the code under test.
func (g *graph) classify(c *ssa.CallCommon, callees []*ssa.Function) (target string, kinds kindSet, injectable bool, iface string) {
	if c.IsInvoke() {
		iface = types.TypeString(c.Value.Type(), relativeTo)

		// An injectable seam is an interface whose implementation lives in this
		// module. (error).Error, io.Closer and http.ResponseWriter are calls
		// through interfaces too, but substituting them tests nothing about the
		// payment flow — they are language and framework plumbing, not the
		// boundary where a provider times out.
		ownIface := g.local[ifacePkg(c.Method)]
		anyLocal := false
		for _, callee := range callees {
			if g.isLocal(callee) {
				anyLocal = true
				kinds |= directKind(callee) | g.reach[callee]
			} else {
				kinds |= directKind(callee)
			}
		}
		if !ownIface && !anyLocal {
			return "", 0, false, ""
		}
		target = c.Method.FullName()
		if kinds == 0 {
			kinds = guessFromName(iface, c.Method.Name())
		}
		if kinds == 0 {
			return "", 0, false, ""
		}
		return target, kinds, true, iface
	}
	callee := c.StaticCallee()
	if callee == nil {
		return "", 0, false, "" // call through a func value; nothing provable
	}
	if g.isLocal(callee) {
		return "", 0, false, "" // it gets its own node in the flow
	}
	// Direct classification only. Using transitive reach here was the same
	// mistake as above at a different scale: net/url.URL.Query transitively
	// touches almost everything in a large program, and reporting it as four
	// separate seams is noise a user will learn to ignore.
	if kinds = directKind(callee); kinds == 0 {
		return "", 0, false, ""
	}
	return callee.String(), kinds, false, ""
}

func ifacePkg(m *types.Func) string {
	if m == nil || m.Pkg() == nil {
		return ""
	}
	return m.Pkg().Path()
}

func relativeTo(p *types.Package) string { return p.Path() }

// guessFromName is the last resort, used only when the call graph could not
// reach a concrete implementation — typically because the real one lives behind
// a build tag or is only wired up in main. Naming conventions are weak
// evidence, so anything found this way should be treated as a hint.
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

func applyTxFacts(f *Facts, target string, isDefer bool) {
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
		if isDefer {
			f.HasDeferredTx = true
		}
	}
}

func dedupeSeams(in []Seam) []Seam {
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
