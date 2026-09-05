package askingAgent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// TestBatchingCutsTurnsNotJustBytes is the cost argument, asserted.
//
// A real run cost $4.95 for round one on a 73-function service. The pack was 49k
// tokens; the other 9.7 MILLION were cache reads, because sixty-six questions
// answered one at a time is sixty-six turns and every turn re-reads everything
// said so far. Compacting prompts attacks the wrong term of cost = turns x
// context. This asserts the term that matters went down.
func TestBatchingCutsTurnsNotJustBytes(t *testing.T) {
	f := fixtureFlow()
	asks := Plan(f, askEntity.NewAgentResponse(), askEntity.RoundUnderstand)
	if len(asks) < 4 {
		t.Fatalf("fixture produced %d asks; too few to say anything about batching", len(asks))
	}

	batches := Batches(asks)
	if len(batches) >= len(asks) {
		t.Errorf("%d asks became %d files: batching bought nothing", len(asks), len(batches))
	}

	// Every ask must land in exactly one batch. A question that falls out of
	// the grouping is never written, never answered, and never reported
	// missing — it just silently is not asked.
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

// TestBatchPromptNamesEveryAnswerKey. The keys are the contract between the
// question file and the answer file. A key the prompt does not print is a key
// the agent cannot use, and collect reports it as missing forever.
func TestBatchPromptNamesEveryAnswerKey(t *testing.T) {
	f := fixtureFlow()
	for _, b := range Batches(Plan(f, askEntity.NewAgentResponse(), askEntity.RoundUnderstand)) {
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

// TestOneBadEntryFailsOnlyItself. The whole reason batching is safe: grouping
// the questions must not group the failures. If one malformed answer discarded
// the rest, a batch would be strictly worse than separate files.
func TestOneBadEntryFailsOnlyItself(t *testing.T) {
	f := fixtureFlow()
	asks := Plan(f, askEntity.NewAgentResponse(), askEntity.RoundUnderstand)

	var batch Batch
	for _, b := range Batches(asks) {
		if b.Kind == askEntity.KindStateRoles && len(b.Asks) >= 1 {
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
	// A state that does not exist. The answer parses; it is simply not about
	// this repository.
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

	got, err := Collect(dir, f, askEntity.NewAgentResponse(), batch.Asks)
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

// TestWrongShapeIsAnErrorNotSixtyMissingAnswers.
//
// A file that parses but matches no key used to be reported as every question
// missing, which sends a person to write sixty answers that are already there
// under the wrong names. The two problems need opposite fixes, so they must not
// look the same.
func TestWrongShapeIsAnErrorNotSixtyMissingAnswers(t *testing.T) {
	f := fixtureFlow()
	asks := Plan(f, askEntity.NewAgentResponse(), askEntity.RoundUnderstand)

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
	// Valid JSON, valid-looking, and about nothing.
	if err := os.WriteFile(filepath.Join(answers, batch.AnswerFile()), []byte(`{"roles": []}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Collect(dir, f, askEntity.NewAgentResponse(), batch.Asks)
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

// TestMigrationsReachTheAgentOnce. The schema is the cheapest information
// testigo owns and the most expensive for an agent to go and get. It belongs in
// the preamble, read once — not in every question, and not nowhere.
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
		"payments",        // the table
		"idempotency_key", // the column
		"unique_index",    // her uniqueness block
		"amount > 0",      // the database's own invariant
		"NOT NULL",        // what a fixture must set
		"db/migrations/1.sql:9",
	} {
		if !strings.Contains(pre, must) {
			t.Errorf("PREAMBLE.md does not carry %q, so the agent has to open the migrations to find it", must)
		}
	}

	// And not repeated per question, which is what made a round cost a million
	// tokens in the first place.
	for _, a := range Plan(f, askEntity.NewAgentResponse(), askEntity.RoundUnderstand) {
		if strings.Contains(a.Prompt, "amount > 0") {
			t.Errorf("%s repeats the schema that is already in PREAMBLE.md", a.ID())
		}
	}
}
