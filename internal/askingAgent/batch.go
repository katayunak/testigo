package askingAgent

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
)

// A Batch is every question of one kind, asked in one file and answered in one
// file.
//
// This is the change that touches the bill. A real recharge service cost $4.95
// for round one, and the pack was only 49k tokens of that — the other 9.7
// MILLION were cache reads, because sixty-six questions answered one at a time
// is sixty-six turns, and every turn re-reads the whole conversation so far:
//
//	cost ~= turns x accumulated context
//
// Compacting prompts attacks the wrong term. Sixty-six questions become five
// files, so five turns, and the accumulated context stops growing sixty-six
// times.
//
// Nothing about validation changes. Each answer is split back out and handed to
// exactly the same validator it had before, so a batched reply is checked as
// strictly as an individual one — and a single bad entry is reported against
// its own question rather than failing the batch.
type Batch struct {
	Kind askEntity.Kind
	Asks []askEntity.Ask
}

// Batches groups the asks, preserving the order they were planned in.
//
// Order matters: the money model and the main entity come first because
// everything else is read in their light, and an agent working top to bottom
// should meet them first.
func Batches(asks []askEntity.Ask) []Batch {
	var order []askEntity.Kind
	byKind := map[askEntity.Kind][]askEntity.Ask{}
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

// Single reports whether this batch is really just one question. A single
// question keeps its own name and its own flat answer shape, because wrapping
// one answer in a map of one is noise.
func (b Batch) Single() bool { return len(b.Asks) == 1 }

// ID names the file this batch is written to.
func (b Batch) ID() string {
	if b.Single() {
		return b.Asks[0].ID()
	}
	return string(b.Kind)
}

func (b Batch) AnswerFile() string { return b.ID() + ".json" }

// Prompt renders the whole batch as one file.
//
// The per-question text is unchanged — each one already carries only its own
// facts, because the shared rules and the flow map moved into PREAMBLE.md. What
// this adds is a header saying how to answer all of them at once, and one
// output shape instead of N.
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

// stripTitle removes a question's own top-level heading.
//
// Inside a batch each question is a section, and a file full of competing `#`
// headings reads as a stack of separate documents rather than one list.
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
