package prompts

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// Binding asks only what phase 1 could not decide.
//
// This prompt used to ask five open questions, including two the compiler had
// already answered for its own findings. Paying an agent for something you have
// is the worst kind of spend: it costs tokens AND risks a worse answer than the
// one you discarded.
//
// So the shape changed. Everything scanningFlow ranked with clear evidence is
// stated here as FACT. Only genuinely close calls become questions, and those
// arrive as multiple choice over a scored list rather than as "find the money
// type", which is both cheaper and a much better question.
//
// When nothing is left over, plan.go does not emit this ask at all.
func Binding(f *flowEntity.Flow, paths []Path, unresolved []string) string {
	var b strings.Builder

	b.WriteString(`# testigo — confirm the money model

A static analyser has already ranked the candidates below, with its reasons. Your
job is to CHECK its work on the parts it could not decide, not to redo it.

## Rules

1. Where a fact is stated below, it came from the type checker or from a
   migration file. Do not contradict it. If your reading disagrees, you have
   misread the code.
2. Answer only the OPEN QUESTIONS at the end. Everything else is settled.
3. Every claim you make needs a file:line. No evidence means the answer is null.
4. A null answer is useful. It tells the next stage not to generate a test that
   depends on something that does not exist.

`)

	if len(f.Docs) > 0 {
		b.WriteString("## Read these first\n\n")
		b.WriteString("This repository documents some of its own rules. That is better evidence\n")
		b.WriteString("than anything you can infer from the code, and it is already on disk:\n\n")
		for _, d := range f.Docs {
			fmt.Fprintf(&b, "  %s", d.Path)
			if d.Title != "" {
				fmt.Fprintf(&b, "  — %s", d.Title)
			}
			b.WriteString("\n")
			if len(d.Topics) > 0 {
				fmt.Fprintf(&b, "     covers: %s\n", strings.Join(d.Topics, ", "))
			}
		}
		b.WriteString("\nOpen them yourself. They are not pasted here because a README can be\n")
		b.WriteString("forty kilobytes, and you are already sitting in this repository.\n\n")
	}

	b.WriteString("## What the analyser proved\n\n")
	fmt.Fprintf(&b, "Module: %s\n\n", f.Module)

	if len(f.MoneyTypes) > 0 {
		if best, ok := f.MoneyTypes.Decided(); ok {
			fmt.Fprintf(&b, "MONEY (settled — do not re-answer)\n  %s.%s  %s\n  %s:%d\n",
				best.Owner, best.Name, best.Type, best.File, best.Line)
			for _, e := range best.Evidence {
				fmt.Fprintf(&b, "    %s\n", e)
			}
			b.WriteString("\n")
		} else {
			b.WriteString("MONEY (open — see question 1)\n\n")
			b.WriteString(f.MoneyTypes.Render())
		}
	}

	if len(f.IdempotencyKeys) > 0 {
		if best, ok := f.IdempotencyKeys.Decided(); ok {
			fmt.Fprintf(&b, "IDEMPOTENCY KEY (settled — do not re-answer)\n  %s.%s\n  %s:%d\n",
				best.Owner, best.Name, best.File, best.Line)
			for _, e := range best.Evidence {
				fmt.Fprintf(&b, "    %s\n", e)
			}
			for _, a := range best.Against {
				fmt.Fprintf(&b, "    WARNING: %s\n", a)
			}
			b.WriteString("\n")
		} else {
			b.WriteString("IDEMPOTENCY KEY (open — see question 2)\n\n")
			b.WriteString(f.IdempotencyKeys.Render())
			b.WriteString("Note what already disqualifies a candidate: a value generated in this\n")
			b.WriteString("process cannot deduplicate a retry, because the retry generates a new one.\n\n")
		}
	}

	if len(f.Infra.Constraints) > 0 {
		b.WriteString("UNIQUENESS, from the migrations (settled)\n")
		for _, c := range f.Infra.Constraints {
			fmt.Fprintf(&b, "  %-18s %s(%s)   %s:%d\n", c.Kind, c.Table, strings.Join(c.Columns, ", "), c.File, c.Line)
		}
		b.WriteString("\n")
	}

	if len(f.Machines) > 0 {
		b.WriteString("STATUS TYPES, complete sets (settled)\n")
		for _, m := range f.Machines {
			fmt.Fprintf(&b, "  %s", m.Type)
			if m.Field != "" {
				fmt.Fprintf(&b, "   (field %s)", m.Field)
			}
			fmt.Fprintf(&b, "   %d states\n", len(m.States))
		}
		b.WriteString("\n")
	}

	b.WriteString("Entry points and what they reach:\n\n")
	for _, p := range paths {
		b.WriteString(indent(p.Render(), "  "))
	}

	b.WriteString("## OPEN QUESTIONS\n\n")
	n := 0
	ask := func(q string) { n++; fmt.Fprintf(&b, "%d. %s\n\n", n, q) }

	for _, u := range unresolved {
		switch u {
		case "money type":
			ask("**Which candidate is the money type?** Pick from the scored list above by\n   name, or answer null if none of them is. Do not propose one that is not listed\n   unless you can point at a file:line the analyser missed.")
		case "idempotency key":
			ask("**Which candidate is the idempotency key?** Pick from the scored list above.\n   The test is behavioural, not nominal: which value does a CLIENT send again when\n   it retries? If none is client-supplied, answer null — that is a real finding.")
		}
	}
	ask("**Which function actually moves money?** Writes a ledger entry, changes a\n   balance, or — in a system where money moves by MESSAGE — sends the signal that\n   commits it. If `testigo.rules.json` defines this, follow it.")
	ask("**Which function reads a balance or ledger total?** A conservation test needs\n   something to read. If there is none, say so; several test scenarios will be\n   skipped rather than faked.")

	b.WriteString(`## Output

One JSON object, nothing else. Include only the keys for questions you were
asked; anything marked settled above is already recorded.

` + "```" + `
{
  "money":        { "type": "pkg.Money" | null, "amount_field": "Cents" | null, "evidence": "..." },
  "idempotency":  { "key_field": "IdempotencyKey" | null, "evidence": "..." },
  "transfer_func": { "symbol": "pkg#(*Ledger).Post" | null, "evidence": "..." },
  "balance_func":  { "symbol": "pkg#(*Ledger).Balance" | null, "evidence": "..." },
  "entity_id_field": "ID" | null,
  "notes": "anything the questions did not cover that a test author needs"
}
` + "```" + `
`)
	return b.String()
}

func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n") + "\n\n"
}
