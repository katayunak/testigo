package prompts

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func Notes(f *flowEntity.Flow, node *flowEntity.Node, step Step, path Path) *Prompt {
	var b strings.Builder

	fmt.Fprintf(&b, "Function: %s\n", node.Ref.Symbol)
	fmt.Fprintf(&b, "Package:  %s\n", node.Ref.Pkg)
	fmt.Fprintf(&b, "Source:   %s:%d\n", node.Ref.File, node.Ref.Line)
	fmt.Fprintf(&b, "Position: %s\n\n", node.Position)

	if tags := factTags(node.Facts); len(tags) > 0 {
		b.WriteString("Proved:\n")
		for _, t := range tags {
			fmt.Fprintf(&b, "  - %s\n", t)
		}
		b.WriteString("\n")
	}
	for _, s := range step.Seams {
		fmt.Fprintf(&b, "Leaves the process: %s [%s] at line %d\n", s.Target, s.Kind, s.Line)
	}
	for _, w := range step.States {
		tx := "outside any transaction"
		if w.InTx {
			tx = "inside a transaction"
		}
		fmt.Fprintf(&b, "Sets status to %s at line %d, %s\n", w.To, w.Line, tx)
	}
	if len(node.Calls) > 0 {
		b.WriteString("\nCalls:\n")
		for _, c := range node.Calls {
			if callee, ok := f.Nodes[c]; ok {
				fmt.Fprintf(&b, "  %s\n", callee.Ref.Symbol)
			}
		}
	}

	return New("testigo — what does this step do?").
		Fact(Proof, "What the analyser proved about this function", b.String()).
		Fact(Subject, "Where this sits in the flow", fmt.Sprintf(
			"You are describing **step %d** of %q.\n"+
				"The full step list is in PREAMBLE.md — read it once, then come back here.\n"+
				"Steps %s are the ones immediately around it.\n",
			step.Order, path.Label, neighbours(step, path))).
		Where("testigo/asks/PREAMBLE.md")
}

func neighbours(step Step, path Path) string {
	lo, hi := step.Order-1, step.Order+1
	if lo < 1 {
		lo = 1
	}
	last := len(path.Steps)
	if hi > last {
		hi = last
	}
	return fmt.Sprintf("%d-%d", lo, hi)
}
