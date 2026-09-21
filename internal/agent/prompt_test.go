package agent

import (
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/agent/prompts"
)

func TestNothingConstantAcrossAKindIsRepeatedPerQuestion(t *testing.T) {
	f := batchedFixture()

	checked := 0
	for _, b := range Batches(Plan(f, domain.NewAgentResponse(), domain.RoundUnderstand)) {
		if b.Single() {
			continue
		}
		checked++

		shared := lineSet(b.Asks[0].Prompt)
		for _, a := range b.Asks[1:] {
			next := lineSet(a.Prompt)
			for l := range shared {
				if !next[l] {
					delete(shared, l)
				}
			}
		}

		const (
			maxRepeatedLine  = 95
			maxRepeatedTotal = 800
		)

		total := 0
		var worst string
		for l := range shared {
			if len(l) < 30 {
				continue
			}
			total += len(l)
			if len(l) > len(worst) {
				worst = l
			}
		}

		if len(worst) > maxRepeatedLine {
			t.Errorf(`%s: a %d-byte line appears in all %d of its questions.
  That is a rule or a goal, and anything constant across a kind belongs in
  PREAMBLE.md, not copied into every prompt:
    %.130s`, b.ID(), len(worst), len(b.Asks), worst)
		}
		if total > maxRepeatedTotal {
			t.Errorf(`%s: %d bytes are identical in EVERY one of its %d questions (%d bytes wasted).
  Usually a JSON example or a block of instructions that belongs in PREAMBLE.md.`,
				b.ID(), total, len(b.Asks), total*(len(b.Asks)-1))
		}
	}

	if checked < 2 {
		t.Fatalf("only %d multi-question batch(es) to check; this test proves nothing", checked)
	}
}

func lineSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out[l] = true
		}
	}
	return out
}

func TestTrimCutsTheRightThingAndSaysSo(t *testing.T) {
	big := strings.Repeat("a line of supporting detail nobody strictly needs\n", 200)

	p := prompts.New("a prompt").
		Fact(prompts.Proof, "load bearing", "the money type is pkg.Money at pay.go:12").
		Fact(prompts.Illustrative, "extra examples", big).
		Fact(prompts.Supporting, "more write sites", big)

	before := p.Chars()
	dropped := p.Trim(2000)

	switch {
	case len(dropped) == 0:
		t.Fatalf("a %d-character prompt was not trimmed to 2000", before)
	case p.Chars() >= before:
		t.Errorf("trim did not shrink anything: %d -> %d", before, p.Chars())
	}

	out := p.Render()

	if !strings.Contains(out, "pkg.Money at pay.go:12") {
		t.Error("Trim cut proof-priority context; that makes answers wrong, not cheaper")
	}

	if dropped[0] != "extra examples" {
		t.Errorf("cut %q first, want the illustrative block: order is what makes a budget safe", dropped[0])
	}

	for _, must := range []string{"Left out to keep this short", "extra examples", "testigo/flow.json"} {
		if !strings.Contains(out, must) {
			t.Errorf("the prompt does not say %q was removed, so the agent will go looking for it", must)
		}
	}
}

func TestEveryRoundOnePromptStatesItsGoalAndItsFacts(t *testing.T) {
	f := fixtureFlow()
	for _, a := range Plan(f, domain.NewAgentResponse(), domain.RoundUnderstand) {
		for _, must := range []string{"## Goal", "## What is already known"} {
			if !strings.Contains(a.Prompt, must) {
				t.Errorf("%s has no %q section", a.ID(), must)
			}
		}
		if !strings.Contains(a.Prompt, "PREAMBLE.md") {
			t.Errorf("%s never points at PREAMBLE.md, so the shared rules never reach the agent", a.ID())
		}
	}
}
