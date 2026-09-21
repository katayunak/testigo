package prompts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/agent/domain"
)

type Prompt struct {
	Title string

	Goals       []string
	Constraints []string
	Context     []Context
	Questions   []domain.Question

	Output string

	See string

	dropped []string

	shared bool
}

type Context struct {
	Priority int
	Label    string
	Content  string
}

const (
	Proof = 0

	Subject = 1

	Supporting = 2

	Illustrative = 3
)

const GoalRecheck = "Recheck what the analyzer already decided. Do not re-derive it. " +
	"Everything under `What is already known` came from the Go type checker, SSA " +
	"and the migrations, so your job is to confirm it or to contradict it with a " +
	"file:line — not to work it out again from the source."

var baseConstraints = []string{
	"**Do not contradict it.** The facts came from the type checker and SSA. If your reading disagrees, you have misread the source, and saying so is more useful than quietly overriding it.",
	"**Every claim carries a file:line.** If you cannot point at one, the answer is null.",
	"**`unknown` is a real answer.** Prefer it to a guess. Downstream treats unknown as the unsafe case, which is the correct default when money is involved.",
}

func New(title string) *Prompt {
	return &Prompt{Title: title, shared: true}
}

func SharedRules() string {
	var b strings.Builder
	b.WriteString("\n## What every question in this pack is for\n\n")
	b.WriteString(GoalRecheck)
	b.WriteString("\n\nEach question has a `What is already known` section. It is fact, from the\nanalyzer — not a reading, and not up for negotiation without a file:line that\ncontradicts it.")
	b.WriteString("\n\n### Rules that apply to every answer\n\n")
	for i, c := range baseConstraints {
		fmt.Fprintf(&b, "%d. %s\n", i+1, c)
	}
	return b.String()
}

func NewGenerate(title string) *Prompt {
	return &Prompt{Title: title}
}

func (p *Prompt) Goal(g string) *Prompt {
	p.Goals = append(p.Goals, g)
	return p
}

func (p *Prompt) Rule(r ...string) *Prompt {
	p.Constraints = append(p.Constraints, r...)
	return p
}

func (p *Prompt) Fact(priority int, label, content string) *Prompt {
	if strings.TrimSpace(content) == "" {
		return p
	}
	p.Context = append(p.Context, Context{Priority: priority, Label: label, Content: content})
	return p
}

func (p *Prompt) Ask(q ...domain.Question) *Prompt {
	p.Questions = append(p.Questions, q...)
	return p
}

func (p *Prompt) Answers(shape string) *Prompt {
	p.Output = shape
	return p
}

func (p *Prompt) Where(path string) *Prompt {
	p.See = path
	return p
}

func (p *Prompt) Chars() int { return len(p.Render()) }

func (p *Prompt) Trim(budget int) (dropped []string) {
	if budget <= 0 || p.Chars() <= budget {
		return nil
	}

	order := make([]int, len(p.Context))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := p.Context[order[a]], p.Context[order[b]]
		if x.Priority != y.Priority {
			return x.Priority > y.Priority
		}
		return len(x.Content) > len(y.Content)
	})

	cut := map[int]bool{}
	for _, i := range order {
		if p.Context[i].Priority <= Proof {
			continue
		}
		if p.renderWithout(cut) <= budget {
			break
		}
		cut[i] = true
		dropped = append(dropped, p.Context[i].Label)
	}
	if len(cut) == 0 {
		return nil
	}

	kept := make([]Context, 0, len(p.Context)-len(cut))
	for i, c := range p.Context {
		if !cut[i] {
			kept = append(kept, c)
		}
	}
	p.Context = kept
	p.dropped = dropped
	return dropped
}

func (p *Prompt) renderWithout(cut map[int]bool) int {
	n := 0
	for i, c := range p.Context {
		if !cut[i] {
			n += len(c.Label) + len(c.Content) + 8
		}
	}
	return n + p.fixedChars()
}

func (p *Prompt) fixedChars() int {
	n := len(p.Title) + len(p.Output) + 200
	for _, g := range p.Goals {
		n += len(g) + 4
	}
	for _, c := range p.Constraints {
		n += len(c) + 6
	}
	for _, q := range p.Questions {
		n += len(q.Generate())
	}
	return n
}

func (p *Prompt) Render() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n", p.Title)

	if len(p.Goals) > 0 || p.shared {
		b.WriteString("\n## Goal\n\n")
		for _, g := range p.Goals {
			fmt.Fprintf(&b, "- %s\n", g)
		}
		if p.shared {

			b.WriteString("- Recheck, do not re-derive. Shared goal and rules: PREAMBLE.md.\n")
		}
	}

	if len(p.Context) > 0 {
		b.WriteString("\n## What is already known\n")
		byPriority := append([]Context(nil), p.Context...)
		sort.SliceStable(byPriority, func(i, j int) bool {
			return byPriority[i].Priority < byPriority[j].Priority
		})
		for _, c := range byPriority {
			if c.Label != "" {
				fmt.Fprintf(&b, "\n### %s\n\n", c.Label)
			} else {
				b.WriteString("\n")
			}
			b.WriteString(strings.TrimRight(c.Content, "\n"))
			b.WriteString("\n")
		}
	}

	if len(p.dropped) > 0 {
		fmt.Fprintf(&b, `
### Left out to keep this short

%s

That is the only thing missing. It is in %s — read that if you need it, rather
than searching the repository, which costs far more than the section did.
`, "  "+strings.Join(p.dropped, "\n  "), orFlowJSON(p.See))
	}

	if len(p.Constraints) > 0 {
		b.WriteString("\n## Rules\n\n")
		for i, c := range p.Constraints {
			fmt.Fprintf(&b, "%d. %s\n", i+1, c)
		}
	}

	if len(p.Questions) > 0 {
		fmt.Fprintf(&b, `
## The question(s) — %d

Each one ends with the shape of its answer. `+"`-> two sentences, or `"+"`unknown`"+`" means
exactly that: two sentences, and `+"`unknown`"+` is a real answer that costs nothing and
is far better than a guess somebody turns into a test. `+"`-> true/false + one line`"+`
means a boolean plus the one line that makes it checkable.

`, len(p.Questions))
		for _, q := range p.Questions {
			b.WriteString(q.Generate())
			b.WriteString("\n")
		}
	}

	if p.Output != "" {
		b.WriteString("\n## Output\n\n")
		b.WriteString(strings.TrimRight(p.Output, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

func orFlowJSON(s string) string {
	if s == "" {
		return "testigo/flow.json"
	}
	return s
}
