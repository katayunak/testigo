package prompts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func Needed(f *flowEntity.Flow, c domain.Classification, needs []domain.Need) *Prompt {
	p := New("testigo — the questions this repository actually needs answered").
		Goal("Every question below was picked because a test testigo intends to generate cannot be written without it. Nothing here is asked out of curiosity.").
		Rule("Answer from THIS repository. The facts block came from the type checker and SSA and cannot be contradicted.",
			"A true/false question wants a bare verdict. Do not justify a `true`.",
			"`unknown` is a real answer and is treated as the unsafe case.")

	var b strings.Builder
	fmt.Fprintf(&b, "  spine    %-18s %s\n", c.Spine, c.Spine.Human())
	for _, m := range c.Motions {
		fmt.Fprintf(&b, "  motion   %-18s %s\n", m, m.Human())
	}
	for _, o := range c.Overlays {
		fmt.Fprintf(&b, "  overlay  %-18s %s\n", o, o.Human())
	}
	p.Fact(Proof, "What testigo decided this repository is", b.String())

	var why strings.Builder
	byReason := map[string][]string{}
	for _, n := range needs {
		for _, r := range n.Because {
			byReason[r] = append(byReason[r], n.Question.ID)
		}
	}
	var reasons []string
	for r := range byReason {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	for _, r := range reasons {
		fmt.Fprintf(&why, "  %-34s %d question(s)\n", r, len(byReason[r]))
	}
	p.Fact(Supporting, "Why each question is here", why.String())

	qs := make([]domain.Question, 0, len(needs))
	for _, n := range needs {
		qs = append(qs, n.Question)
	}
	p.Ask(qs...)

	return p.Answers(neededShape(qs)).Where("testigo/flow.json")
}

func neededShape(qs []domain.Question) string {
	var b strings.Builder
	b.WriteString("One JSON object, nothing else.\n\n```\n{\n")
	for i, q := range qs {
		comma := ","
		if i == len(qs)-1 {
			comma = ""
		}
		switch {
		case i == 0 && q.TrueOrFalse:
			fmt.Fprintf(&b, "  %q: { \"verdict\": true }%s\n", q.ID, comma)
		case i == 0:
			fmt.Fprintf(&b, "  %q: { \"info\": \"one sentence\" }%s\n", q.ID, comma)
		case i == 1, i == len(qs)-1:
			fmt.Fprintf(&b, "  %q: { ... }%s\n", q.ID, comma)
		case i == 2:
			fmt.Fprintf(&b, "  ...%d more, one per question ID above...\n", len(qs)-3)
		}
	}
	b.WriteString("}\n```\n\n")
	b.WriteString(`Match the shape to the ` + "`->`" + ` line under each question:

    -> true/false      { "verdict": true }
    -> one sentence    { "info": "..." }

A ` + "`true`" + ` verdict is the whole answer. Add ` + "`info`" + ` only where the question asks
for a sentence, or where a verdict is ` + "`false`" + ` on something testigo already found.

Every question ID above must appear exactly once. If you genuinely cannot tell,
omit ` + "`verdict`" + ` and say in ` + "`info`" + ` what would settle it.`)
	return b.String()
}
