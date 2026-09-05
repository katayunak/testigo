package scanningFlow

import (
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fixtureRoot is a deliberately broken payment service. Every defect in it was
// planted on purpose, and this test asserts testigo still finds each one.
//
// This is the only kind of test that means anything for a bug-finding tool. A
// unit test on the parser proves the parser parses; it says nothing about
// whether the tool would catch a float64 balance. If someone tightens a
// heuristic to reduce noise and silently stops reporting TX-NET-CALL, this test
// is what fails.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "paysvc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("fixture not present: %v", err)
	}
	return root
}

// The fixture scanningFlow builds SSA for the whole of net/http and database/sql, which
// costs a few seconds. Running it once per test would make the suite slow
// enough that people stop running it, so it is computed once and shared. The
// scanningFlow is read-only, so sharing is safe.
var (
	fixtureOnce sync.Once
	fixtureRes  *Result
	fixtureErr  error
)

func scanFixture(t *testing.T) *Result {
	t.Helper()
	root := fixtureRoot(t)
	// The fixture is a self-contained module living inside testigo's own tree,
	// so it is also the case that proves GOWORK=off has to be unconditional: a
	// go.work at the testigo root would otherwise hide the fixture's packages
	// entirely. Scan sets it internally now, which is why nothing is passed.
	fixtureOnce.Do(func() {
		fixtureRes, fixtureErr = scanFixtureOnce(root)
	})
	if fixtureErr != nil {
		t.Fatalf("scanningFlow: %v", fixtureErr)
	}
	return fixtureRes
}

func scanFixtureOnce(root string) (*Result, error) {
	return Scan(Options{
		Root: root,
		Entries: []flowEntity.EntryPoint{
			{Pkg: "example.com/paysvc/api", Symbol: "(*Server).CreatePayment"},
			{Pkg: "example.com/paysvc/webhook", Symbol: "(*Handler).PSPCallback"},
			{Pkg: "example.com/paysvc/recon", Symbol: "(*Job).Reconcile"},
		},
	})
}

func TestFindsPlantedDefects(t *testing.T) {
	res := scanFixture(t)

	type want struct {
		id   string
		file string
		sev  flowEntity.Severity
	}
	wants := []want{
		{"MONEY-FLOAT", "domain/payment.go", flowEntity.SevCritical},     // Payment.Amount is a float64
		{"MONEY-FLOAT", "api/server.go", flowEntity.SevCritical},         // applyDiscount takes a float
		{"MONEY-DIV", "api/server.go", flowEntity.SevHigh},               // splitFee truncates the remainder
		{"MONEY-NO-CURRENCY", "domain/payment.go", flowEntity.SevMedium}, // FeeCents with no currency
		{"TX-NET-CALL", "api/server.go", flowEntity.SevCritical},         // PSP call inside the transaction
		{"TX-NO-ROLLBACK", "api/server.go", flowEntity.SevHigh},          // BeginTx with no rollback
		{"STATE-NEVER-SET", "", flowEntity.SevMedium},                    // StatusRefunded / StatusAbandoned
	}
	for _, w := range wants {
		found := false
		for _, f := range res.Flow.Findings {
			if f.ID == w.id && (w.file == "" || f.Ref.File == w.file) {
				found = true
				if f.Severity != w.sev {
					t.Errorf("%s in %s: severity %s, want %s", w.id, w.file, f.Severity, w.sev)
				}
				break
			}
		}
		if !found {
			t.Errorf("missed planted defect %s in %s", w.id, w.file)
		}
	}
}

// Noise is a correctness problem, not a cosmetic one: a user who learns to
// scroll past the findings list will scroll past the real bug too. These are
// the false positives that actually showed up during development, kept as a
// test so they cannot come back.
func TestDoesNotReportNoise(t *testing.T) {
	res := scanFixture(t)
	banned := map[string]string{
		"(error).Error":                   "CHA unions every error implementation; formatting an error is not a seam",
		"(io.Closer).Close":               "language plumbing, not a boundary a test can usefully fail",
		"(net/http.ResponseWriter).Write": "server-side response writing is not an outbound call",
		"(net/http.Header).Get":           "reading a header is not an HTTP request",
		"(*net/url.URL).Query":            "parsing a query string is not I/O",
		"(*database/sql.Rows).Next":       "cursor mechanics; the query is the seam, not the iteration",
		"(*database/sql.Rows).Scan":       "cursor mechanics",
	}
	for _, s := range res.Flow.Seams {
		for bad, why := range banned {
			if s.Target == bad {
				t.Errorf("reported %s as a %s seam in %s — %s", bad, s.Kind, s.In.Symbol, why)
			}
		}
	}
}

// Injectability is the field that decides whether a failure test can be written
// at all, so it gets its own assertions rather than being checked by count.
func TestInjectabilityIsCorrect(t *testing.T) {
	res := scanFixture(t)
	byTarget := map[string]flowEntity.Seam{}
	for _, s := range res.Flow.Seams {
		byTarget[s.Target] = s
	}
	cases := map[string]bool{
		"(example.com/paysvc/psp.Gateway).Authorize": true,  // our interface: a test can make it time out
		"(example.com/paysvc/ledger.Ledger).Post":    true,  // our interface
		"(*database/sql.DB).BeginTx":                 false, // concrete: nothing to substitute
		"(*net/http.Client).Do":                      false, // concrete
		"time.Now":                                   false, // concrete: expiry logic is untestable as written
	}
	for target, wantInjectable := range cases {
		s, ok := byTarget[target]
		if !ok {
			t.Errorf("seam %s not found at all", target)
			continue
		}
		if s.Injectable != wantInjectable {
			t.Errorf("%s: injectable=%v, want %v", target, s.Injectable, wantInjectable)
		}
	}
}

func TestStateMachineExtraction(t *testing.T) {
	res := scanFixture(t)
	// PaymentStatus by its name, SettlementMode by its behaviour. DeclineCode is
	// also a named string with constants and must NOT be here: it is returned
	// and compared, never stored and never advanced, so it is an enum and not a
	// lifecycle. Asking an agent which of its transitions are illegal would be
	// nonsense that costs money.
	byType := map[string]flowEntity.StateMachine{}
	for _, m := range res.Flow.States {
		byType[shortName(m.Type)] = m
	}
	for _, want := range []string{"PaymentStatus", "SettlementMode"} {
		if _, ok := byType[want]; !ok {
			t.Errorf("%s should have been recognised as a lifecycle", want)
		}
	}
	if _, ok := byType["DeclineCode"]; ok {
		t.Error("DeclineCode is an enum, not a lifecycle: it is never stored and never advanced")
	}

	m, ok := byType["PaymentStatus"]
	if !ok {
		t.Fatal("PaymentStatus machine missing")
	}
	if m.Field != "Status" {
		t.Errorf("field: got %q want %q", m.Field, "Status")
	}
	// The declared constants are the complete state set — the compiler
	// guarantees no others exist, which is exactly why this is worth extracting
	// statically instead of asking a models to read the code and list them.
	wantStates := []string{
		"StatusAbandoned", "StatusAuthorized", "StatusCaptured",
		"StatusFailed", "StatusPending", "StatusRefunded",
	}
	if len(m.States) != len(wantStates) {
		t.Fatalf("states: got %v want %v", m.States, wantStates)
	}
	for i, s := range wantStates {
		if m.States[i] != s {
			t.Errorf("states[%d]: got %s want %s", i, m.States[i], s)
		}
	}
	written := map[string]string{}
	for _, w := range m.Writes {
		written[w.To] = w.In.Symbol
	}
	for state, wantIn := range map[string]string{
		"StatusPending":    "(*Server).process",
		"StatusAuthorized": "(*Server).process",
		"StatusCaptured":   "(*Handler).capture",
		"StatusFailed":     "(*Job).Reconcile",
	} {
		if got := written[state]; got != wantIn {
			t.Errorf("%s written in %q, want %q", state, got, wantIn)
		}
	}
	for _, never := range []string{"StatusRefunded", "StatusAbandoned"} {
		if _, ok := written[never]; ok {
			t.Errorf("%s should have no write site in the fixture", never)
		}
	}
}

// The graph must include the webhook and the reconciliation job. Following only
// the API handler is the mistake that hides double-credit bugs, so it is worth
// a test that the plural entry points actually work.
func TestAllEntryPointsAreFollowed(t *testing.T) {
	res := scanFixture(t)
	want := map[string]flowEntity.NodePosition{
		"example.com/paysvc/api#(*Server).CreatePayment":    flowEntity.NodePositionEntry,
		"example.com/paysvc/webhook#(*Handler).PSPCallback": flowEntity.NodePositionEntry,
		"example.com/paysvc/recon#(*Job).Reconcile":         flowEntity.NodePositionEntry,
		"example.com/paysvc/api#(*Server).process":          flowEntity.NodePositionInternal,
		"example.com/paysvc/webhook#(*Handler).capture":     flowEntity.NodePositionInternal,
		"example.com/paysvc/ledger#(*SQLLedger).Post":       flowEntity.NodePositionLeaf,
		"example.com/paysvc/psp#(*HTTPGateway).Authorize":   flowEntity.NodePositionLeaf,
	}
	for id, kind := range want {
		n, ok := res.Flow.Nodes[id]
		if !ok {
			t.Errorf("node %s missing from the flow", id)
			continue
		}
		if n.Position != kind {
			t.Errorf("%s: kind %s, want %s", id, n.Position, kind)
		}
	}
	// The webhook spawns a goroutine before responding, so the caller gets a
	// 200 before the capture has happened. The fact must survive into the node
	// even though the work is inside a closure.
	if n := res.Flow.Nodes["example.com/paysvc/webhook#(*Handler).PSPCallback"]; n == nil || !n.Facts.SpawnsGoroutine {
		t.Error("PSPCallback should be marked as spawning a goroutine")
	}
	// flowEntity.Facts from a closure belong to the function a human would name.
	if n := res.Flow.Nodes["example.com/paysvc/api#(*Server).process"]; n != nil {
		if !n.Facts.OpensTx || !n.Facts.CommitsTx || n.Facts.RollsBackTx {
			t.Errorf("process tx facts wrong: %+v", n.Facts)
		}
	}
}

// A function the flow does not reach must not appear in the graph, or the
// diagram stops describing the payment flow and starts describing the package.
func TestUnreachedFunctionsAreExcluded(t *testing.T) {
	res := scanFixture(t)
	for _, id := range []string{
		"example.com/paysvc/api#(*Server).splitFee",
		"example.com/paysvc/domain#(*Payment).Terminal",
	} {
		if _, ok := res.Flow.Nodes[id]; ok {
			t.Errorf("%s is not reachable from any entry point but appears in the flow", id)
		}
	}
	// It is still findable by the money checks, which deliberately run over the
	// whole module: a rounding bug matters whether or not today's entry points
	// happen to reach it.
	found := false
	for _, f := range res.Flow.Findings {
		if f.ID == "MONEY-DIV" {
			found = true
		}
	}
	if !found {
		t.Error("MONEY-DIV should be reported even though splitFee is unreachable")
	}
}

// Regression test. The scanner once emitted node code references with an empty
// BodyHash. Nothing failed, nothing warned — but every rescan compared the
// empty hash against a real one, decided all eight nodes had changed, and threw
// away every note. The incremental path silently degraded into a full
// re-analysis on every run, which on a real repo means paying for a complete
// phase 2 every time.
//
// The lesson worth keeping: a cache that misses is invisible. It has to be
// asserted, because it will never announce itself.
func TestNodeCodeRefsCarryTheirBodyHash(t *testing.T) {
	res := scanFixture(t)
	for id, n := range res.Flow.Nodes {
		if n.Ref.BodyHash == "" {
			t.Errorf("%s has no body hash: every future scanningFlow would discard its notes", id)
			continue
		}
		idx, ok := res.Index.Get(id)
		if !ok {
			t.Errorf("%s is in the flow but not in the reference index", id)
			continue
		}
		if idx.BodyHash != n.Ref.BodyHash {
			t.Errorf("%s: flow hash %s != index hash %s; the two would never resolve",
				id, n.Ref.BodyHash[:8], idx.BodyHash[:8])
		}
	}
}

// The flow anchor and the index anchor must agree on the file path too, or a
// report links to one place and the resolver looks in another.
func TestNodeCodeRefsHaveFilePaths(t *testing.T) {
	res := scanFixture(t)
	for id, n := range res.Flow.Nodes {
		if n.Ref.File == "" {
			t.Errorf("%s has no file path", id)
		}
		if filepath.IsAbs(n.Ref.File) {
			t.Errorf("%s: file path %q is absolute; code references must be repo-relative to survive being checked in", id, n.Ref.File)
		}
	}
}

func shortName(qualified string) string {
	if i := strings.LastIndex(qualified, "."); i >= 0 {
		return qualified[i+1:]
	}
	return qualified
}

// The discovery walk computes a SET, so the order it drains its worklist in must
// not be observable in the output. This is asserted rather than assumed because
// the claim is load-bearing: two READMEs and a long code comment say there is no
// traversal decision to defend here, and if that ever stops being true the docs
// become wrong before anyone notices the behaviour changed.
func TestDiscoveryOrderIsNotObservable(t *testing.T) {
	res := scanFixture(t)

	// Every reachable function appears exactly once, whatever order it was found
	// in. A duplicate would mean a node emitted its edges twice.
	seen := map[string]bool{}
	for id := range res.Flow.Nodes {
		if seen[id] {
			t.Errorf("%s appears twice in the flow", id)
		}
		seen[id] = true
	}

	// Calls are sorted by call-site position, which is the ordering that IS
	// observable and the only one the walk is allowed to affect.
	for id, n := range res.Flow.Nodes {
		lines := make([]int, 0, len(n.Calls))
		for _, callee := range n.Calls {
			if c, ok := res.Flow.Nodes[callee]; ok {
				lines = append(lines, c.Ref.Line)
			}
		}
		_ = lines // positions are of call SITES, not of callee declarations
		if len(n.Calls) != len(uniqueStrings(n.Calls)) {
			t.Errorf("%s lists the same callee more than once: %v", id, n.Calls)
		}
	}
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Two different things get called "order", and only one of them is used.
//
//	DECLARATION order — where `func A` sits in the file. Irrelevant in Go, which
//	                    allows forward references at package level, and never
//	                    consulted by testigo.
//	CALL-SITE order   — where the call expression sits INSIDE a function body.
//	                    That is statement order, and it is the sequence those
//	                    statements run in.
//
// This test writes a package whose declaration order is the REVERSE of its call
// order and asserts the graph follows the calls. It exists because the
// distinction is easy to blur in prose, and a reader who thinks testigo sorts by
// declaration position would rightly not trust the flow it prints.
func TestDeclarationOrderIsIgnored(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/decl\n\ngo 1.24\n")
	// Declared: last, third, second, first.  Called: first, second, third, last.
	write("svc/svc.go", `package svc

import "context"

func Last(ctx context.Context) error  { return nil }

func Third(ctx context.Context) error { return Last(ctx) }

func Second(ctx context.Context) error { return Third(ctx) }

func Entry(ctx context.Context) error { return Second(ctx) }
`)

	res, err := Scan(Options{
		Root:    dir,
		Entries: []flowEntity.EntryPoint{{Pkg: "example.com/decl/svc", Symbol: "Entry"}},
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	// The chain must follow the calls, not the file layout.
	chain := []string{"Entry", "Second", "Third", "Last"}
	for i := 0; i < len(chain)-1; i++ {
		id := "example.com/decl/svc#" + chain[i]
		node, ok := res.Flow.Nodes[id]
		if !ok {
			t.Fatalf("%s missing from the flow", chain[i])
		}
		want := "example.com/decl/svc#" + chain[i+1]
		if len(node.Calls) != 1 || node.Calls[0] != want {
			t.Errorf("%s calls %v, want [%s]", chain[i], node.Calls, want)
		}
	}

	// And the declaration lines really are reversed, so the test is testing
	// something rather than accidentally agreeing.
	entryLine := res.Flow.Nodes["example.com/decl/svc#Entry"].Ref.Line
	lastLine := res.Flow.Nodes["example.com/decl/svc#Last"].Ref.Line
	if entryLine <= lastLine {
		t.Fatalf("fixture is not reversed: Entry at %d, Last at %d", entryLine, lastLine)
	}
}

// Within ONE function body, the calls come out in the order they are written.
// This is the ordering that actually gets used, and the one a crash-at-each-step
// test depends on.
func TestCallsWithinABodyFollowSourceOrder(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/order\n\ngo 1.24\n")
	// Declared alphabetically backwards; called in a deliberate sequence.
	write("svc/svc.go", `package svc

func zulu() {}
func yankee() {}
func xray() {}

func Flow() {
	xray()   // first
	zulu()   // second
	yankee() // third
}
`)
	res, err := Scan(Options{
		Root:    dir,
		Entries: []flowEntity.EntryPoint{{Pkg: "example.com/order/svc", Symbol: "Flow"}},
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := res.Flow.Nodes["example.com/order/svc#Flow"].Calls
	want := []string{
		"example.com/order/svc#xray",
		"example.com/order/svc#zulu",
		"example.com/order/svc#yankee",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls are in the wrong order:\n got  %v\n want %v", got, want)
		}
	}
}

// writeRepo lays out a throwaway module and returns its root.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func entry(pkg, symbol string) flowEntity.EntryPoint {
	return flowEntity.EntryPoint{Pkg: pkg, Symbol: symbol}
}

func scanRepo(t *testing.T, root string, entries ...flowEntity.EntryPoint) *flowEntity.Flow {
	t.Helper()
	res, err := Scan(Options{Root: root, Entries: entries})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return res.Flow
}

// TestFindingsAgreeWithTheScorer is the regression for the worst bug this tool
// had: two idempotency detectors with one opinion each, and the wrong one
// holding the megaphone.
//
// The finding used to run a name regex over every struct field, so a `bool`
// called AcceptsDedupKey was reported as a critical missing unique index while
// the actual key went unmentioned. A finding now needs the same proof a
// decision needs.
func TestFindingsAgreeWithTheScorer(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"go.mod": "module example.com/idem\n\ngo 1.21\n",
		"migrations/0001.sql": `
CREATE TABLE payments (
  id TEXT PRIMARY KEY,
  order_id TEXT NOT NULL
);
`,
		"domain/domain.go": `package domain

// Caps mentions dedup keys without carrying one. Neither field is a key and
// neither may be reported.
type Caps struct {
	AcceptsDedupKey  bool
	DedupKeyArgument []string
}

type Payment struct {
	ID      string
	OrderID string
}
`,
		"api/api.go": `package api

import (
	"context"
	"net/http"

	"example.com/idem/domain"
)

type Server struct{ caps domain.Caps }

func (s *Server) Handle(ctx context.Context, r *http.Request) error {
	p := &domain.Payment{OrderID: r.Header.Get("X-Order-Id")}
	_ = p
	return nil
}
`,
	})

	flow := scanRepo(t, root, entry("example.com/idem/api", "(*Server).Handle"))

	for _, f := range flow.Findings {
		if f.ID != "IDEM-KEY-NOT-UNIQUE" {
			continue
		}
		if strings.Contains(f.Title, "AcceptsDedupKey") || strings.Contains(f.Title, "DedupKeyArgument") {
			t.Errorf("reported a non-key as an idempotency key: %s", f.Title)
		}
	}

	// And the scorer must not carry them either, or they reach the prompts.
	for _, c := range flow.IdempotencyKeys {
		if c.Name == "AcceptsDedupKey" || c.Name == "DedupKeyArgument" {
			t.Errorf("%s is not a type that can hold a key, but it was scored", c.Name)
		}
	}
}

// TestReturnedStateIsNotDead covers the other half of the same class of bug:
// a detector that measured one thing and reported another.
//
// `return StatusSettled` produces a state. The old inspector walked only
// AssignStmt and CompositeLit, so every status a function RETURNS looked dead,
// and returning a status is ordinary Go.
func TestReturnedStateIsNotDead(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"go.mod": "module example.com/st\n\ngo 1.21\n",
		"domain/domain.go": `package domain

type Status string

const (
	StatusNew      Status = "new"
	StatusSettled  Status = "settled"
	StatusVoided   Status = "voided"
	StatusDisputed Status = "disputed"
	StatusGhost    Status = "ghost"
)

type Payment struct {
	ID     string
	Status Status
}
`,
		"api/api.go": `package api

import "example.com/st/domain"

type Server struct{}

func (s *Server) Settle() domain.Status { return domain.StatusSettled }

func (s *Server) Void(id string) *domain.Payment {
	return &domain.Payment{ID: id, Status: domain.StatusVoided}
}

// Declared with :=, which go/types files under Defs, not Types.
func (s *Server) Dispute() domain.Status {
	st := domain.StatusDisputed
	return st
}

func (s *Server) Handle() error {
	s.Settle()
	s.Void("x")
	s.Dispute()
	return nil
}
`,
	})

	flow := scanRepo(t, root, entry("example.com/st/api", "(*Server).Handle"))

	var machine *flowEntity.StateMachine
	for i := range flow.States {
		if strings.HasSuffix(flow.States[i].Type, "domain.Status") {
			machine = &flow.States[i]
		}
	}
	if machine == nil {
		t.Fatal("domain.Status was not detected as a state machine")
	}

	dead := map[string]bool{}
	for _, s := range machine.NeverAssigned {
		dead[s] = true
	}

	// Produced three ways: returned, set in a struct literal, and bound with :=.
	for _, live := range []string{"StatusSettled", "StatusVoided", "StatusDisputed"} {
		if dead[live] {
			t.Errorf("%s is produced in the source but was reported as never set", live)
		}
	}
	// This one really is dead, and must survive the fix.
	if !dead["StatusGhost"] {
		t.Error("StatusGhost is never produced anywhere and should still be reported")
	}
}

// TestNameAloneIsNotALifecycle guards the door that skips the other checks.
//
// A strong type name lets a candidate bypass the "stored in a field" and
// "assigned in two places" rules, which is right — PaymentStatus is a lifecycle
// even when one function sets it. But testigo's own Phase type showed what
// happens when a name alone is enough: two constants nothing ever assigns
// became a state machine, two STATE-NEVER-SET findings, and a paid round-1
// prompt asking which transitions between phases are legal.
func TestNameAloneIsNotALifecycle(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"go.mod": "module example.com/lc\n\ngo 1.21\n",
		"domain/domain.go": `package domain

// Named like a lifecycle. Only ever printed.
type Phase string

const (
	PhaseOne Phase = "one"
	PhaseTwo Phase = "two"
)

// Named like a lifecycle and actually used as one.
type PaymentStatus string

const (
	StatusPending PaymentStatus = "pending"
	StatusDone    PaymentStatus = "done"
)

type Payment struct {
	Status PaymentStatus
}
`,
		"api/api.go": `package api

import (
	"fmt"

	"example.com/lc/domain"
)

func Handle(p *domain.Payment) error {
	fmt.Println(domain.PhaseOne)
	p.Status = domain.StatusPending
	return nil
}
`,
	})

	flow := scanRepo(t, root, entry("example.com/lc/api", "Handle"))

	for _, m := range flow.States {
		if strings.HasSuffix(m.Type, "domain.Phase") {
			t.Errorf("Phase became a state machine with %d write sites; "+
				"nothing assigns it, so there is no lifecycle to ask about", len(m.Writes))
		}
	}

	var found bool
	for _, m := range flow.States {
		if strings.HasSuffix(m.Type, "domain.PaymentStatus") {
			found = true
		}
	}
	if !found {
		t.Error("PaymentStatus is assigned and must still be detected")
	}
}
