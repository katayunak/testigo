package prompts

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func Transitions(f *flowEntity.Flow, m flowEntity.StateMachine, paths []Path) *Prompt {

	p := New("testigo — which transitions must be impossible?")

	var b strings.Builder
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

	p.Fact(Proof, "The state machine, as proved from source", b.String())

	var flow strings.Builder
	for _, path := range paths {
		flow.WriteString(indent(path.RenderHeader(), "  "))
	}
	p.Fact(Subject, "The flow these states belong to", flow.String())

	return p.Answers("The JSON shape and the note on final states are in PREAMBLE.md, under\n\"transitions\".").Where("testigo/asks/PREAMBLE.md")
}
