package agent

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/agent/domain"
)

type Batch struct {
	Kind domain.Kind
	Asks []domain.Ask
}

func Batches(asks []domain.Ask) []Batch {
	var order []domain.Kind
	byKind := map[domain.Kind][]domain.Ask{}
	for _, a := range asks {
		if _, seen := byKind[a.Kind]; !seen {
			order = append(order, a.Kind)
		}
		byKind[a.Kind] = append(byKind[a.Kind], a)
	}

	out := make([]Batch, 0, len(order))
	for _, k := range order {
		out = append(out, Batch{Kind: k, Asks: byKind[k]})
	}
	return out
}

func (b Batch) Single() bool { return len(b.Asks) == 1 }

func (b Batch) ID() string {
	if b.Single() {
		return b.Asks[0].ID()
	}
	return string(b.Kind)
}

func (b Batch) AnswerFile() string { return b.ID() + ".json" }

func (b Batch) Prompt() string {
	if b.Single() {
		return b.Asks[0].Prompt
	}

	var s strings.Builder
	fmt.Fprintf(&s, `# testigo — %d %s questions, answered together

Answer ALL %d below in ONE reply. They are the same kind of question about
different subjects, so the rules and the JSON shape are stated once, in
PREAMBLE.md under "%s", and apply to every one.

**Answer them in a single pass.** Do not ask them one at a time in separate
turns: each turn re-reads everything said so far, and on a pack this size that
re-reading costs more than the questions do.

If you cannot answer one, put it in the reply anyway with whatever you do know
and say so in its `+"`notes`"+`. A missing entry is indistinguishable from a
question you never reached.

---

`, len(b.Asks), b.Kind, len(b.Asks), b.Kind)

	for i, a := range b.Asks {
		fmt.Fprintf(&s, "\n## %d of %d — answer key `%s`\n\n", i+1, len(b.Asks), a.ID())
		fmt.Fprintf(&s, "%s\n", strings.TrimSpace(stripTitle(a.Prompt)))
		s.WriteString("\n---\n")
	}

	fmt.Fprintf(&s, `
# Output for all %d

One JSON object. Every key is an answer key from above; every value is the
shape PREAMBLE.md gives for a %s answer.

`+"```"+`
{
`, len(b.Asks), b.Kind)
	for i, a := range b.Asks {
		comma := ","
		if i == len(b.Asks)-1 {
			comma = ""
		}
		if i < 2 || i == len(b.Asks)-1 {
			fmt.Fprintf(&s, "  %q: { ... }%s\n", a.ID(), comma)
		} else if i == 2 {
			fmt.Fprintf(&s, "  ...%d more...\n", len(b.Asks)-3)
		}
	}
	s.WriteString("}\n```\n")
	return s.String()
}

func stripTitle(prompt string) string {
	lines := strings.Split(prompt, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "# ") {
			return strings.Join(lines[i+1:], "\n")
		}
		if i > 3 {
			break
		}
	}
	return prompt
}
