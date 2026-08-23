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
	for _, m := range res.Flow.Machines {
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
