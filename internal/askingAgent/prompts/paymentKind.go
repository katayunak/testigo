package prompts

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// PaymentKind asks the questions that only apply to this kind of payment system.
//
// The classification itself is NOT asked. Which kind this is was worked out in
// Go from the migrations, the struct fields and the function names, and it goes
// in as CONTEXT the agent rechecks — not as a question it is paid to answer.
//
// What that buys is the whole point. Seventy-nine questions exist across sixteen
// kinds. A repository is asked the fifteen or so that can apply to it, and never
// sees a subscription proration question or a marketplace liability question,
// because those cannot be true of it. A question that cannot apply is not
// cheaper when asked briefly; it is worth nothing at any price, and a plausible
// answer to it becomes a test.
func PaymentKind(f *flowEntity.Flow, c askEntity.Classification) *Prompt {
	qs := c.Questions()
	if len(qs) == 0 {
		return nil
	}

	p := New(fmt.Sprintf("testigo — the %d questions this KIND of payment system raises", len(qs))).
		Goal("Answer what static analysis cannot reach: what the business INTENDS, what the provider does on its side of the wire, and what is supposed to happen when reality disagrees with the code.").
		Ask(qs...)

	// Proof priority: everything below is scoped by the classification. An agent
	// that cannot see which kind this is has no way to tell whether a question
	// applies to it.
	var what strings.Builder
	fmt.Fprintf(&what, "  spine    %-18s %s\n", c.Spine, c.Spine.Human())
	for _, m := range c.Motions {
		fmt.Fprintf(&what, "  motion   %-18s %s\n", m, m.Human())
	}
	for _, o := range c.Overlays {
		fmt.Fprintf(&what, "  overlay  %-18s %s\n", o, o.Human())
	}
	p.Fact(Proof, "What testigo decided this repository is", what.String())

	// Subject rather than Proof: the classification survives without its
	// evidence, and an agent that wants to argue can go and get it.
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

func paymentKindShape(qs []askEntity.Question) string {
	var b strings.Builder
	b.WriteString("One JSON object, nothing else.\n\n```\n{\n  \"answers\": {\n")
	for i, q := range qs {
		comma := ","
		if i == len(qs)-1 {
			comma = ""
		}
		switch {
		case i == 0 && q.TrueOrFalse:
			fmt.Fprintf(&b, "    %q: { \"verdict\": false, \"answer\": \"one line of why\", \"proof\": \"file.go:41\" }%s\n", q.ID, comma)
		case i == 0:
			fmt.Fprintf(&b, "    %q: { \"answer\": \"two sentences\", \"proof\": \"file.go:41\" }%s\n", q.ID, comma)
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

Every question ID above must appear exactly once. If you do not know one, the
answer is the word ` + "`unknown`" + ` and one line saying what would settle it — that
is a real answer, it costs almost nothing, and it is far more useful than a
guess that becomes a test somebody trusts.`)
	return b.String()
}

// PaymentKindPreamble is the shared half.
const PaymentKindPreamble = `
## paymentKind — questions specific to this kind of payment system

1. The classification in the question is testigo's, made from the schema and the
   Go type names. Disagree with it if the code says otherwise, and say so with a
   file:line.
2. Answer every question ID. ` + "`unknown`" + ` is a real answer.
3. A true/false question needs a ` + "`verdict`" + ` boolean AND one line of why. The
   line is what makes the boolean checkable.
4. Answer about the BUSINESS RULE, not only about the implementation. Where the
   two differ, that difference is the most valuable thing you can report.
5. Two sentences is the budget for a free-text answer.
`
