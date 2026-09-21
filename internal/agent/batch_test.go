package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func TestBatchingCutsTurnsNotJustBytes(t *testing.T) {
	f := fixtureFlow()
	asks := Plan(f, domain.NewAgentResponse(), domain.RoundUnderstand)
	if len(asks) < 4 {
		t.Fatalf("fixture produced %d asks; too few to say anything about batching", len(asks))
	}

	batches := Batches(asks)
	if len(batches) >= len(asks) {
		t.Errorf("%d asks became %d files: batching bought nothing", len(asks), len(batches))
	}

	seen := map[string]int{}
	for _, b := range batches {
		for _, a := range b.Asks {
			seen[a.ID()]++
		}
	}
	for _, a := range asks {
		if seen[a.ID()] != 1 {
			t.Errorf("%s appears in %d batches, want exactly 1", a.ID(), seen[a.ID()])
		}
	}
	t.Logf("%d questions in %d files", len(asks), len(batches))
}

func TestBatchPromptNamesEveryAnswerKey(t *testing.T) {
	f := fixtureFlow()
	for _, b := range Batches(Plan(f, domain.NewAgentResponse(), domain.RoundUnderstand)) {
		if b.Single() {
			continue
		}
		p := b.Prompt()
		for _, a := range b.Asks {
			if !strings.Contains(p, "`"+a.ID()+"`") {
				t.Errorf("%s.md never prints the answer key %q, so nothing can be filed under it", b.ID(), a.ID())
			}
		}
	}
}

func TestOneBadEntryFailsOnlyItself(t *testing.T) {
	f := fixtureFlow()
	asks := Plan(f, domain.NewAgentResponse(), domain.RoundUnderstand)

	var batch Batch
	for _, b := range Batches(asks) {
		if b.Kind == domain.KindStateRoles && len(b.Asks) >= 1 {
			batch = b
		}
	}
	if batch.Kind == "" {
		t.Skip("fixture has no state machine to batch")
	}

	dir := t.TempDir()
	answers := filepath.Join(dir, answersDir)
	if err := os.MkdirAll(answers, 0o755); err != nil {
		t.Fatal(err)
	}

	good := map[string]any{"roles": []map[string]any{
		{"state": "StatusPending", "initializing": true, "evidence": "api/server.go:44"},
		{"state": "StatusAuthorized", "in_progress": true, "evidence": "api/server.go:45"},
		{"state": "StatusCaptured", "final": true, "evidence": "api/server.go:46"},
		{"state": "StatusFailed", "final": true, "evidence": "api/server.go:47"},
	}}

	bad := map[string]any{"roles": []map[string]any{
		{"state": "NOT_A_STATE", "final": true, "evidence": "x.go:1"},
	}}

	payload := map[string]any{}
	for i, a := range batch.Asks {
		if i == 0 {
			payload[a.ID()] = bad
		} else {
			payload[a.ID()] = good
		}
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(filepath.Join(answers, batch.AnswerFile()), b, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Collect(dir, f, domain.NewAgentResponse(), batch.Asks)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Invalid) != 1 {
		t.Errorf("want exactly the one bad entry invalid, got %d: %v", len(got.Invalid), got.Invalid)
	}
	if _, bad := got.Invalid[batch.Asks[0].ID()]; !bad {
		t.Errorf("the bad entry %s was accepted", batch.Asks[0].ID())
	}
	if want := len(batch.Asks) - 1; len(got.Answered) != want {
		t.Errorf("%d good answers survived, want %d — a batch must not fail together", len(got.Answered), want)
	}
}

func TestWrongShapeIsAnErrorNotSixtyMissingAnswers(t *testing.T) {
	f := fixtureFlow()
	asks := Plan(f, domain.NewAgentResponse(), domain.RoundUnderstand)

	var batch Batch
	for _, b := range Batches(asks) {
		if !b.Single() {
			batch = b
			break
		}
	}
	if batch.Kind == "" {
		t.Skip("fixture produced no multi-question batch")
	}

	dir := t.TempDir()
	answers := filepath.Join(dir, answersDir)
	if err := os.MkdirAll(answers, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(answers, batch.AnswerFile()), []byte(`{"roles": []}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Collect(dir, f, domain.NewAgentResponse(), batch.Asks)
	if err != nil {
		t.Fatal(err)
	}
	err, reported := got.Invalid[batch.ID()]
	if !reported {
		t.Fatalf("a wrong-shaped file was reported as %d missing answers instead of an error", len(got.Missing))
	}
	for _, must := range []string{"none of its keys", batch.Asks[0].ID()} {
		if !strings.Contains(err.Error(), must) {
			t.Errorf("the error does not mention %q, so it does not say how to fix it:\n%v", must, err)
		}
	}
}

func TestMigrationsReachTheAgentOnce(t *testing.T) {
	f := fixtureFlow()
	f.Infra.MigrationDirs = []string{"db/migrations"}
	f.Infra.MigrationFiles = 1
	f.Infra.Tables = []flowEntity.Table{{
		Name: "payments", File: "db/migrations/1.sql", Line: 3,
		Columns: []flowEntity.Column{
			{Name: "id", Type: "bigserial", PrimaryKey: true},
			{Name: "idempotency_key", Type: "text", NotNull: true},
		},
	}}
	f.Infra.Constraints = []flowEntity.Constraint{{
		Table: "payments", Columns: []string{"idempotency_key"},
		Kind: "unique_index", File: "db/migrations/1.sql", Line: 9,
	}}
	f.Infra.Checks = []flowEntity.Check{{
		Table: "payments", Expr: "amount > 0", File: "db/migrations/1.sql", Line: 6,
	}}

	pre := UnderstandPreambleFor(f)
	for _, must := range []string{
		"payments",
		"idempotency_key",
		"unique_index",
		"amount > 0",
		"NOT NULL",
		"db/migrations/1.sql:9",
	} {
		if !strings.Contains(pre, must) {
			t.Errorf("PREAMBLE.md does not carry %q, so the agent has to open the migrations to find it", must)
		}
	}

	for _, a := range Plan(f, domain.NewAgentResponse(), domain.RoundUnderstand) {
		if strings.Contains(a.Prompt, "amount > 0") {
			t.Errorf("%s repeats the schema that is already in PREAMBLE.md", a.ID())
		}
	}
}
