package prompts

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func PaymentKind(f *flowEntity.Flow, c domain.Classification) *Prompt {
	qs := c.Questions()
	if len(qs) == 0 {
		return nil
	}

	p := New(fmt.Sprintf("testigo — the %d questions this KIND of payment system raises", len(qs))).
		Goal("Answer what static analysis cannot reach: what the business INTENDS, what the provider does on its side of the wire, and what is supposed to happen when reality disagrees with the code.").
		Ask(qs...)

	var what strings.Builder
	fmt.Fprintf(&what, "  spine    %-18s %s\n", c.Spine, c.Spine.Human())
	for _, m := range c.Motions {
		fmt.Fprintf(&what, "  motion   %-18s %s\n", m, m.Human())
	}
	for _, o := range c.Overlays {
		fmt.Fprintf(&what, "  overlay  %-18s %s\n", o, o.Human())
	}
	p.Fact(Proof, "What testigo decided this repository is", what.String())

	var why strings.Builder
	for _, t := range c.All() {
		for _, w := range c.Why[t] {
			fmt.Fprintf(&why, "  %-16s %s\n", t, w)
		}
	}
	p.Fact(Subject, "Why, in evidence", why.String())

	if len(c.Unsure) > 0 {
		names := make([]string, 0, len(c.Unsure))
		for _, u := range c.Unsure {
			names = append(names, string(u))
		}
		p.Fact(Proof, "NOT SETTLED", fmt.Sprintf(
			`The evidence for %s was close, so the classification above may be wrong.
If it is, say so in `+"`notes`"+` with a file:line and answer what you can. A wrong
classification means you were asked the wrong questions, and that is worth one
sentence to correct.`, strings.Join(names, " and ")))
	}

	if missing := c.MissingOverlays(); len(missing) > 0 {
		var m strings.Builder
		m.WriteString(`testigo found no code for the following, and a system of this kind normally
has it. Absence is a finding, not a reason to skip the question — answer whether
it genuinely does not exist, or exists somewhere this scan did not reach.

`)
		for _, o := range missing {
			fmt.Fprintf(&m, "  %-18s %s\n", o, o.Human())
		}
		p.Fact(Proof, "What is NOT here, which is itself a question", m.String())
	}

	return p.Answers(paymentKindShape(qs)).Where("testigo/flow.json")
}

func paymentKindShape(qs []domain.Question) string {
	var b strings.Builder
	b.WriteString("One JSON object, nothing else.\n\n```\n{\n  \"answers\": {\n")
	for i, q := range qs {
		comma := ","
		if i == len(qs)-1 {
			comma = ""
		}
		switch {
		case i == 0:
			fmt.Fprintf(&b, "    %q: %s%s\n", q.ID, answerExample(q), comma)
		case i == 1, i == len(qs)-1:
			fmt.Fprintf(&b, "    %q: { ... }%s\n", q.ID, comma)
		case i == 2:
			fmt.Fprintf(&b, "    ...%d more, one per question ID above...\n", len(qs)-3)
		}
	}
	b.WriteString(`  },
  "notes": "anything the questions did not cover, including a wrong classification"
}
` + "```" + `

The three answer shapes, matching the ` + "`->`" + ` line under each question:

    -> true/false                        { "verdict": true }
    -> true/false (one line only if false)  { "verdict": false, "info": "what is true instead" }
    -> one sentence                      { "info": "..." }

A ` + "`true`" + ` verdict is the whole answer. Do not explain it.

Add ` + "`proof`" + ` only when you are pointing at a specific line:

    { "verdict": false, "info": "...", "proof": { "symbol": "Order.Status", "at": "order.go:41" } }

Every question ID above must appear exactly once. If you genuinely cannot tell,
omit ` + "`verdict`" + ` and say in ` + "`info`" + ` what would settle it. That is recorded as
unanswered, which is far more useful than a guess that becomes a test somebody
trusts.`)
	return b.String()
}

func answerExample(q domain.Question) string {
	if q.TrueOrFalse {
		return `{ "verdict": true }`
	}
	return `{ "info": "one sentence" }`
}

const PaymentKindPreamble = `
## paymentKind — questions specific to this kind of payment system

1. The classification in the question is testigo's, made from the schema and the
   Go type names. Disagree with it if the code says otherwise, and say so with a
   file:line.
2. Answer every question ID.
3. A true/false question needs only the ` + "`verdict`" + ` boolean. A bare ` + "`true`" + ` is a
   complete answer — do not justify it.
4. Add ` + "`info`" + `, one line, when the verdict is ` + "`false`" + ` on a question that re-checks
   something testigo already found in the source. Overturning a fact is the one
   place an explanation is worth paying for.
5. Answer about the BUSINESS RULE, not only about the implementation. Where the
   two differ, that difference is the most valuable thing you can report.
6. One sentence is the budget for a free-text answer.
`
