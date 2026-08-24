package askingAgent

import (
	"encoding/json"
	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
	"github.com/katayunak/testigo/internal/askingAgent/prompts"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/codeRef"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
)

func fixtureFlow() *flowEntity.Flow {
	f := flowEntity.NewFlow("example.com/paysvc")
	f.Entries = []flowEntity.EntryPoint{
		{Pkg: "example.com/paysvc/api", Symbol: "(*Server).CreatePayment", Label: "API create"},
	}
	entry := &flowEntity.Node{
		Ref: codeRef.CodeRef{Pkg: "example.com/paysvc/api", Symbol: "(*Server).CreatePayment",
			File: "api/server.go", Line: 21, BodyHash: "h-entry"},
		Position: flowEntity.NodePositionEntry,
		Calls:    []string{"example.com/paysvc/api#(*Server).process"},
	}
	process := &flowEntity.Node{
		Ref: codeRef.CodeRef{Pkg: "example.com/paysvc/api", Symbol: "(*Server).process",
			File: "api/server.go", Line: 35, BodyHash: "h-process"},
		Position: flowEntity.NodePositionInternal,
		Facts:    flowEntity.Facts{OpensTx: true, TouchesNet: true, WritesStatus: []string{"StatusPending"}},
	}
	f.Nodes[entry.Ref.ID()] = entry
	f.Nodes[process.Ref.ID()] = process
	f.Seams = []flowEntity.Seam{
		{In: process.Ref, Kind: flowEntity.SeamHTTP, Target: "(example.com/paysvc/psp.Gateway).Authorize",
			Line: 61, Injectable: true, Iface: "example.com/paysvc/psp.Gateway"},
		{In: process.Ref, Kind: flowEntity.SeamDB, Target: "(*database/sql.DB).BeginTx", Line: 47},
		{In: process.Ref, Kind: flowEntity.SeamClock, Target: "time.Now", Line: 26},
	}
	f.Machines = []flowEntity.StateMachine{{
		Type:   "example.com/paysvc/domain.PaymentStatus",
		Field:  "Status",
		States: []string{"StatusAuthorized", "StatusCaptured", "StatusFailed", "StatusPending"},
		Writes: []flowEntity.StateWrite{{In: process.Ref, To: "StatusPending", Line: 44, InTx: true}},
	}}
	return f
}

// Every question must be worth its cost. A node whose body has not changed
// already has a valid answer, and asking again buys nothing.
func TestPlanSkipsNodesWithFreshNotes(t *testing.T) {
	f := fixtureFlow()
	id := "example.com/paysvc/api#(*Server).process"
	f.Nodes[id].Notes = &flowEntity.Notes{Step: "reserve funds", ForHash: "h-process"}

	for _, a := range Plan(f, askEntity.NewKnowledge(), askEntity.RoundUnderstand) {
		if a.Kind == askEntity.KindNotes && a.Subject == id {
			t.Fatal("re-asked for notes on a function whose body has not changed")
		}
	}

	// ...but a changed body must be re-asked, or the notes describe code that
	// no longer exists.
	f.Nodes[id].Ref.BodyHash = "h-process-EDITED"
	found := false
	for _, a := range Plan(f, askEntity.NewKnowledge(), askEntity.RoundUnderstand) {
		if a.Kind == askEntity.KindNotes && a.Subject == id {
			found = true
		}
	}
	if !found {
		t.Fatal("did not re-ask about a function whose body changed")
	}
}

// Two runs on an unchanged repository must produce byte-identical prompts, or
// every prompt cache misses and two runs cannot be diffed.
func TestPlanIsDeterministic(t *testing.T) {
	a := Plan(fixtureFlow(), askEntity.NewKnowledge(), askEntity.RoundUnderstand)
	b := Plan(fixtureFlow(), askEntity.NewKnowledge(), askEntity.RoundUnderstand)
	if len(a) != len(b) {
		t.Fatalf("different ask counts: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID() != b[i].ID() {
			t.Fatalf("ask %d: %s vs %s", i, a[i].ID(), b[i].ID())
		}
		if a[i].Prompt != b[i].Prompt {
			t.Fatalf("ask %s: prompt text differs between runs", a[i].ID())
		}
	}
}

// Binding is asked first because everything else is built on it.
func TestBindingIsAskedFirst(t *testing.T) {
	asks := Plan(fixtureFlow(), askEntity.NewKnowledge(), askEntity.RoundUnderstand)
	if len(asks) == 0 || asks[0].Kind != askEntity.KindBinding {
		t.Fatalf("first ask is %v, want binding", asks[0].Kind)
	}
}

// Clock and randomness are not retry-safety questions. Asking about them costs
// money and teaches nothing.
func TestNoForeignEffectAskForClock(t *testing.T) {
	for _, a := range Plan(fixtureFlow(), askEntity.NewKnowledge(), askEntity.RoundUnderstand) {
		if a.Kind == askEntity.KindExternalEffect && strings.Contains(a.Subject, "time.Now") {
			t.Fatal("asked whether retrying time.Now makes the money move twice")
		}
	}
}

// The payoff of a bounded question: completeness is mechanical.
func TestTransitionsMustCoverEveryDeclaredState(t *testing.T) {
	states := []string{"StatusPending", "StatusAuthorized", "StatusCaptured", "StatusFailed"}

	missing := &askEntity.TransitionsAnswer{MayMoveTo: map[string][]string{
		"StatusPending":    {"StatusAuthorized"},
		"StatusAuthorized": {"StatusCaptured"},
		"StatusCaptured":   {},
		// StatusFailed omitted
	}}
	err := missing.ValidateAgainst(states)
	if err == nil || !strings.Contains(err.Error(), "StatusFailed") {
		t.Fatalf("a forgotten state should be an error, got %v", err)
	}

	invented := &askEntity.TransitionsAnswer{MayMoveTo: map[string][]string{
		"StatusPending":    {"StatusSettled"}, // not a declared state
		"StatusAuthorized": {}, "StatusCaptured": {}, "StatusFailed": {},
	}}
	if err := invented.ValidateAgainst(states); err == nil || !strings.Contains(err.Error(), "StatusSettled") {
		t.Fatalf("an invented state should be an error, got %v", err)
	}
}

// Illegal transitions are the complement of the allowed set, minus the pairs the
// agent explicitly said it was unsure about. Guesses must not become tests.
func TestIllegalTransitionsExcludeUnsure(t *testing.T) {
	states := []string{"StatusPending", "StatusCaptured"}
	a := &askEntity.TransitionsAnswer{MayMoveTo: map[string][]string{
		"StatusPending":  {"StatusCaptured"},
		"StatusCaptured": {},
	}}
	a.Unsure = append(a.Unsure, struct {
		From string `json:"from"`
		To   string `json:"to"`
		Why  string `json:"why"`
	}{From: "StatusCaptured", To: "StatusPending", Why: "refund may reuse the row"})

	for _, pair := range a.Illegal(states) {
		if pair == [2]string{"StatusCaptured", "StatusPending"} {
			t.Fatal("a pair the agent flagged as unsure became an illegal-transition test")
		}
	}
}

// The most common way a model fails this task is answering the illustration
// instead of the repository.
func TestBindingRejectsThePlaceholderExample(t *testing.T) {
	a := &askEntity.BindingAnswer{}
	a.Money.Type = "example.com/pay/domain.Money"
	a.Money.Evidence = "domain/money.go:14"
	if err := a.Validate(); err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("want a placeholder rejection, got %v", err)
	}
}

func TestBindingRequiresEvidenceForEveryClaim(t *testing.T) {
	a := &askEntity.BindingAnswer{}
	a.TransferFunc.Symbol = "pay#(*Ledger).Post"
	if err := a.Validate(); err == nil || !strings.Contains(err.Error(), "evidence") {
		t.Fatalf("a named symbol with no file:line should be rejected, got %v", err)
	}
}

// Every default in ExternalEffectAnswer leans the same way, and it is chosen
// rather than accidental. Treating an irreversible effect as reversible means a
// missing test and money moved twice. The reverse costs one unnecessary test.
func TestUnknownExternalEffectIsTreatedAsIrreversible(t *testing.T) {
	for _, raw := range []string{`"unknown"`, `null`, `""`, `"maybe"`, `{}`} {
		a := &askEntity.ExternalEffectAnswer{
			ChangesExternalState: json.RawMessage(raw),
			ReversibleByRollback: json.RawMessage(raw),
			OutcomeObservable:    json.RawMessage(raw),
			AcceptsDedupKey:      json.RawMessage(raw),
		}
		if a.Undoable() {
			t.Errorf("reversible_by_rollback %s was treated as undoable", raw)
		}
		if !a.EscapesRollback() {
			t.Errorf("an unknown effect %s was treated as contained by a rollback", raw)
		}
		if a.Observable() {
			t.Errorf("outcome_observable %s was treated as checkable after a timeout", raw)
		}
		if a.Deduplicated() {
			t.Errorf("accepts_dedup_key %s was treated as deduplicated", raw)
		}
	}
	clear := &askEntity.ExternalEffectAnswer{
		ChangesExternalState: json.RawMessage(`true`),
		ReversibleByRollback: json.RawMessage(`true`),
	}
	if !clear.Undoable() || clear.EscapesRollback() {
		t.Error("an explicit true was not honoured")
	}
}

// A state cannot be final and still have somewhere to go, unless someone wrote
// down why. That contradiction produces a confidently wrong test.
func TestFinalStateContradictionIsRejected(t *testing.T) {
	states := []string{"StatusPending", "StatusCaptured", "StatusRefunded"}
	a := &askEntity.TransitionsAnswer{
		MayMoveTo: map[string][]string{
			"StatusPending":  {"StatusCaptured"},
			"StatusCaptured": {"StatusRefunded"},
			"StatusRefunded": {},
		},
		FinalStates: []string{"StatusCaptured", "StatusRefunded"},
	}
	err := a.ValidateAgainst(states)
	if err == nil || !strings.Contains(err.Error(), "listed as final") {
		t.Fatalf("want a final-state contradiction, got %v", err)
	}

	// ...unless it is declared as a real business exception, with a reason.
	a.FinalStateExceptions = append(a.FinalStateExceptions, struct {
		From string `json:"from"`
		To   string `json:"to"`
		Why  string `json:"why"`
	}{From: "StatusCaptured", To: "StatusRefunded", Why: "a chargeback can arrive up to 40 days later"})
	if err := a.ValidateAgainst(states); err != nil {
		t.Fatalf("a documented exception should be accepted: %v", err)
	}
	if a.IsFinal("StatusCaptured") {
		t.Error("a state with a declared exception is not truly final")
	}
	if !a.IsFinal("StatusRefunded") {
		t.Error("a state with no exception should be final")
	}
}

// A sleep in a concurrency test makes its green result meaningless.
func TestGeneratedCaseRejectsSleep(t *testing.T) {
	c := &askEntity.CaseAnswer{Status: "written", ReachedAssertions: []string{"fails if never called"}}
	c.File.Path = "api/idempotency_testigo_test.go"
	c.File.Content = "package api\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) { time.Sleep(time.Second) }\n"

	err := c.Validate(planEntity.SizeSmall)
	if err == nil || !strings.Contains(err.Error(), "time.Sleep") {
		t.Fatalf("want a sleep rejection, got %v", err)
	}
}

// A small test that opens a socket or reads the wall clock is not small, and the
// difference is checkable rather than a matter of trust.
func TestSizeIsEnforcedNotDescribed(t *testing.T) {
	cases := map[string]string{
		"time.Now": "package api\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) { _ = time.Now() }\n",
		"os.Open":  "package api\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) { _, _ = os.Open(\"x\") }\n",
		"sql.Open": "package api\n\nimport (\n\t\"database/sql\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) { _, _ = sql.Open(\"pg\", \"\") }\n",
	}
	for banned, src := range cases {
		problems := testPlan.CheckSize("x_test.go", src, planEntity.SizeSmall)
		if len(problems) == 0 {
			t.Errorf("a small test calling %s was accepted", banned)
		}
	}

	clean := "package api\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = 1 }\n"
	if problems := testPlan.CheckSize("x_test.go", clean, planEntity.SizeSmall); len(problems) != 0 {
		t.Errorf("a clean small test was rejected: %v", problems)
	}
	// Medium tests are defined by permission, and permission is not checkable.
	if problems := testPlan.CheckSize("x_test.go", cases["sql.Open"], planEntity.SizeMedium); len(problems) != 0 {
		t.Errorf("a medium test was held to the small predicate: %v", problems)
	}
}

// A test that cannot prove the interesting situation happened reports green
// having checked nothing.
func TestWrittenCaseMustProveItDidSomething(t *testing.T) {
	c := &askEntity.CaseAnswer{Status: "written"}
	c.File.Path = "api/x_testigo_test.go"
	c.File.Content = "package api\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n"
	err := c.Validate(planEntity.SizeSmall)
	if err == nil || !strings.Contains(err.Error(), "reached_assertions") {
		t.Fatalf("want a reached-assertions rejection, got %v", err)
	}
}

// A blocked case with no reason is indistinguishable from one that was skipped.
func TestBlockedCaseMustSayWhy(t *testing.T) {
	c := &askEntity.CaseAnswer{Status: "blocked"}
	if err := c.Validate(planEntity.SizeSmall); err == nil || !strings.Contains(err.Error(), "no reason") {
		t.Fatalf("want a missing-reason rejection, got %v", err)
	}
}

// Being strict about a markdown fence would be principled and would waste money:
// the answer is right, the wrapping is wrong, and re-running costs tokens.
func TestStripFenceTolerates(t *testing.T) {
	want := `{"step":"reserve funds"}`
	for _, in := range []string{
		want,
		"```json\n" + want + "\n```",
		"```\n" + want + "\n```\n",
		"   " + want + "  ",
	} {
		if got := string(stripFence([]byte(in))); got != want {
			t.Errorf("stripFence(%q) = %q", in, got)
		}
	}
}

func TestRoundTwoIsBlockedUntilRoundOneIsAnswered(t *testing.T) {
	f := fixtureFlow()
	k := askEntity.NewKnowledge()

	if BlockedReason(f, k) == "" {
		t.Fatal("round 2 should be blocked with no answers at all")
	}
	k.Binding = &askEntity.BindingAnswer{}
	if r := BlockedReason(f, k); r == "" || !strings.Contains(r, "transitions") {
		t.Fatalf("should still be blocked on transitions, got %q", r)
	}
	k.Transitions["example.com/paysvc/domain.PaymentStatus"] = &askEntity.TransitionsAnswer{}
	if r := BlockedReason(f, k); r == "" {
		t.Fatal("should still be blocked on retry safety of the seams")
	}
	for _, target := range prompts.SeamTargets(f) {
		k.ExternalEffects[target] = &askEntity.ExternalEffectAnswer{}
	}
	if r := BlockedReason(f, k); r != "" {
		t.Fatalf("should be unblocked now, got %q", r)
	}
}

// A note written about a body that changed while the agent was working describes
// code that no longer exists.
func TestNotesAnswerRejectedWhenTheBodyMovedUnderIt(t *testing.T) {
	dir := t.TempDir()
	f := fixtureFlow()
	id := "example.com/paysvc/api#(*Server).process"

	if err := os.MkdirAll(filepath.Join(dir, answersDir), 0o755); err != nil {
		t.Fatal(err)
	}
	ask := askEntity.Ask{Kind: askEntity.KindNotes, Subject: id, ForHash: "h-process"}
	body := `{"step":"reserve funds","purpose":"holds the money","confidence":"high"}`
	if err := os.WriteFile(filepath.Join(dir, answersDir, ask.AnswerFile()), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	f.Nodes[id].Ref.BodyHash = "h-process-EDITED"
	got, err := Collect(dir, f, askEntity.NewKnowledge(), []askEntity.Ask{ask})
	if err != nil {
		t.Fatal(err)
	}
	if got.Invalid[ask.ID()] == nil {
		t.Fatal("accepted a note describing a body that had changed underneath it")
	}
	if f.Nodes[id].Notes != nil {
		t.Fatal("stale note was applied anyway")
	}
}

func TestWriteTestRefusesDangerousPaths(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{
		"../escape_test.go",
		"/etc/passwd_test.go",
		"api/helper.go", // not a _test.go file: would ship in the binary
	} {
		g := &askEntity.CaseAnswer{}
		g.File.Path = path
		g.File.Content = "package api\n"
		if _, err := WriteTest(dir, g); err == nil {
			t.Errorf("wrote %q, which should have been refused", path)
		}
	}
}

// The prompts are the product. If a rule that stops a specific failure mode gets
// edited out, this test says so.
func TestPromptsCarryTheirLoadBearingRules(t *testing.T) {
	f := fixtureFlow()
	paths := prompts.Paths(f)

	cases := []struct {
		name   string
		prompt string
		must   []string
	}{
		{"binding", prompts.Binding(f, paths, []string{"money type", "idempotency key"}), []string{
			"Do not contradict it",
			"A null answer is useful",
			"OPEN QUESTIONS",
		}},
		{"transitions", prompts.Transitions(f, f.Machines[0], paths), []string{
			"complete set", "unsure", "Is failure terminal?",
		}},
		{"externalEffect", prompts.ExternalEffect(f, "psp.Authorize", f.Seams[:1], paths), []string{
			"UNKNOWN outcome, not a failed one",
			"You are NOT being asked whether this should be retried",
			"unknown is treated as irreversible and unobservable",
			"Does a database ROLLBACK undo it",
			"Can the outcome be checked afterwards",
		}},
	}
	// A rule has to REACH the agent. It does not have to be in every prompt.
	//
	// Round one now hoists the fixed blocks into PREAMBLE.md, which the agent
	// reads once, because repeating them made a 73-function service cost a
	// million tokens. So the assertion is "the agent is told this", not "this
	// prompt repeats it" — and the preamble counts.
	pre := UnderstandPreambleFor(f)
	for _, c := range cases {
		for _, must := range c.must {
			if !strings.Contains(c.prompt, must) && !strings.Contains(pre, must) {
				t.Errorf("%s: the rule about %q reaches the agent from neither the prompt nor PREAMBLE.md", c.name, must)
			}
		}
	}
}

// UnderstandPreambleFor is a test shorthand for the round-one shared block.
func UnderstandPreambleFor(f *flowEntity.Flow) string {
	return Preamble(f, askEntity.RoundUnderstand)
}

// The shared preamble must keep the rules that were hoisted out of the per-case
// prompts. If one is edited away, nothing else carries it.
func TestPreambleCarriesTheHoistedRules(t *testing.T) {
	for _, must := range []string{
		"Never derive an expected value from the implementation",
		"this test proved nothing",
		"Size is enforced, not described",
		"Blocked is a real answer",
		"expected_to_fail",
	} {
		if !strings.Contains(prompts.Preamble, must) {
			t.Errorf("PREAMBLE lost its rule about %q", must)
		}
	}
}

// Facts must reach the case prompt, and only the facts that case needs.
func TestCasePromptCarriesItsOwnFactsAndNoMore(t *testing.T) {
	f := fixtureFlow()
	var concurrent, money planEntity.TestCase
	for _, c := range testPlan.Select(f, planEntity.Bindings{}) {
		switch c.Scenario.ID {
		case "IDEM-CONCURRENT":
			concurrent = c
		case "MINOR-UNIT-CONVERSION":
			money = c
		}
	}
	if concurrent.Scenario.ID == "" {
		t.Fatal("IDEM-CONCURRENT was not selected for the fixture")
	}

	p := prompts.TestCase(concurrent, f)
	for _, want := range []string{
		"(example.com/paysvc/psp.Gateway).Authorize",
		"fake via example.com/paysvc/psp.Gateway",
		"DONE",
		"NOT",
		"barrier",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("IDEM-CONCURRENT prompt is missing %q", want)
		}
	}

	// The saving comes from NOT pasting the whole flow into every case.
	if money.Scenario.ID != "" {
		mp := prompts.TestCase(money, f)
		if strings.Contains(mp, "(*database/sql.DB).BeginTx") {
			t.Error("a currency-conversion prompt is carrying the database call graph it has no use for")
		}
	}
}

// Hoisting the shared rules must actually be cheaper, not just tidier.
func TestSharedPreambleIsCheaperThanRepeatingIt(t *testing.T) {
	f := fixtureFlow()
	asks := Plan(f, fullKnowledge(f), askEntity.RoundGenerate)
	if len(asks) < 2 {
		t.Skip("need at least two cases to compare")
	}
	cost := Estimate(askEntity.RoundGenerate, asks, Preamble(f, askEntity.RoundGenerate))
	if cost.SavedByShared <= 0 {
		t.Fatal("no saving reported from the shared preamble")
	}
	repeated := cost.EstTokens + cost.SavedByShared
	if cost.EstTokens >= repeated {
		t.Fatalf("hoisting did not reduce the pack: %d vs %d", cost.EstTokens, repeated)
	}
}

func fullKnowledge(f *flowEntity.Flow) *askEntity.Knowledge {
	k := askEntity.NewKnowledge()
	k.Binding = &askEntity.BindingAnswer{}
	for _, m := range f.Machines {
		k.Transitions[m.Type] = &askEntity.TransitionsAnswer{}
	}
	for _, target := range prompts.SeamTargets(f) {
		k.ExternalEffects[target] = &askEntity.ExternalEffectAnswer{}
	}
	return k
}

// Phase 1 ranks candidates by evidence so round one does not pay to ask. When the
// evidence decides, no question is emitted; when it is close, a narrow question
// is. This test pins the boundary, because getting it wrong in either direction
// costs money or costs accuracy.
func TestEvidenceDecidesInsteadOfAsking(t *testing.T) {
	clear := flowEntity.Candidates{
		{Name: "IdempotencyKey", Owner: "Payment", Score: 7, Declarative: true},
		{Name: "OrderID", Owner: "Payment", Score: 4, Declarative: true},
	}
	if _, ok := clear.Decided(); !ok {
		t.Error("a 7 against a 4 should be decided without asking")
	}

	close := flowEntity.Candidates{
		{Name: "ReferenceID", Owner: "Order", Score: 5, Declarative: true},
		{Name: "IdempotencyKey", Owner: "Order", Score: 4, Declarative: true},
	}
	if _, ok := close.Decided(); ok {
		t.Error("a one-point gap is a real question, not a decision")
	}

	// A field that matched only on its name must never win alone. That was the
	// original bug: an Order carrying ID, ReferenceID and TraceID had three
	// name matches and no way to choose.
	nameOnly := flowEntity.Candidates{{Name: "ReferenceID", Owner: "Order", Score: 1, Declarative: true}}
	if _, ok := nameOnly.Decided(); ok {
		t.Error("a name match with no behavioural evidence decided the question")
	}

	// The mirror image, and the one that actually shipped wrong. On a real
	// recharge service Order.Phone scored 6 on behaviour alone — it arrives in
	// the request and it is passed to a database call, both true, both true of
	// every other query parameter in the repo. Nothing named it a key, and a
	// phone number is the TARGET of a topup, not a deduplication key.
	//
	// Behaviour says a value COULD be a key. Only a name or a unique constraint
	// says anyone meant it to be one.
	behaviourOnly := flowEntity.Candidates{{Name: "Phone", Owner: "Order", Score: 6}}
	if _, ok := behaviourOnly.Decided(); ok {
		t.Error("behavioural evidence with nothing declaring the field a key decided the question")
	}
	if behaviourOnly[0].Credible() {
		t.Error("Phone is not credible enough to raise a finding on its own")
	}

	if _, ok := (flowEntity.Candidates{}).Decided(); ok {
		t.Error("an empty list cannot decide anything")
	}
}

// TestRoundOneDoesNotRepeatTheFlowMap is the regression for the bug that made a
// 73-function service cost a million tokens to ask about.
//
// Every round-one prompt used to paste the whole step list. Measured on a real
// service that was 34 KB per prompt and 94% of each one; two prompts compared
// byte for byte came out 99% identical. Worse than the size was the shape — the
// map grows with the function count and there is roughly one prompt per
// function, so the pack was QUADRATIC.
func TestRoundOneDoesNotRepeatTheFlowMap(t *testing.T) {
	f := fixtureFlow()
	asks := Plan(f, askEntity.NewKnowledge(), askEntity.RoundUnderstand)
	if len(asks) < 2 {
		t.Skip("need at least two prompts to compare")
	}

	// The map is in the preamble, so the preamble is allowed to be large.
	pre := Preamble(f, askEntity.RoundUnderstand)
	if !strings.Contains(pre, "ENTRY POINT") {
		t.Fatal("round one's preamble does not carry the flow map")
	}

	// No individual prompt may. "step N" pointing into the shared map is fine;
	// a second copy of the steps is not.
	for _, a := range asks {
		if strings.Count(a.Prompt, "ENTRY POINT") > 1 {
			t.Errorf("%s: prompt contains the flow map more than once", a.Kind)
		}
		if strings.Contains(a.Prompt, "leaves process:") && strings.Contains(a.Prompt, "step 2") {
			t.Errorf("%s: prompt is reprinting the step list instead of citing PREAMBLE.md", a.Kind)
		}
	}

	// And the whole point: hoisting has to be cheaper than repeating.
	cost := Estimate(askEntity.RoundUnderstand, asks, pre)
	if cost.SavedByShared <= 0 {
		t.Error("round one reports no saving from hoisting the shared block")
	}
}
