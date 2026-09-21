package prompts

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func MoneyModel(f *flowEntity.Flow, paths []Path, unresolved []string) *Prompt {
	p := New("testigo — the money model").
		Goal("Answer the OPEN QUESTIONS at the end. Everything else here is settled and only needs a true or false.").
		Rule("**A null answer is useful.** Where the repository has no such thing — no balance function, no local transfer — null is the correct answer and it stops round two generating a test for something that does not exist. A guess here produces a test asserting behaviour nobody wrote.")

	var facts strings.Builder
	fmt.Fprintf(&facts, "Module: %s\n\n", f.Module)

	if len(f.MoneyTypes) > 0 {
		if best, ok := f.MoneyTypes.Decided(); ok {
			fmt.Fprintf(&facts, "MONEY\n  %s.%s  %s\n  %s:%d\n",
				best.Owner, best.Name, best.Type, best.File, best.Line)
			for _, e := range best.Proof {
				fmt.Fprintf(&facts, "    %s\n", e)
			}
			facts.WriteString("True || False?\n\n")
		} else {
			facts.WriteString("MONEY is unknown +OPEN QUESTION\n\n")
			facts.WriteString(f.MoneyTypes.Render())
		}
	}

	if len(f.IdempotencyKeys) > 0 {
		if best, ok := f.IdempotencyKeys.Decided(); ok {
			fmt.Fprintf(&facts, "IDEMPOTENCY KEY\n  %s.%s\n  %s:%d\n",
				best.Owner, best.Name, best.File, best.Line)
			for _, e := range best.Proof {
				fmt.Fprintf(&facts, "    %s\n", e)
			}
			for _, a := range best.Against {
				fmt.Fprintf(&facts, "    WARNING: %s\n", a)
			}
			facts.WriteString("True || False?\n\n")
		} else {
			facts.WriteString("IDEMPOTENCY KEY is unknown +OPEN QUESTION\n\n")
			facts.WriteString(f.IdempotencyKeys.Render())
		}
	}

	if len(f.States) > 0 {
		facts.WriteString("STATUS TYPES\n")
		for _, m := range f.States {
			fmt.Fprintf(&facts, "  %s", m.Type)
			if m.Field != "" {
				fmt.Fprintf(&facts, "   (field %s)", m.Field)
			}
			fmt.Fprintf(&facts, "   %d states\n", len(m.States))
		}
		facts.WriteString("True || False?\n")
	}
	p.Fact(Proof, "Recheck facts — GO's SSA and AST ranked these, with their reasons", facts.String())

	var entries strings.Builder
	for _, path := range paths {
		entries.WriteString(indent(path.RenderHeader(), "  "))
	}
	p.Fact(Subject, "Entry points and what they reach", entries.String())

	if len(f.Docs) > 0 {
		var docs strings.Builder
		docs.WriteString("This repository documents itself. Read these before answering; a rule the\nteam wrote down beats one inferred from the code.\n\n")
		for _, d := range f.Docs {
			fmt.Fprintf(&docs, "  %s", d.Path)
			if d.Title != "" {
				fmt.Fprintf(&docs, "  — %s", d.Title)
			}
			docs.WriteString("\n")
			if len(d.Topics) > 0 {
				fmt.Fprintf(&docs, "     covers: %s\n", strings.Join(d.Topics, ", "))
			}
		}
		docs.WriteString("\nIf you use something from one, say which document it came from.\n")
		p.Fact(Supporting, "Read these first", docs.String())
	}

	for _, u := range unresolved {
		switch u {
		case "money type":
			p.Ask(*domain.Discover("money",
				"Which type carries money in this flow, and which field on it is the amount?",
				"The analyzer ranked candidates and none won clearly. Every later question is read in the light of this one, so a wrong answer here is wrong everywhere.").
				Knowing("the ranked candidates are listed above, with the reason for each"))
		case "idempotency key":
			p.Ask(*domain.Discover("idempotency",
				"Which field is the value a client repeats when it retries the same payment?",
				"A retry test needs a key to retry WITH. Without one, the duplicate-charge scenario cannot be generated at all — and that is the bug this tool most exists to find.").
				Knowing("a value that only exists AFTER the payment cannot be a key: it cannot guard a decision made before it exists"))
		}
	}
	p.Ask(
		*domain.Discover("transfer_func",
			"Which function actually moves money?",
			"A duplicate-effect test has to call something. If nothing here moves money locally — because the movement is an HTTP call to a provider — say null, and say that."),
		*domain.Discover("balance_func",
			"Which function reads a balance or a ledger total?",
			"A conservation test needs something to read. If there is none, null is correct and several scenarios are skipped rather than faked.").
			Knowing("the compiler can see that a function returns an int64; it cannot see that the int64 is a balance"),
	)

	return p.Answers(`One JSON object, nothing else. Include only the keys for questions you were
asked; anything marked settled above is already recorded.

` + "```" + `
{
  "money":        { "type": "pkg.Money" | null, "amount_field": "Cents" | null, "proof": { "symbol": "pkg.Money", "at": "money.go:14" } },
  "idempotency":  { "key_field": "IdempotencyKey" | null, "proof": { "symbol": "Order.IdempotencyKey", "at": "order.go:22" } },
  "transfer_func": { "symbol": "pkg#(*Ledger).Post" | null, "at": "ledger.go:41" },
  "balance_func":  { "symbol": "pkg#(*Ledger).Balance" | null, "at": "ledger.go:88" },
  "entity_id_field": "ID" | null,
  "notes": "anything the questions did not cover that a test author needs"
}
` + "```" + `

The key for each question is its ` + "`id`" + ` above.`).Where("testigo/flow.json")
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
