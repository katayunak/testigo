package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/agent/prompts"
	"github.com/katayunak/testigo/internal/testPlan"
)

func caseByID(t *testing.T, f *fact.Flow, id string) testPlan.TestCase {
	t.Helper()
	for _, c := range testPlan.Select(f, testPlan.Facts{}) {
		if c.Scenario.ID == id {
			if !c.Runnable() {
				t.Skipf("%s is not runnable on the fixture: %s", id, c.Blocked)
			}
			return c
		}
	}
	t.Skipf("%s is not in the catalog", id)
	return testPlan.TestCase{}
}

func TestRoundOneAnswersReachOnlyTheirOwnCase(t *testing.T) {
	f := fixtureFlow()
	no := false
	k := domain.NewAgentResponse()
	k.PaymentKind["IDEM-CONCURRENT.exactly_one"] = &domain.QuestionAnswer{Verdict: &no, Info: "no unique index covers the key"}
	k.PaymentKind["MINOR-UNIT-CONVERSION.boundaries"] = &domain.QuestionAnswer{Info: "JPY and KWD are handled"}
	k.ExternalEffects["(example.com/paysvc/psp.Gateway).Authorize"] = &domain.ExternalEffectAnswer{
		ChangesExternalState: json.RawMessage(`true`),
		ReversibleByRollback: json.RawMessage(`false`),
		OutcomeObservable:    json.RawMessage(`false`),
	}

	idem := prompts.TestCase(caseByID(t, f, "IDEM-CONCURRENT"), f, k).Render()
	for _, want := range []string{
		"no unique index covers the key",
		"(example.com/paysvc/psp.Gateway).Authorize: escapes a rollback=yes",
	} {
		if !strings.Contains(idem, want) {
			t.Errorf("IDEM-CONCURRENT did not receive %q from round one", want)
		}
	}
	if strings.Contains(idem, "JPY and KWD") {
		t.Error("IDEM-CONCURRENT received an answer that belongs to MINOR-UNIT-CONVERSION; a case gets only what its own scenario requires")
	}
}

func TestAStateCaseCarriesTheRulesItMustAssert(t *testing.T) {
	f := fixtureFlow()
	k := domain.NewAgentResponse()
	k.Transitions[f.States[0].Type] = &domain.TransitionsAnswer{
		InitialState: "StatusPending",
		MayMoveTo: map[string][]string{
			"StatusPending":    {"StatusAuthorized", "StatusFailed"},
			"StatusAuthorized": {"StatusCaptured"},
			"StatusCaptured":   {},
			"StatusFailed":     {},
		},
		FinalStates: []string{"StatusCaptured", "StatusFailed"},
	}

	p := prompts.TestCase(caseByID(t, f, "FINAL-STATE-IS-FINAL"), f, k).Render()
	for _, want := range []string{"Initial state: StatusPending", "Final, nothing may leave them: StatusCaptured", "Must be refused:", "StatusCaptured -> StatusPending"} {
		if !strings.Contains(p, want) {
			t.Errorf("FINAL-STATE-IS-FINAL is missing %q", want)
		}
	}
}

func TestWithoutRoundOneTheCaseHasNoAnswersBlock(t *testing.T) {
	f := fixtureFlow()
	if strings.Contains(prompts.TestCase(caseByID(t, f, "IDEM-CONCURRENT"), f, nil).Render(), "ANSWERED IN ROUND ONE") {
		t.Error("an empty answers block is noise the agent pays to read")
	}
}

func TestACaseCapsWriteSitesAndPointsAtFlowJSON(t *testing.T) {
	f := fixtureFlow()
	c := caseByID(t, f, "FINAL-STATE-IS-FINAL")

	writes := make([]fact.StateWrite, 20)
	for i := range writes {
		writes[i] = fact.StateWrite{
			To:   "StatusPending",
			In:   fact.CodeRef{Symbol: fmt.Sprintf("Handler%d", i), File: "service/order.go"},
			Line: 10 + i,
		}
	}
	states := *c.States
	states.Writes = writes
	c.States = &states

	p := prompts.TestCase(c, f, nil).Render()
	if !strings.Contains(p, "...and 8 more write site(s), in testigo/flow.json") {
		t.Errorf("a case's write-site list should cap and point at flow.json once it runs long, got:\n%s", p)
	}
	if strings.Contains(p, "Handler19") {
		t.Error("this block is Proof priority, which Trim() never cuts — it must cap itself rather than grow with the flow, or a busy state machine blows the prompt budget silently")
	}
}

func TestACaseGetsTheClaimsItsScenarioDependsOn(t *testing.T) {
	f := fixtureFlow()
	no := false
	k := domain.NewAgentResponse()
	k.PaymentKind["IDEM-REPLAY.stores_result"] = &domain.QuestionAnswer{Verdict: &no, Info: "every repeat runs the insert again"}
	k.PaymentKind["IDEM-CONCURRENT.exactly_one"] = &domain.QuestionAnswer{Verdict: &no, Info: "no unique index covers the key"}
	k.PaymentKind["AT-LEAST-ONCE-DELIVERY-IS-SAFE.duplicate_message"] = &domain.QuestionAnswer{Verdict: &no, Info: "the consumer keeps no message ids"}

	p := prompts.TestCase(caseByID(t, f, "IDEM-CONCURRENT"), f, k).Render()
	for _, want := range []string{
		"WHAT OTHER ANSWERS ADD UP TO",
		"does not hold  Repeating a finished request returns the first result",
		"-  IDEM-REPLAY.stores_result — every repeat runs the insert again",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("IDEM-CONCURRENT is missing %q", want)
		}
	}
	if strings.Contains(p, "Two requests with the same key never both do the work") {
		t.Error("a claim built only from this case's own answer repeats what the case already reads above")
	}
	if strings.Contains(p, "the consumer keeps no message ids") {
		t.Error("a claim this scenario does not depend on reached its case")
	}
}

func TestOnlyOwnAnswersMeansNoConclusionsBlock(t *testing.T) {
	f := fixtureFlow()
	no := false
	k := domain.NewAgentResponse()
	k.PaymentKind["IDEM-CONCURRENT.exactly_one"] = &domain.QuestionAnswer{Verdict: &no, Info: "no unique index covers the key"}
	if strings.Contains(prompts.TestCase(caseByID(t, f, "IDEM-CONCURRENT"), f, k).Render(), "WHAT OTHER ANSWERS ADD UP TO") {
		t.Error("an empty or repeated conclusions block is noise the agent pays to read")
	}
}

func TestTheReportGetsConclusionsByArea(t *testing.T) {
	f := fixtureFlow()
	no, yes := false, true
	k := domain.NewAgentResponse()
	k.PaymentKind["IDEM-CONCURRENT.exactly_one"] = &domain.QuestionAnswer{Verdict: &no, Info: "no unique index covers the key"}
	k.PaymentKind["TIMEOUT-UNKNOWN-OUTCOME.requery"] = &domain.QuestionAnswer{Verdict: &yes}

	p := prompts.Report(f, k, nil, prompts.Tokens{}).Render()
	for _, want := range []string{
		"What the answers add up to, by area",
		"Seams\n  holds          After a timeout, the real outcome is found by asking the provider",
		"Main entity\n  does not hold  Two requests with the same key never both do the work.\n      -  IDEM-CONCURRENT.exactly_one\n",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the report is missing %q", want)
		}
	}
	if n := strings.Count(p, "no unique index covers the key"); n != 1 {
		t.Errorf("the reason should appear once, in the answers block, got %d", n)
	}
}
