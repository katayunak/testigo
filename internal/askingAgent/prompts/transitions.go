package prompts

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// Transitions asks which state transitions must be impossible.
//
// This is the cleanest bounded question testigo has. The state set is complete
// — the compiler guarantees it — so the answer space is a finite N x N matrix
// and every claim is checkable against it. Compare that with "explain this
// payment flow", where nothing constrains the answer and nothing verifies it.
//
// The question is asked per state rather than per pair. Listing "which states
// may X move to" is how a person actually reasons about a state machine, and it
// makes completeness mechanical: every declared state must appear as a key, so
// a forgotten state is a validation error rather than a silent gap.
func Transitions(f *flowEntity.Flow, m flowEntity.StateMachine, paths []Path) string {
	var b strings.Builder

	b.WriteString(`# testigo — which transitions must be impossible?

A static analyser found a status type in this repository, read its declared
constants, and located every line that assigns it. It cannot know which
transitions are ALLOWED, because that is a business rule and not a fact about
the code. That is the only thing being asked here.

## Rules

1. Use only the state names listed below. They are the complete set — the type
   system guarantees no other value can exist. Do not invent, abbreviate, or
   normalise them.
2. Every declared state must appear exactly once as a key in ` + "`may_move_to`" + `.
3. A state that nothing can move on from is terminal: give it an empty list.
4. Self-transitions matter. If setting a payment to the state it is already in
   should be a no-op, include the state in its own list. If it should be an
   error, leave it out.
5. Where the code and the correct business rule disagree, describe the RULE, not
   the code. That disagreement is exactly the bug the generated test should find.
6. Name the FINAL states — the ones a payment cannot leave. Then name every way
   one can be left after all, and say why. That second list is where the real
   business knowledge lives.
7. If you cannot tell, put the pair in ` + "`unsure`" + ` rather than guessing either way.
   A guess here becomes a failing test that wastes a person's afternoon, or a
   missing test that lets a real bug through.

## The state machine, as proved from source

`)

	fmt.Fprintf(&b, "Type:  %s\n", m.Type)
	if m.Field != "" {
		fmt.Fprintf(&b, "Field: %s\n", m.Field)
	}
	b.WriteString("\nDeclared states (complete):\n\n")
	for _, s := range m.States {
		fmt.Fprintf(&b, "  %s\n", s)
	}

	b.WriteString("\nEvery place the status is assigned:\n\n")
	if len(m.Writes) == 0 {
		b.WriteString("  (none found)\n")
	}
	for _, w := range m.Writes {
		tx := "OUTSIDE any transaction"
		if w.InTx {
			tx = "inside a transaction"
		}
		fmt.Fprintf(&b, "  -> %-20s in %s\n", w.To, w.In.Symbol)
		fmt.Fprintf(&b, "     %s:%d, %s\n", w.In.File, w.Line, tx)
	}

	if len(m.NeverAssigned) > 0 {
		b.WriteString("\nDeclared but NEVER assigned anywhere in this module:\n\n")
		for _, s := range m.NeverAssigned {
			fmt.Fprintf(&b, "  %s\n", s)
		}
		b.WriteString(`
For each of these, decide which it is: dead code left over from a removed
feature, or a state something OUTSIDE this module puts payments into — a
migration, an operations runbook, another service. If it is the second, every
read path here has to handle a state no write path here produces, and nothing
currently tests that.
`)
	}

	b.WriteString("\n## The flow these states belong to\n\n")
	for _, p := range paths {
		b.WriteString(indent(p.Render(), "  "))
	}

	b.WriteString(`Note on ordering: the step numbers above are a breadth-first walk of the call
graph, not execution order. A compiler cannot know which branch runs. Use them
to see WHAT is reachable, not in what sequence.

## Output

Reply with one JSON object and nothing else.

` + "```" + `
{
  "initial_state": "StatusPending",
  "may_move_to": {
    "StatusPending":    ["StatusAuthorized", "StatusFailed"],
    "StatusAuthorized": ["StatusCaptured", "StatusFailed"],
    "StatusCaptured":   ["StatusRefunded"],
    "StatusFailed":     [],
    "StatusRefunded":   [],
    "StatusAbandoned":  []
  },
  "unsure": [
    { "from": "StatusFailed", "to": "StatusPending",
      "why": "retry may be intended to reuse the row rather than create a new payment" }
  ],
  "never_assigned_verdict": {
    "StatusRefunded":  "reachable_from_outside",
    "StatusAbandoned": "dead"
  },
  "final_states": ["StatusRefunded", "StatusFailed"],
  "final_state_exceptions": [
    { "from": "StatusCaptured", "to": "StatusRefunded",
      "why": "a chargeback can arrive up to 40 days after capture" }
  ],
  "notes": "anything a test author needs that the shape above cannot carry"
}
` + "```" + `

### On final states

A state is final when a payment in it will never legitimately change again. This
matters more than it sounds, because it produces one of the strongest tests
available: drive a payment into a final state, attempt every other transition,
and assert every one is refused.

That test is only correct if the final list is correct, which is why the
exceptions are asked for in the same breath. Real payment systems are full of
states that look final and are not:

- **captured** is finished, until a chargeback arrives weeks later
- **settled** is finished, until the transfer is recalled
- **refunded** is finished, until the refund itself is reversed
- **failed** is often NOT final at all — see below

A state you list as final with no exception becomes a hard assertion. If reality
can leave it, that test will fail the first time reality happens, and someone
will delete it instead of fixing the code. So put the exception in the list, with
the reason, and the generated test will allow that one path and forbid the rest.

Two transitions are worth extra thought before you answer, because published
payment state machines get both wrong more often than any others:

- **Is failure terminal?** Stripe's PaymentIntent returns to
  ` + "`requires_payment_method`" + ` after a failed payment so it can be retried.
  Implementations that treat failure as terminal look correct until a customer
  retries a declined card.
- **Is the terminal state really terminal?** A captured payment is done, but
  money can still leave through a refund, a chargeback, or a reversal. If this
  repository models any of those, the "terminal" state has outgoing edges.
`)
	return b.String()
}
