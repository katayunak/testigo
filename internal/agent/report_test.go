package agent

import (
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/agent/prompts"
)

func reportPrompt(t *testing.T, k *domain.AgentResponse) string {
	t.Helper()
	f := fixtureFlow()
	return prompts.Report(f, k, Cases(f, k, nil), prompts.Tokens{
		AskTokens: 43000, AnswerTokens: 9000, Prompts: 65, Saved: 700000,
	}).Render()
}

func TestReportPromptIsSelfContained(t *testing.T) {
	got := reportPrompt(t, domain.NewAgentResponse())

	if strings.Contains(got, "PREAMBLE.md") {
		t.Error("the report prompt is written on its own; pointing at PREAMBLE.md sends the agent to a file it was not given")
	}
	for _, want := range []string{"ONE markdown file", "## What this is", "## Problems", "## What this cost"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not ask for %q", want)
		}
	}
}

func TestReportDemandsACitationAndAPaymentSafeFix(t *testing.T) {
	got := reportPrompt(t, domain.NewAgentResponse())

	if !strings.Contains(got, "names a file and a line") {
		t.Error("a report whose claims cannot be checked is worse than none; the prompt must demand file:line")
	}
	if !strings.Contains(got, "charge twice") {
		t.Error("this is a payment flow — the prompt must rule out fixes that can double-charge")
	}
	if !strings.Contains(got, "constraint the database enforces") {
		t.Error("for money, a database constraint beats an application check; the prompt should say so")
	}
}

func TestReportCarriesTheTokenLedgerWithoutClaimingAPrice(t *testing.T) {
	got := reportPrompt(t, domain.NewAgentResponse())

	if !strings.Contains(got, "43.0k") {
		t.Error("the token figures did not reach the prompt")
	}
	if !strings.Contains(got, "not a bill") {
		t.Error("testigo never calls an API and cannot know the price; the prompt must stop the agent inventing one")
	}
}

func TestAFailedTestReachesTheReportWithItsOutput(t *testing.T) {
	no := false
	k := domain.NewAgentResponse()
	k.TestRuns = []domain.TestRun{
		{CaseID: "IDEM-CONCURRENT", Func: "TestIdemConcurrent", File: "pay/idem_testigo_test.go",
			Status: "written", Passed: &no,
			ExpectedToFail: "no unique index covers the key",
			Output:         "--- FAIL: TestIdemConcurrent\n    both requests were accepted"},
		{CaseID: "IDEM-REPLAY", Status: "blocked", Reason: "no idempotency key was named"},
	}

	got := reportPrompt(t, k)

	for _, want := range []string{"IDEM-CONCURRENT", "FAIL", "both requests were accepted",
		"no unique index covers the key", "no idempotency key was named"} {
		if !strings.Contains(got, want) {
			t.Errorf("the report prompt lost %q, so the report cannot mention it", want)
		}
	}
	if !strings.Contains(got, "expected to fail is a FINDING") {
		t.Error("a red test that was predicted red is the product, not a bug; the prompt must say so")
	}
}

func TestFalseVerdictsAreMarkedAsTheDeparturePoint(t *testing.T) {
	no, yes := false, true
	k := domain.NewAgentResponse()
	k.PaymentKind = map[string]*domain.QuestionAnswer{
		"IDEM-REPLAY.stores_result": {Verdict: &no, Info: "nothing is stored against the key"},
		"LOST-UPDATE.locking":       {Verdict: &yes},
	}

	got := reportPrompt(t, k)

	if !strings.Contains(got, "F  IDEM-REPLAY.stores_result") {
		t.Error("a false verdict must be legible at a glance")
	}
	if !strings.Contains(got, "nothing is stored against the key") {
		t.Error("the explanation attached to a false verdict is the part worth reporting")
	}
	if !strings.Contains(got, "departs from the safe default") {
		t.Error("the prompt should tell the agent what a false verdict means")
	}
}

func TestBlockedScenariosAreReportedWithTheirReason(t *testing.T) {
	got := reportPrompt(t, domain.NewAgentResponse())

	if !strings.Contains(got, "NOT RUNNABLE") {
		t.Error("a scenario that was skipped is information; the report must be able to say which and why")
	}
	var blocked int
	for _, c := range Cases(fixtureFlow(), domain.NewAgentResponse(), nil) {
		if !c.Runnable() {
			blocked++
			if !strings.Contains(got, c.Scenario.ID) {
				t.Errorf("blocked scenario %s never reached the prompt", c.Scenario.ID)
			}
		}
	}
	if blocked == 0 {
		t.Skip("fixture blocks nothing")
	}
}
