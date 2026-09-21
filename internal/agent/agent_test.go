package agent

import (
	"encoding/json"
	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/agent/planner"
	"github.com/katayunak/testigo/internal/agent/prompts"
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
)

func fixtureFlow() *flowEntity.Flow {
	f := flowEntity.NewFlow("example.com/paysvc")
	f.Entries = []flowEntity.EntryPoint{
		{Pkg: "example.com/paysvc/api", Symbol: "(*Server).CreatePayment", Label: "API create"},
	}
	entry := &flowEntity.Node{
		Ref: flowEntity.CodeRef{Pkg: "example.com/paysvc/api", Symbol: "(*Server).CreatePayment",
			File: "api/server.go", Line: 21},
		Position: flowEntity.NodePositionEntry,
		Calls:    []string{"example.com/paysvc/api#(*Server).process"},
	}
	process := &flowEntity.Node{
		Ref: flowEntity.CodeRef{Pkg: "example.com/paysvc/api", Symbol: "(*Server).process",
			File: "api/server.go", Line: 35},
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
	f.States = []flowEntity.StateMachine{{
		Type:   "example.com/paysvc/domain.PaymentStatus",
		Field:  "Status",
		States: []string{"StatusAuthorized", "StatusCaptured", "StatusFailed", "StatusPending"},
		Writes: []flowEntity.StateWrite{{In: process.Ref, To: "StatusPending", Line: 44, InTx: true}},
	}}
	return f
}

func TestPlanIsDeterministic(t *testing.T) {
	a := Plan(fixtureFlow(), domain.NewAgentResponse(), domain.RoundUnderstand)
	b := Plan(fixtureFlow(), domain.NewAgentResponse(), domain.RoundUnderstand)
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

func TestBindingIsAskedFirst(t *testing.T) {
	asks := Plan(fixtureFlow(), domain.NewAgentResponse(), domain.RoundUnderstand)
	if len(asks) == 0 || asks[0].Kind != domain.KindMoneyModel {
		t.Fatalf("first ask is %v, want binding", asks[0].Kind)
	}
}

func TestNoForeignEffectAskForClock(t *testing.T) {
	for _, a := range Plan(fixtureFlow(), domain.NewAgentResponse(), domain.RoundUnderstand) {
		if a.Kind == domain.KindExternalEffect && strings.Contains(a.Subject, "time.Now") {
			t.Fatal("asked whether retrying time.Now makes the money move twice")
		}
	}
}

func TestTransitionsMustCoverEveryDeclaredState(t *testing.T) {
	states := []string{"StatusPending", "StatusAuthorized", "StatusCaptured", "StatusFailed"}

	missing := &domain.TransitionsAnswer{MayMoveTo: map[string][]string{
		"StatusPending":    {"StatusAuthorized"},
		"StatusAuthorized": {"StatusCaptured"},
		"StatusCaptured":   {},
	}}
	err := missing.ValidateAgainst(states)
	if err == nil || !strings.Contains(err.Error(), "StatusFailed") {
		t.Fatalf("a forgotten state should be an error, got %v", err)
	}

	invented := &domain.TransitionsAnswer{MayMoveTo: map[string][]string{
		"StatusPending":    {"StatusSettled"},
		"StatusAuthorized": {}, "StatusCaptured": {}, "StatusFailed": {},
	}}
	if err := invented.ValidateAgainst(states); err == nil || !strings.Contains(err.Error(), "StatusSettled") {
		t.Fatalf("an invented state should be an error, got %v", err)
	}
}

func TestIllegalTransitionsExcludeUnsure(t *testing.T) {
	states := []string{"StatusPending", "StatusCaptured"}
	a := &domain.TransitionsAnswer{MayMoveTo: map[string][]string{
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

func TestBindingRejectsThePlaceholderExample(t *testing.T) {
	a := &domain.MoneyModelAnswer{}
	a.Money.Type = "example.com/pay/domain.Money"
	a.Money.Proof = domain.Proof{Symbol: "Money", At: "domain/money.go:14"}
	if err := a.Validate(); err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("want a placeholder rejection, got %v", err)
	}
}

func TestBindingRequiresEvidenceForEveryClaim(t *testing.T) {
	a := &domain.MoneyModelAnswer{}
	a.TransferFunc.Symbol = "pay#(*Ledger).Post"
	if err := a.Validate(); err == nil || !strings.Contains(err.Error(), "proof") {
		t.Fatalf("a named symbol with no file:line should be rejected, got %v", err)
	}
}

func TestUnknownExternalEffectIsTreatedAsIrreversible(t *testing.T) {
	for _, raw := range []string{`"unknown"`, `null`, `""`, `"maybe"`, `{}`} {
		a := &domain.ExternalEffectAnswer{
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
	clear := &domain.ExternalEffectAnswer{
		ChangesExternalState: json.RawMessage(`true`),
		ReversibleByRollback: json.RawMessage(`true`),
	}
	if !clear.Undoable() || clear.EscapesRollback() {
		t.Error("an explicit true was not honoured")
	}
}

func TestFinalStateContradictionIsRejected(t *testing.T) {
	states := []string{"StatusPending", "StatusCaptured", "StatusRefunded"}
	a := &domain.TransitionsAnswer{
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

func TestGeneratedCaseRejectsSleep(t *testing.T) {
	c := &domain.CaseAnswer{Status: "written", ReachedAssertions: []string{"fails if never called"}}
	c.File.Path = "api/idempotency_testigo_test.go"
	c.File.Content = "package api\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) { time.Sleep(time.Second) }\n"

	err := c.Validate(testPlan.SizeSmall)
	if err == nil || !strings.Contains(err.Error(), "time.Sleep") {
		t.Fatalf("want a sleep rejection, got %v", err)
	}
}

func TestSizeIsEnforcedNotDescribed(t *testing.T) {
	cases := map[string]string{
		"time.Now": "package api\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) { _ = time.Now() }\n",
		"os.Open":  "package api\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) { _, _ = os.Open(\"x\") }\n",
		"sql.Open": "package api\n\nimport (\n\t\"database/sql\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) { _, _ = sql.Open(\"pg\", \"\") }\n",
	}
	for banned, src := range cases {
		problems := testPlan.CheckSize("x_test.go", src, testPlan.SizeSmall)
		if len(problems) == 0 {
			t.Errorf("a small test calling %s was accepted", banned)
		}
	}

	clean := "package api\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = 1 }\n"
	if problems := testPlan.CheckSize("x_test.go", clean, testPlan.SizeSmall); len(problems) != 0 {
		t.Errorf("a clean small test was rejected: %v", problems)
	}

	if problems := testPlan.CheckSize("x_test.go", cases["sql.Open"], testPlan.SizeMedium); len(problems) != 0 {
		t.Errorf("a medium test was held to the small predicate: %v", problems)
	}
}

func TestWrittenCaseMustProveItDidSomething(t *testing.T) {
	c := &domain.CaseAnswer{Status: "written"}
	c.File.Path = "api/x_testigo_test.go"
	c.File.Content = "package api\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n"
	err := c.Validate(testPlan.SizeSmall)
	if err == nil || !strings.Contains(err.Error(), "reached_assertions") {
		t.Fatalf("want a reached-assertions rejection, got %v", err)
	}
}

func TestBlockedCaseMustSayWhy(t *testing.T) {
	c := &domain.CaseAnswer{Status: "blocked"}
	if err := c.Validate(testPlan.SizeSmall); err == nil || !strings.Contains(err.Error(), "no reason") {
		t.Fatalf("want a missing-reason rejection, got %v", err)
	}
}

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
	k := domain.NewAgentResponse()

	if BlockedReason(f, k) == "" {
		t.Fatal("round 2 should be blocked with no answers at all")
	}
	k.MoneyModel = &domain.MoneyModelAnswer{}
	if r := BlockedReason(f, k); r == "" || !strings.Contains(r, "transitions") {
		t.Fatalf("should still be blocked on transitions, got %q", r)
	}
	k.Transitions["example.com/paysvc/domain.PaymentStatus"] = &domain.TransitionsAnswer{}
	if r := BlockedReason(f, k); r == "" {
		t.Fatal("should still be blocked on retry safety of the seams")
	}
	for _, target := range prompts.SeamTargets(f) {
		k.ExternalEffects[target] = &domain.ExternalEffectAnswer{}
	}
	if r := BlockedReason(f, k); r != "" {
		t.Fatalf("should be unblocked now, got %q", r)
	}
}

func TestRoundTwoIsUnblockedByOnlyTheSeamsAnyRunnableScenarioNeeds(t *testing.T) {
	f := fixtureFlow()
	k := domain.NewAgentResponse()
	k.MoneyModel = &domain.MoneyModelAnswer{}
	for _, m := range f.States {
		k.Transitions[m.Type] = &domain.TransitionsAnswer{}
	}

	f.Seams = append(f.Seams, flowEntity.Seam{
		In:         flowEntity.CodeRef{Pkg: "example.com/paysvc/unreached", Symbol: "(*Ghost).Call"},
		Kind:       flowEntity.SeamHTTP,
		Target:     "example.com/paysvc/unreached#(*Ghost).Call",
		Injectable: true,
	})

	d := planner.Demanded(f, factsOf(k))
	if len(d.Seams) == 0 {
		t.Fatal("fixture setup: expected at least one runnable scenario to demand a seam")
	}
	if all := prompts.SeamTargets(f); len(all) <= len(d.Seams) {
		t.Fatal("fixture setup: expected the flow to have seams no runnable scenario needs, or this test proves nothing")
	}

	for target := range d.Seams {
		k.ExternalEffects[target] = &domain.ExternalEffectAnswer{}
	}
	if r := BlockedReason(f, k); r != "" {
		t.Fatalf("round 2 must not stay blocked on a seam nothing runnable touches — the planner never asks about those, so this gate could never be satisfied: %q", r)
	}
}

func TestWriteTestRefusesDangerousPaths(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{
		"../escape_test.go",
		"/etc/passwd_test.go",
		"api/helper.go",
	} {
		g := &domain.CaseAnswer{}
		g.File.Path = path
		g.File.Content = "package api\n"
		if _, err := WriteTest(dir, g); err == nil {
			t.Errorf("wrote %q, which should have been refused", path)
		}
	}
}

func TestPromptsCarryTheirLoadBearingRules(t *testing.T) {
	f := fixtureFlow()
	paths := prompts.Paths(f)

	cases := []struct {
		name   string
		prompt string
		must   []string
	}{
		{"moneyModel", prompts.MoneyModel(f, paths, []string{"money type", "idempotency key"}).Render(), []string{
			"Do not contradict it",
			"A null answer is useful",
			"OPEN QUESTIONS",
		}},
		{"stateRoles", prompts.StateRoles(f, f.States[0]).Render(), []string{
			"COMPLETE", "exactly once", "is free and correct", "over-approximating on purpose",
		}},
		{"externalEffect", prompts.ExternalEffect(f, "psp.Authorize", f.Seams[:1], paths).Render(), []string{
			"UNKNOWN outcome, not a failed one",
			"You are NOT being asked whether this should be retried",
			"unknown is treated as irreversible and unobservable",
			"Does a database ROLLBACK undo it",
			"Can the outcome be checked afterwards",
		}},
	}

	pre := UnderstandPreambleFor(f)
	for _, c := range cases {
		for _, must := range c.must {
			if !strings.Contains(c.prompt, must) && !strings.Contains(pre, must) {
				t.Errorf("%s: the rule about %q reaches the agent from neither the prompt nor PREAMBLE.md", c.name, must)
			}
		}
	}
}

func UnderstandPreambleFor(f *flowEntity.Flow) string {
	return Preamble(f, domain.RoundUnderstand)
}

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

func TestCasePromptCarriesItsOwnFactsAndNoMore(t *testing.T) {
	f := fixtureFlow()
	var concurrent, money testPlan.TestCase
	for _, c := range testPlan.Select(f, testPlan.Facts{}) {
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

	p := prompts.TestCase(concurrent, f, nil).Render()
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

	if money.Scenario.ID != "" {
		mp := prompts.TestCase(money, f, nil).Render()
		if strings.Contains(mp, "(*database/sql.DB).BeginTx") {
			t.Error("a currency-conversion prompt is carrying the database call graph it has no use for")
		}
	}
}

func TestSharedPreambleIsCheaperThanRepeatingIt(t *testing.T) {
	f := fixtureFlow()
	asks := Plan(f, fullKnowledge(f), domain.RoundGenerate)
	if len(asks) < 2 {
		t.Skip("need at least two cases to compare")
	}
	cost := Estimate(domain.RoundGenerate, asks, Preamble(f, domain.RoundGenerate))
	if cost.SavedByShared <= 0 {
		t.Fatal("no saving reported from the shared preamble")
	}
	repeated := cost.EstTokens + cost.SavedByShared
	if cost.EstTokens >= repeated {
		t.Fatalf("hoisting did not reduce the pack: %d vs %d", cost.EstTokens, repeated)
	}
}

func fullKnowledge(f *flowEntity.Flow) *domain.AgentResponse {
	k := domain.NewAgentResponse()
	k.MoneyModel = &domain.MoneyModelAnswer{}
	for _, m := range f.States {
		k.Transitions[m.Type] = &domain.TransitionsAnswer{}
	}
	for _, target := range prompts.SeamTargets(f) {
		k.ExternalEffects[target] = &domain.ExternalEffectAnswer{}
	}
	return k
}

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

	nameOnly := flowEntity.Candidates{{Name: "ReferenceID", Owner: "Order", Score: 1, Declarative: true}}
	if _, ok := nameOnly.Decided(); ok {
		t.Error("a name match with no behavioural proof decided the question")
	}

	behaviourOnly := flowEntity.Candidates{{Name: "Phone", Owner: "Order", Score: 6}}
	if _, ok := behaviourOnly.Decided(); ok {
		t.Error("behavioural proof with nothing declaring the field a key decided the question")
	}
	if behaviourOnly[0].Credible() {
		t.Error("Phone is not credible enough to raise a finding on its own")
	}

	if _, ok := (flowEntity.Candidates{}).Decided(); ok {
		t.Error("an empty list cannot decide anything")
	}
}

func TestRoundOneDoesNotRepeatTheFlowMap(t *testing.T) {
	f := fixtureFlow()
	asks := Plan(f, domain.NewAgentResponse(), domain.RoundUnderstand)
	if len(asks) < 2 {
		t.Skip("need at least two prompts to compare")
	}

	pre := Preamble(f, domain.RoundUnderstand)
	if !strings.Contains(pre, "ENTRY POINT") {
		t.Fatal("round one's preamble does not carry the flow map")
	}

	for _, a := range asks {
		if strings.Count(a.Prompt, "ENTRY POINT") > 1 {
			t.Errorf("%s: prompt contains the flow map more than once", a.Kind)
		}
		if strings.Contains(a.Prompt, "leaves process:") && strings.Contains(a.Prompt, "step 2") {
			t.Errorf("%s: prompt is reprinting the step list instead of citing PREAMBLE.md", a.Kind)
		}
	}

	cost := Estimate(domain.RoundUnderstand, asks, pre)
	if cost.SavedByShared <= 0 {
		t.Error("round one reports no saving from hoisting the shared block")
	}
}

func TestTheSameFieldCannotBeKeyAndNotKey(t *testing.T) {
	newAnswer := func() *domain.MainEntityAnswer {
		a := &domain.MainEntityAnswer{}
		a.MainEntity.Struct = "Order"
		a.MainEntity.Proof = domain.Proof{Symbol: "Order", At: "domain/entity/order.go:9"}
		a.IdempotencyKey.Field = "OrderId"
		a.IdempotencyKey.Proof = domain.Proof{Symbol: "OrderId", At: "domain/entity/payment.go:46"}
		return a
	}

	a := newAnswer()
	a.OtherIdentifiers = append(a.OtherIdentifiers, domain.Identifier{
		Field:      "OrderID",
		Purpose:    "business-level order reference",
		Proof:      domain.Proof{Symbol: "Order.OrderID", At: "domain/entity/order.go:12"},
		CouldBeKey: false,
	})

	err := a.Validate()
	if err == nil {
		t.Fatal("accepted an answer that calls the same field both the key and not the key")
	}
	if !strings.Contains(err.Error(), "decide which") {
		t.Errorf("wrong reason: %v", err)
	}

	b := newAnswer()
	b.OtherIdentifiers = append(b.OtherIdentifiers, domain.Identifier{
		Field:      "RRN",
		Purpose:    "bank retrieval number, arrives after authorization",
		CouldBeKey: false,
	})

	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "file and line") {
		t.Errorf("a purpose with no citation must be refused, got %v", err)
	}

	c := newAnswer()
	c.OtherIdentifiers = append(c.OtherIdentifiers, domain.Identifier{
		Field:      "RRN",
		Purpose:    "bank retrieval number, arrives after authorization",
		Proof:      domain.Proof{Symbol: "Order.RRN", At: "domain/entity/order.go:21"},
		CouldBeKey: false,
	})

	if err := c.Validate(); err != nil {
		t.Errorf("a cited, non-conflicting identifier must be accepted: %v", err)
	}
}
