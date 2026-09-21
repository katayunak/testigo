package scanningFlow

import (
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

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

var (
	fixtureOnce sync.Once
	fixtureRes  *Result
	fixtureErr  error
)

func scanFixture(t *testing.T) *Result {
	t.Helper()
	root := fixtureRoot(t)

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
		{"MONEY-FLOAT", "domain/payment.go", flowEntity.SevCritical},
		{"MONEY-FLOAT", "api/server.go", flowEntity.SevCritical},
		{"MONEY-DIV", "api/server.go", flowEntity.SevHigh},
		{"MONEY-NO-CURRENCY", "domain/payment.go", flowEntity.SevMedium},
		{"TX-NET-CALL", "api/server.go", flowEntity.SevCritical},
		{"TX-NO-ROLLBACK", "api/server.go", flowEntity.SevHigh},
		{"STATE-NEVER-SET", "", flowEntity.SevMedium},
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

func TestInjectabilityIsCorrect(t *testing.T) {
	res := scanFixture(t)
	byTarget := map[string]flowEntity.Seam{}
	for _, s := range res.Flow.Seams {
		byTarget[s.Target] = s
	}
	cases := map[string]bool{
		"(example.com/paysvc/psp.Gateway).Authorize": true,
		"(example.com/paysvc/ledger.Ledger).Post":    true,
		"(*database/sql.DB).BeginTx":                 false,
		"(*net/http.Client).Do":                      false,
		"time.Now":                                   false,
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

	if n := res.Flow.Nodes["example.com/paysvc/webhook#(*Handler).PSPCallback"]; n == nil || !n.Facts.SpawnsGoroutine {
		t.Error("PSPCallback should be marked as spawning a goroutine")
	}

	if n := res.Flow.Nodes["example.com/paysvc/api#(*Server).process"]; n != nil {
		if !n.Facts.OpensTx || !n.Facts.CommitsTx || n.Facts.RollsBackTx {
			t.Errorf("process tx facts wrong: %+v", n.Facts)
		}
	}
}

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

func TestDiscoveryOrderIsNotObservable(t *testing.T) {
	res := scanFixture(t)

	seen := map[string]bool{}
	for id := range res.Flow.Nodes {
		if seen[id] {
			t.Errorf("%s appears twice in the flow", id)
		}
		seen[id] = true
	}

	for id, n := range res.Flow.Nodes {
		lines := make([]int, 0, len(n.Calls))
		for _, callee := range n.Calls {
			if c, ok := res.Flow.Nodes[callee]; ok {
				lines = append(lines, c.Ref.Line)
			}
		}
		_ = lines
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

	entryLine := res.Flow.Nodes["example.com/decl/svc#Entry"].Ref.Line
	lastLine := res.Flow.Nodes["example.com/decl/svc#Last"].Ref.Line
	if entryLine <= lastLine {
		t.Fatalf("fixture is not reversed: Entry at %d, Last at %d", entryLine, lastLine)
	}
}

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

	for _, c := range flow.IdempotencyKeys {
		if c.Name == "AcceptsDedupKey" || c.Name == "DedupKeyArgument" {
			t.Errorf("%s is not a type that can hold a key, but it was scored", c.Name)
		}
	}
}

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

	for _, live := range []string{"StatusSettled", "StatusVoided", "StatusDisputed"} {
		if dead[live] {
			t.Errorf("%s is produced in the source but was reported as never set", live)
		}
	}

	if !dead["StatusGhost"] {
		t.Error("StatusGhost is never produced anywhere and should still be reported")
	}
}

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
