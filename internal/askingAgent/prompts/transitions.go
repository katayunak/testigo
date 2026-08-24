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
		b.WriteString(indent(p.RenderHeader(), "  "))
	}

	b.WriteString("The JSON shape and the note on final states are in\nPREAMBLE.md, under \"transitions\".\n")
		return b.String()
}
