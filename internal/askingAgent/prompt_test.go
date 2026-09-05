package askingAgent

import (
	"strings"
	"testing"

	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
	"github.com/katayunak/testigo/internal/askingAgent/prompts"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// TestNothingConstantAcrossAKindIsRepeatedPerQuestion is the rule that makes the
// Prompt structure pay for itself, asserted.
//
// The structure was a cost REGRESSION the first time it was wired up: giving
// every prompt its own Goals and Constraints copied the same three hundred words
// into thirty-four files and added eight thousand tokens to a round. On the
// externalEffect batch alone — twenty-seven questions with identical rules — it
// was thirty-five kilobytes to say the same four things over and over.
//
// The rule that came out of it: anything constant across a KIND belongs in
// PREAMBLE.md, and only what varies per question belongs in the prompt. This
// test is what stops someone helpfully moving it back.
func TestNothingConstantAcrossAKindIsRepeatedPerQuestion(t *testing.T) {
	f := fixtureFlow()
	// Enough seams that externalEffect is a real batch rather than one question.
	f.Seams = append(f.Seams,
		flowEntity.Seam{In: f.Seams[0].In, Kind: flowEntity.SeamHTTP,
			Target: "(example.com/paysvc/psp.Gateway).Capture", Line: 71,
			Injectable: true, Iface: "example.com/paysvc/psp.Gateway"},
		flowEntity.Seam{In: f.Seams[0].In, Kind: flowEntity.SeamQueue,
			Target: "(example.com/paysvc/bus.Publisher).Publish", Line: 80,
			Injectable: true, Iface: "example.com/paysvc/bus.Publisher"},
	)

	checked := 0
	for _, b := range Batches(Plan(f, askEntity.NewAgentResponse(), askEntity.RoundUnderstand)) {
		if b.Single() {
			continue
		}
		checked++

		// Lines every question in the batch carries. Short ones are structure —
		// headings, blank lines, list markers — and cost nothing worth counting.
		shared := lineSet(b.Asks[0].Prompt)
		for _, a := range b.Asks[1:] {
			next := lineSet(a.Prompt)
			for l := range shared {
				if !next[l] {
					delete(shared, l)
				}
			}
		}

		// Two signals, because the waste comes in two shapes and one threshold
		// cannot see both.
		//
		// A copied RULE or GOAL is one long line. The longest line that
		// legitimately repeats is the pointer to PREAMBLE.md at about seventy
		// bytes, so anything past ninety-five is prose that should have been
		// hoisted.
		//
		// A copied EXAMPLE is many short lines. The first version of this test
		// only looked at lines over sixty bytes and sailed past a 1.2 KB JSON
		// block copied into every state-machine question, because each of its
		// lines was forty-eight. So the total is measured too, at a granularity
		// that can see it.
		//
		// Both are measured PER QUESTION. A copied line is waste at any batch
		// size; the batch size only decides how much it costs, and measuring the
		// total would let a small fixture hide a regression that is expensive on
		// a real repository.
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

	// A test that inspects nothing passes for the wrong reason. This one did
	// exactly that on the first attempt — the fixture produced one question per
	// kind, every batch was Single, the loop body never ran, and it reported
	// success while the regression it exists to catch was sitting in the code.
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

// TestTrimCutsTheRightThingAndSaysSo.
//
// Trim is a safety net — it does not fire on a normal prompt, and it should not:
// a budget that trims everything is a budget silently deciding what the agent
// may know. What it must do is behave correctly when a pathological prompt does
// arrive, which is why this is a unit test rather than an observation about a
// real run.
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

	// Proof survives. Cutting it would make the agent re-derive the fact from
	// source, which costs more than the fact did.
	if !strings.Contains(out, "pkg.Money at pay.go:12") {
		t.Error("Trim cut proof-priority context; that makes answers wrong, not cheaper")
	}
	// The lowest priority goes first.
	if dropped[0] != "extra examples" {
		t.Errorf("cut %q first, want the illustrative block: order is what makes a budget safe", dropped[0])
	}
	// And the cut is announced. A silent omission sends the agent to read the
	// repository, which is far more expensive than the block that was saved.
	for _, must := range []string{"Left out to keep this short", "extra examples", "testigo/flow.json"} {
		if !strings.Contains(out, must) {
			t.Errorf("the prompt does not say %q was removed, so the agent will go looking for it", must)
		}
	}
}

// TestEveryRoundOnePromptStatesItsGoalAndItsFacts. The structure exists so a
// section cannot go missing during a compaction, which is exactly how the money
// model prompt lost two load-bearing rules.
func TestEveryRoundOnePromptStatesItsGoalAndItsFacts(t *testing.T) {
	f := fixtureFlow()
	for _, a := range Plan(f, askEntity.NewAgentResponse(), askEntity.RoundUnderstand) {
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
