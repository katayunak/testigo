package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/agent/prompts"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
)

func caseByID(t *testing.T, f *flowEntity.Flow, id string) testPlan.TestCase {
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
