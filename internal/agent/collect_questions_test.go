package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/katayunak/testigo/internal/agent/domain"
)

func writeAnswer(t *testing.T, dir, name string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "answers"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "answers", name), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAnAnsweredQuestionSurvivesTheDemandChanging(t *testing.T) {
	dir := t.TempDir()
	f := fixtureFlow()
	k := domain.NewAgentResponse()

	yes := true
	writeAnswer(t, dir, "questions-x.json", map[string]any{
		"LOST-UPDATE.locking": map[string]any{"verdict": yes},
	})

	ask := domain.Ask{
		Kind:      domain.KindQuestions,
		Subject:   "x",
		Questions: []string{"LOST-UPDATE.locking"},
	}

	got, err := Collect(dir, f, k, []domain.Ask{ask})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Invalid) > 0 {
		t.Fatalf("an answer to a question the ask itself recorded was rejected: %v", got.Invalid)
	}
	if k.PaymentKind["LOST-UPDATE.locking"] == nil {
		t.Fatal("the verdict was not stored")
	}
}

func TestAnAnswerToSomethingNeverAskedIsStillRefused(t *testing.T) {
	dir := t.TempDir()
	f := fixtureFlow()
	k := domain.NewAgentResponse()

	yes := true
	writeAnswer(t, dir, "questions-x.json", map[string]any{
		"IDEM-REPLAY.stores_result": map[string]any{"verdict": yes},
		"not.a.question":            map[string]any{"verdict": yes},
	})

	ask := domain.Ask{
		Kind: domain.KindQuestions, Subject: "x",
		Questions: []string{"IDEM-REPLAY.stores_result"},
	}

	got, err := Collect(dir, f, k, []domain.Ask{ask})
	if err != nil {
		t.Fatal(err)
	}
	if got.Invalid["not.a.question"] == nil {
		t.Error("an answer to a question that was never asked must still be refused")
	}
	if k.PaymentKind["IDEM-REPLAY.stores_result"] == nil {
		t.Error("the genuine answer beside it should still have been kept")
	}
}

func TestEveryQuestionIDResolvesWithoutConsultingDemand(t *testing.T) {
	for _, id := range []string{
		"LOST-UPDATE.locking",
		"CONSERVATION-UNDER-CONCURRENCY.invariant",
		"technique.concurrency.contended",
	} {
		if _, ok := domain.QuestionByID(id); !ok {
			t.Errorf("%s is in a registry but QuestionByID could not find it", id)
		}
	}
	if _, ok := domain.QuestionByID("nope.not.here"); ok {
		t.Error("QuestionByID invented a question")
	}
}
