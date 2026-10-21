package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/testigo/internal/agent/domain"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func roundTwoPack(t *testing.T) (asks, answers string) {
	t.Helper()
	root := t.TempDir()
	asks = filepath.Join(root, "asks")
	answers = filepath.Join(root, "answers")
	writeFiles(t, asks, map[string]string{
		"manifest.json":   `{"round": 2, "asks": []}`,
		"PREAMBLE.md":     "shared rules",
		"INSTRUCTIONS.md": "how to answer",
		"testCase.md":     "the test cases",
		"report.md":       "the report prompt",
	})
	writeFiles(t, answers, map[string]string{
		"moneyModel.json":     "{}",
		"externalEffect.json": "{}",
		"stateRoles.json":     "{}",
		"questions.json":      "{}",
		"testCase.json":       "{}",
	})
	return asks, answers
}

func TestMeasureCountsOnlyTheAnswersTheCurrentPackAskedFor(t *testing.T) {
	asks, answers := roundTwoPack(t)
	l := Measure(asks, answers)

	if l.Round != 2 {
		t.Errorf("round = %d, want 2 from manifest.json", l.Round)
	}
	if l.Prompts != 1 || l.AnsweredPrompts != 1 || l.Missing != 0 {
		t.Errorf("prompts %d, answered %d, missing %d; want 1, 1, 0: round 1's answer files and report.md are not part of the round 2 pack",
			l.Prompts, l.AnsweredPrompts, l.Missing)
	}
}

func TestReportSaysWhichRoundItCountsAndNeverMoreAnswersThanQuestions(t *testing.T) {
	asks, answers := roundTwoPack(t)
	out := Report(&fact.Flow{Module: "example.com/x"}, &domain.AgentResponse{}, Measure(asks, answers), asks)

	if !strings.Contains(out, "round 2   1 of 1 answered") {
		t.Fatalf("summary does not say round 2, 1 of 1 answered:\n%s", out)
	}
}

func TestReportListsEveryRedTestAfterRoundTwo(t *testing.T) {
	green, red := true, false
	k := &domain.AgentResponse{TestRuns: []domain.TestRun{
		{CaseID: "IDEM-REPLAY", File: "api/idem_replay_testigo_test.go", Status: "written", Passed: &red, ExpectedToFail: "nothing reads the key back"},
		{CaseID: "TRANSFER-IS-ATOMIC", File: "api/transfer_is_atomic_testigo_test.go", Status: "written", Passed: &red},
		{CaseID: "FINAL-STATE-IS-FINAL", File: "api/final_state_is_final_testigo_test.go", Status: "written", Passed: &green},
		{CaseID: "LOST-UPDATE", Status: "blocked", Reason: "needs a Postgres driver"},
	}}
	out := Report(&fact.Flow{Module: "example.com/x"}, k, Ledger{}, "asks")

	for _, want := range []string{
		"3 written: 1 passed, 2 failed",
		"1 blocked",
		"failed    IDEM-REPLAY",
		"api/idem_replay_testigo_test.go  (expected)",
		"failed    TRANSFER-IS-ATOMIC",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "failed    FINAL-STATE-IS-FINAL") {
		t.Errorf("a passing test is listed as failed:\n%s", out)
	}
}
