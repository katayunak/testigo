package prompts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
)

// A Prompt is one question file, built from parts rather than written as prose.
//
// Every prompt in this package used to assemble itself with its own
// strings.Builder and its own idea of what a prompt looks like. That drifts. The
// money-model prompt lost two load-bearing rules during a compaction — "do not
// contradict the facts" and "a null answer is useful" — and nothing noticed
// until a test went looking for them. Prose that everybody edits and nobody owns
// is where things quietly go missing.
//
// So the shape is fixed and the sections mean something:
//
//	Goals        why this question is being asked at all. For round one that is
//	             almost always the same sentence: recheck what the analyzer
//	             decided. Saying it stops the agent re-deriving facts we already
//	             own, which is the most expensive thing it can do.
//	Context      what testigo already proved. Fact, not opinion, and carrying a
//	             Priority so it can be cut in the right order under a budget.
//	Constraints  the rules an answer must obey.
//	Questions    the part that costs money, in a shape built to be cheap to
//	             answer.
//	Output       the exact JSON expected back.
//
// This is also the third lever on the bill, after hoisting the shared half into
// PREAMBLE.md and batching questions into one file per kind. Those two cut what
// is repeated and how many turns it takes. This one caps what any single
// question may carry, and — the part that matters — makes the cut VISIBLE.
type Prompt struct {
	Title string

	Goals       []string
	Constraints []string
	Context     []Context
	Questions   []askEntity.Question

	// Output is the JSON shape expected back, rendered verbatim.
	Output string

	// See is where the material lives that a trim removed, so an agent can go
	// and get exactly the piece it needs instead of re-reading the repository.
	See string

	// dropped is what Trim removed, remembered so Render can say so.
	dropped []string

	// shared marks a round-one prompt, whose goal and rules are in PREAMBLE.md.
	shared bool
}

// Context is one block of proved fact.
//
// Priority is what makes a budget safe. Cutting the least load-bearing block
// first is the difference between a prompt that is shorter and a prompt that is
// wrong, and the two are indistinguishable from the outside.
type Context struct {
	Priority int
	Label    string
	Content  string
}

// The four priorities. Lower survives longer.
const (
	// Proof is what the compiler and the migrations established. Cutting it does
	// not save money: an agent that cannot see a fact goes and re-derives it
	// from the source, which costs more than the fact did.
	Proof = 0

	// Subject is the specific thing being asked about — this state machine,
	// this call, this entity.
	Subject = 1

	// Supporting is helpful and survivable. Extra write sites, further
	// examples, the fourth illustration of a pattern already shown three times.
	Supporting = 2

	// Illustrative goes first. Nothing depends on it.
	Illustrative = 3
)

// The goal every round-one prompt shares.
//
// It is worth one line in every prompt because of what it prevents. An agent
// asked "which field is the idempotency key" starts by working out which field
// is the idempotency key — reading the repository, forming an opinion, and
// billing for all of it. An agent told "the analyzer says it is X, with this
// proof; check it" does one comparison. Same answer, a fraction of the cost, and
// a disagreement becomes visible instead of silently replacing our fact.
const GoalRecheck = "Recheck what the analyzer already decided. Do not re-derive it. " +
	"Everything under `What is already known` came from the Go type checker, SSA " +
	"and the migrations, so your job is to confirm it or to contradict it with a " +
	"file:line — not to work it out again from the source."

// The constraints every round-one prompt shares.
var baseConstraints = []string{
	"**Do not contradict it.** The facts came from the type checker and SSA. If your reading disagrees, you have misread the source, and saying so is more useful than quietly overriding it.",
	"**Every claim carries a file:line.** If you cannot point at one, the answer is null.",
	"**`unknown` is a real answer.** Prefer it to a guess. Downstream treats unknown as the unsafe case, which is the correct default when money is involved.",
}

// New starts a round-one prompt.
//
// The shared goal and the shared rules are NOT copied in. They are the same
// three hundred words in every question, and this pack has thirty-four of them:
// copying them cost eight thousand tokens a round, which is the exact mistake
// this package already made once with the flow map. They go into PREAMBLE.md,
// written once, by SharedRules below — and each prompt carries one line saying
// so.
//
// The structure still guarantees they reach the agent. It just does not pay for
// them thirty-four times.
func New(title string) *Prompt {
	return &Prompt{Title: title, shared: true}
}

// SharedRules is the goal and the rules every round-one question has in common,
// rendered once into PREAMBLE.md.
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

// NewGenerate starts a round-two prompt.
//
// Round two asks for CODE, not for answers, so it gets neither the recheck goal
// nor the round-one rules: there is nothing to recheck and "every claim carries
// a file:line" is meaningless advice to something writing a test file. It keeps
// the sections and the budget, which is what it is here for.
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

// Fact adds a block of proved context at the given priority.
func (p *Prompt) Fact(priority int, label, content string) *Prompt {
	if strings.TrimSpace(content) == "" {
		return p
	}
	p.Context = append(p.Context, Context{Priority: priority, Label: label, Content: content})
	return p
}

func (p *Prompt) Ask(q ...askEntity.Question) *Prompt {
	p.Questions = append(p.Questions, q...)
	return p
}

func (p *Prompt) Answers(shape string) *Prompt {
	p.Output = shape
	return p
}

// Where names the file that holds anything a trim removed.
func (p *Prompt) Where(path string) *Prompt {
	p.See = path
	return p
}

// Chars is the rendered size. Used for budgeting, and reported by `testigo ask`
// before anything is spent.
func (p *Prompt) Chars() int { return len(p.Render()) }

// Trim drops context until the prompt fits, least load-bearing first, and says
// what it dropped.
//
// The announcement is the whole point and it is not politeness. A prompt that
// silently omits something makes an agent go looking for it in the repository,
// and reading source is an order of magnitude more expensive than the block that
// was cut. Naming what was removed and where it lives turns a cut into a cheap
// lookup instead of an open-ended search.
//
// Proof-priority context is never cut. A budget small enough to require that is
// a budget that produces wrong answers, and a wrong answer at any price is worse
// than a right one at full price.
func (p *Prompt) Trim(budget int) (dropped []string) {
	if budget <= 0 || p.Chars() <= budget {
		return nil
	}

	// Highest priority number goes first; ties broken by size, because dropping
	// one large block beats dropping four small ones.
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

// dropped is what Trim removed, remembered so Render can say so in the prompt.

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

// Render writes the prompt out.
//
// The order is deliberate: goal, then fact, then rule, then question. An agent
// that meets the question first starts answering it before it has read the facts
// that make the answer cheap.
func (p *Prompt) Render() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n", p.Title)

	if len(p.Goals) > 0 || p.shared {
		b.WriteString("\n## Goal\n\n")
		for _, g := range p.Goals {
			fmt.Fprintf(&b, "- %s\n", g)
		}
		if p.shared {
			// One line instead of three hundred words. See New.
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
