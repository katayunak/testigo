package prompts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
)

type Tokens struct {
	AskTokens    int
	AnswerTokens int
	Prompts      int
	Saved        int
}

func Report(f *flowEntity.Flow, k *domain.AgentResponse, cases []testPlan.TestCase, tok Tokens) *Prompt {
	p := New("testigo — write the report").
		Standalone().
		Goal("Write ONE markdown file. Everything you need is below; do not open the repository to re-derive it.").
		Rule("Every claim names a file and a line. If you cannot cite one, do not make the claim.",
			"A fix must be safe for a system that moves money. Never propose something that could charge twice, drop a payment silently, or lose the audit trail. Prefer a constraint the database enforces over a check in Go, and say what the fix costs.",
			"Do not invent findings. If a section has no material, write one line saying so.",
			"No preamble and no conclusion. Facts, consequences, fixes.")

	p.Fact(Proof, "The system", systemBlock(f, k))
	p.Fact(Proof, "What the scanner proved", provedBlock(f))
	if s := findingsBlock(f); s != "" {
		p.Fact(Proof, "Findings, with cause", s)
	}
	p.Fact(Subject, "The test plan", planBlock(cases))
	if s := runsBlock(k); s != "" {
		p.Fact(Proof, "Test results", s)
	}
	if s := answersBlock(k); s != "" {
		p.Fact(Supporting, "What the agent was told about this repository", s)
	}
	p.Fact(Supporting, "Tokens", tokensBlock(tok))

	return p.Answers(reportShape).Where("testigo/flow.json")
}

const reportShape = "Write the file as markdown, in this order. Nothing before the first heading.\n\n" +
	"    # <module> — payment flow report\n" +
	"\n" +
	"    ## What this is\n" +
	"      The flow in three or four sentences: what it moves, for whom, through\n" +
	"      which provider. Name its payment kind and say what that kind implies.\n" +
	"\n" +
	"    ## What was tested\n" +
	"      A table: scenario | test kind | why it matters here.\n" +
	"      Then the scenarios that were NOT run and the one-line reason each.\n" +
	"\n" +
	"    ## Results\n" +
	"      A table: test | result | what it proves.\n" +
	"      A red test that was expected to fail is a FINDING, not a broken test.\n" +
	"      Say which is which.\n" +
	"\n" +
	"    ## Strengths\n" +
	"      What this code already gets right, each with file:line. Be specific;\n" +
	"      \"good error handling\" is not a strength, \"every provider call carries a\n" +
	"      context deadline (client/x.go:40)\" is.\n" +
	"\n" +
	"    ## Weaknesses\n" +
	"      The same, inverted. Rank by what it would cost in production.\n" +
	"\n" +
	"    ## Problems\n" +
	"      One subsection per problem, worst first:\n" +
	"        ### <short name>\n" +
	"        **Where** file.go:LINE\n" +
	"        **What happens** the failure in one or two sentences, concretely:\n" +
	"          which two requests, in which order, leaving what state.\n" +
	"        **Fix** the change to make, and why it is the right one for money.\n" +
	"          Name the migration, the constraint or the function. If the fix has a\n" +
	"          cost — a lock, a column, a slower path — say so.\n" +
	"\n" +
	"    ## What this cost\n" +
	"      The token figures, and one line on what is still unanswered.\n" +
	"\n" +
	"Return the markdown only. No JSON, no fence around the whole document."

func systemBlock(f *flowEntity.Flow, k *domain.AgentResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "module   %s\n", f.Module)
	for _, e := range f.Entries {
		label := e.Label
		if label == "" {
			label = "unlabelled"
		}
		fmt.Fprintf(&b, "entry    %s#%s  (%s)\n", e.Pkg, e.Symbol, label)
	}
	if k != nil && k.Classification != nil {
		c := k.Classification
		fmt.Fprintf(&b, "\nspine    %-18s %s\n", c.Spine, c.Spine.Human())
		for _, m := range c.Motions {
			fmt.Fprintf(&b, "motion   %-18s %s\n", m, m.Human())
		}
		for _, o := range c.Overlays {
			fmt.Fprintf(&b, "overlay  %-18s %s\n", o, o.Human())
		}
	}
	if k != nil && k.MoneyModel != nil {
		m := k.MoneyModel
		if m.Money.Type != "" {
			fmt.Fprintf(&b, "\nmoney    %s.%s (%s) at %s\n",
				m.Money.Type, m.Money.AmountField, m.Money.Representation, m.Money.Proof.At)
		}
		if m.Idempotency.KeyField != "" {
			fmt.Fprintf(&b, "key      %s, uniqueness %s, at %s\n",
				m.Idempotency.KeyField, m.Idempotency.Uniqueness, m.Idempotency.Proof.At)
		} else {
			b.WriteString("key      NONE NAMED — nothing deduplicates a repeated request\n")
		}
		if m.TransferFunc.Symbol != "" {
			fmt.Fprintf(&b, "transfer %s at %s\n", m.TransferFunc.Symbol, m.TransferFunc.At)
		}
	}
	if k != nil && k.MainEntity != nil && k.MainEntity.MainEntity.Struct != "" {
		fmt.Fprintf(&b, "entity   %s at %s\n",
			k.MainEntity.MainEntity.Struct, k.MainEntity.MainEntity.Proof.At)
	}
	return b.String()
}

func provedBlock(f *flowEntity.Flow) string {
	var b strings.Builder
	inj, con := 0, 0
	for _, s := range f.Seams {
		if s.Injectable {
			inj++
		} else {
			con++
		}
	}
	fmt.Fprintf(&b, "%d function(s) reachable from the entry point(s)\n", len(f.Nodes))
	fmt.Fprintf(&b, "%d seam(s): %d injectable, %d on concrete types and therefore untestable under failure\n",
		len(f.Seams), inj, con)
	for _, m := range f.States {
		fmt.Fprintf(&b, "state machine %s: %s\n", shortName(m.Type), strings.Join(m.States, ", "))
		if len(m.NeverAssigned) > 0 {
			fmt.Fprintf(&b, "  never assigned anywhere in the scanned code: %s\n",
				strings.Join(m.NeverAssigned, ", "))
		}
	}
	if f.GeneratedFiles > 0 {
		fmt.Fprintf(&b, "%d generated file(s) excluded; %d finding(s) inside them not reported\n",
			f.GeneratedFiles, f.GeneratedFindings)
	}
	return b.String()
}

func findingsBlock(f *flowEntity.Flow) string {
	if len(f.Findings) == 0 {
		return ""
	}
	byID := map[string][]flowEntity.Finding{}
	var order []string
	for _, x := range f.Findings {
		if _, seen := byID[x.ID]; !seen {
			order = append(order, x.ID)
		}
		byID[x.ID] = append(byID[x.ID], x)
	}
	sort.Slice(order, func(i, j int) bool { return len(byID[order[i]]) > len(byID[order[j]]) })

	var b strings.Builder
	for _, id := range order {
		hits := byID[id]
		fmt.Fprintf(&b, "%s  [%s]  x%d\n  %s\n", id, hits[0].Severity, len(hits), hits[0].Title)
		shown := hits
		if len(shown) > 6 {
			shown = shown[:6]
		}
		for _, h := range shown {
			if h.Ref.File == "" && h.Ref.Symbol == "" {
				fmt.Fprintf(&b, "  %s\n", firstLine(h.Detail))
				continue
			}
			loc := h.Ref.File
			if loc == "" {
				loc = h.Ref.Pkg
			}
			fmt.Fprintf(&b, "  %s:%d  %s\n", loc, h.Line, h.Ref.Symbol)
		}
		if len(hits) > len(shown) {
			fmt.Fprintf(&b, "  ...%d more of the same, in testigo/flow.json\n", len(hits)-len(shown))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func planBlock(cases []testPlan.TestCase) string {
	var b strings.Builder
	b.WriteString("RUNNABLE\n")
	n := 0
	for _, c := range cases {
		if !c.Runnable() {
			continue
		}
		n++
		fmt.Fprintf(&b, "  %-8s %-34s %-18s %s\n",
			c.Scenario.Severity, c.Scenario.ID, c.Technique, c.Scenario.LookingFor)
	}
	if n == 0 {
		b.WriteString("  none\n")
	}
	b.WriteString("\nNOT RUNNABLE\n")
	for _, c := range cases {
		if c.Runnable() {
			continue
		}
		fmt.Fprintf(&b, "  %-34s %s\n", c.Scenario.ID, c.Blocked)
	}
	return b.String()
}

func runsBlock(k *domain.AgentResponse) string {
	if k == nil || len(k.TestRuns) == 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range k.TestRuns {
		result := r.Status
		switch {
		case r.Green():
			result = "PASS"
		case r.Red():
			result = "FAIL"
		}
		fmt.Fprintf(&b, "%-34s %-10s %s\n", r.CaseID, result, r.File)
		if r.ExpectedToFail != "" {
			fmt.Fprintf(&b, "  expected to fail: %s\n", r.ExpectedToFail)
		}
		if r.Reason != "" {
			fmt.Fprintf(&b, "  %s\n", r.Reason)
		}
		if r.Output != "" {
			fmt.Fprintf(&b, "  output:\n%s\n", indent(r.Output, "    "))
		}
	}
	return b.String()
}

func answersBlock(k *domain.AgentResponse) string {
	if k == nil || len(k.PaymentKind) == 0 {
		return ""
	}
	var ids []string
	for id := range k.PaymentKind {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var b strings.Builder
	for _, id := range ids {
		a := k.PaymentKind[id]
		if a == nil {
			continue
		}
		switch {
		case a.Verdict != nil && *a.Verdict:
			fmt.Fprintf(&b, "  T  %s\n", id)
		case a.Verdict != nil:
			fmt.Fprintf(&b, "  F  %s", id)
			if a.Info != "" {
				fmt.Fprintf(&b, " — %s", a.Info)
			}
			b.WriteString("\n")
		case a.Info != "":
			fmt.Fprintf(&b, "  .  %s — %s\n", id, a.Info)
		}
	}
	b.WriteString("\nA false verdict is where this repository departs from the safe default.\n")
	return b.String()
}

func tokensBlock(t Tokens) string {
	var b strings.Builder
	fmt.Fprintf(&b, "asked    ~%s tokens across %d prompt(s)\n", kilo(t.AskTokens), t.Prompts)
	fmt.Fprintf(&b, "answered ~%s tokens read back\n", kilo(t.AnswerTokens))
	if t.Saved > 0 {
		fmt.Fprintf(&b, "saved    ~%s by hoisting the shared rules into one preamble\n", kilo(t.Saved))
	}
	b.WriteString("\nThese are sizes on disk, not a bill. testigo makes no API call and cannot\nsee what the model charged. Say so in the report rather than implying a price.\n")
	return b.String()
}

func kilo(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

func shortName(q string) string {
	if i := strings.LastIndex(q, "/"); i >= 0 {
		return q[i+1:]
	}
	return q
}
