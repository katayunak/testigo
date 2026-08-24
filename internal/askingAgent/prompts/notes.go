package prompts

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// Notes asks what one function means in business terms.
//
// Kept deliberately small. The temptation is to ask for an essay per function,
// but notes exist to make LATER prompts readable, not to be read on their own.
// Four short fields per function is enough to turn a call graph of symbol names
// into something a person can follow, and short answers are cheaper to
// regenerate when the function changes — which, on a repository under active
// development, is constantly.
func Notes(f *flowEntity.Flow, node *flowEntity.Node, step Step, path Path) string {
	var b strings.Builder

	b.WriteString(`# testigo — what does this step do?

Describe one function in business terms, for a reader who knows payments but has
not read this repository.

Rules and the JSON shape are in PREAMBLE.md, under "notes".

## What the analyser proved about this function

`)
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

	// The flow map lives in PREAMBLE.md, printed once for the whole pack.
	//
	// It used to be pasted into every notes prompt. On a 73-function service
	// that map is 34 KB and it was 94% of every prompt — two prompts compared
	// byte for byte were 99% identical, and the pack came to a million tokens
	// to describe 73 functions. The map is the same map every time; only the
	// step number changes.
	//
	// Worse than the size, the shape: the map grows with the function count AND
	// there is one prompt per function, so pasting it made the pack quadratic.
	// Eight times the functions cost seventy-three times the tokens.
	fmt.Fprintf(&b, "\n## Where this sits in the flow\n\nYou are describing **step %d** of %q.\n"+
		"The full step list is in PREAMBLE.md — read it once, then come back here.\n"+
		"Steps %s are the ones immediately around it.\n",
		step.Order, path.Label, neighbours(step, path))

	
	return b.String()
}

// neighbours names the steps either side of this one, so the prompt can point
// into the shared map instead of carrying a copy of it.
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
