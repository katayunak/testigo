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

## Rules

1. Business terms, not mechanics. "Reserves the funds and records the hold" is
   useful. "Calls ExecContext with an INSERT statement" is not — the analyser
   already knows that and it is printed below.
2. Two sentences at most for ` + "`purpose`" + `. If it needs more, the function is doing
   more than one thing, and saying THAT is the useful note.
3. ` + "`effects`" + ` lists only what survives the function returning: rows written,
   messages published, money moved, provider state changed. Not local variables,
   not logging.
4. ` + "`assumptions`" + ` lists what this function needs its CALLER to have already
   guaranteed — validated input, an open transaction, an acquired lock, a
   checked balance. This is the field that finds bugs, because an assumption
   nobody enforces is a bug waiting for an unusual call order.
5. Describe what the code DOES, not what it should do. Bugs are reported
   elsewhere. A note that quietly describes intended behaviour hides the defect.

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

	b.WriteString("\n## The flow it belongs to\n\n")
	b.WriteString(indent(path.Render(), "  "))

	b.WriteString(`## Output

Reply with one JSON object and nothing else.

` + "```" + `
{
  "step": "reserve funds",
  "purpose": "One or two sentences.",
  "effects": ["writes a pending payment row", "debits the payer ledger account"],
  "assumptions": ["caller has validated the amount is positive",
                  "caller holds an open transaction"],
  "confidence": "high" | "medium" | "low"
}
` + "```" + `

Use ` + "`confidence: low`" + ` freely. A low-confidence note is still useful — it tells a
person which parts of the map to check first — whereas a confident wrong note is
worse than none at all.
`)
	return b.String()
}
