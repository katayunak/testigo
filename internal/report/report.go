package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/testigo/internal/agent/domain"
)

type Ledger struct {
	Round           int
	AskBytes        int
	PreambleByes    int
	Prompts         int
	AnsweredPrompts int
	AnswerBytes     int
	Answers         int
	Missing         int
}

func Report(f *fact.Flow, k *domain.AgentResponse, l Ledger, asksDir string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "testigo report — %s\n", f.Module)
	fmt.Fprintf(&b, "scanned    %s, from %d entry point(s)\n", f.GeneratedAt, len(f.Entries))
	for _, e := range f.Entries {
		label := e.Label
		if label == "" {
			label = "unlabelled"
		}
		fmt.Fprintf(&b, "           %s#%s  (%s)\n", e.Pkg, e.Symbol, label)
	}

	inj, con := 0, 0
	for _, s := range f.Seams {
		if s.Injectable {
			inj++
		} else {
			con++
		}
	}
	b.WriteString("\nPROVED — phase 1, no agent, no tokens\n")
	fmt.Fprintf(&b, "  %d function(s) reachable from the entry points\n", len(f.Nodes))
	fmt.Fprintf(&b, "  %d seam(s): %d injectable, %d on concrete types\n", len(f.Seams), inj, con)
	if con > 0 {

		fmt.Fprintf(&b, "           %d of them cannot be made to fail in a test\n", con)
	}
	for _, m := range f.States {
		fmt.Fprintf(&b, "  state machine %s: %d states, %d write site(s)\n",
			shortType(m.Type), len(m.States), len(m.Writes))
	}
	if f.GeneratedFiles > 0 {
		fmt.Fprintf(&b, "  %d generated file(s) excluded; %d finding(s) from them not reported\n",
			f.GeneratedFiles, f.GeneratedFindings)
	}

	if len(f.Findings) > 0 {
		bySeverity := map[fact.Severity]int{}
		byID := map[string]int{}
		title := map[string]string{}
		for _, x := range f.Findings {
			bySeverity[x.Severity]++
			byID[x.ID]++
			if _, seen := title[x.ID]; !seen {
				title[x.ID] = x.Title
			}
		}
		fmt.Fprintf(&b, "\nFINDINGS — %d critical, %d high, %d medium, %d info\n",
			bySeverity[fact.SevCritical], bySeverity[fact.SevHigh],
			bySeverity[fact.SevMedium], bySeverity[fact.SevInfo])

		ids := make([]string, 0, len(byID))
		for id := range byID {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return byID[ids[i]] > byID[ids[j]] })
		for _, id := range ids {
			fmt.Fprintf(&b, "  %-22s %3d   %s\n", id, byID[id], firstClause(title[id]))
		}
		fmt.Fprintf(&b, "\n  every one with its file and line: %s\n", "testigo/flow.json")
	}

	b.WriteString("\nSTILL UNKNOWN — phase 2 asks an agent\n")
	switch {
	case l.Prompts == 0:
		b.WriteString("  nothing asked yet. run 'testigo ask' to write the prompt pack.\n")
	case l.AnsweredPrompts == 0:
		fmt.Fprintf(&b, "  round %d   %d question(s) written, none answered yet\n", l.Round, l.Prompts)
		fmt.Fprintf(&b, "            point an agent at %s\n", filepath.Join(asksDir, "INSTRUCTIONS.md"))
	default:
		fmt.Fprintf(&b, "  round %d   %d of %d answered", l.Round, l.AnsweredPrompts, l.Prompts)
		if l.Missing > 0 {
			fmt.Fprintf(&b, ", %d still missing", l.Missing)
		}
		b.WriteString("\n")
	}
	if k != nil {
		if k.MoneyModel != nil {
			b.WriteString("  binding   answered\n")
		}
		if k.MainEntity != nil && k.MainEntity.MainEntity.Struct != "" {
			fmt.Fprintf(&b, "  entity    %s", k.MainEntity.MainEntity.Struct)
			if key := k.MainEntity.IdempotencyKey.Field; key != "" {
				fmt.Fprintf(&b, ", idempotency key %s\n", key)
			} else {

				b.WriteString(", NO IDEMPOTENCY KEY — see no_key_reason\n")
			}
		}
		if n := len(k.Transitions); n > 0 {
			fmt.Fprintf(&b, "  states    %d machine(s) described\n", n)
		}
		if n := len(k.ExternalEffects); n > 0 {
			fmt.Fprintf(&b, "  effects   %d external call(s) described\n", n)
		}
	}

	if k != nil && len(k.TestRuns) > 0 {
		b.WriteString(testRuns(k.TestRuns))
	}

	b.WriteString("\nTOKENS\n")
	if l.AskBytes > 0 {
		fmt.Fprintf(&b, "  written to ask     %7s est. input across %d prompt(s)\n",
			tokens(l.AskBytes), l.Prompts)
		if l.PreambleByes > 0 && l.Prompts > 1 {
			saved := l.PreambleByes * (l.Prompts - 1)
			fmt.Fprintf(&b, "  saved by hoisting  %7s that repeating the shared block would have cost\n",
				tokens(saved))
		}
	}
	if l.AnswerBytes > 0 {
		fmt.Fprintf(&b, "  read back          %7s across %d answer(s)\n", tokens(l.AnswerBytes), l.Answers)
	}

	b.WriteString(`
  These are SIZES ON DISK, not a bill.

  testigo writes markdown and reads JSON. It never makes an API call — that is
  why it works with any agent and needs no key, and it is also why it cannot
  see what your model actually charged. Reading a prompt costs the model more
  than the prompt, because it also reads the source files the prompt names.

  For what was really spent:
    Claude Code       /cost        (this session)
    Claude Console    console.anthropic.com/usage
`)

	return b.String()
}

func testRuns(runs []domain.TestRun) string {
	var b strings.Builder
	count := map[string]int{}
	for _, r := range runs {
		switch {
		case r.Status == "written" && r.Green():
			count["passed"]++
		case r.Status == "written":
			count["failed"]++
		default:
			count[r.Status]++
		}
	}
	written := count["passed"] + count["failed"]
	b.WriteString("\nTESTS — round 2, written by the agent, run with -race\n")
	fmt.Fprintf(&b, "  %d written: %d passed, %d failed\n", written, count["passed"], count["failed"])
	for _, status := range []string{"blocked", "no_build", "vet_fail", "refused"} {
		if count[status] > 0 {
			fmt.Fprintf(&b, "  %d %s\n", count[status], strings.ReplaceAll(status, "_", " "))
		}
	}
	for _, r := range runs {
		if r.Status != "written" || r.Green() {
			continue
		}
		note := ""
		if r.ExpectedToFail != "" {
			note = "  (expected)"
		}
		fmt.Fprintf(&b, "  failed    %-34s %s%s\n", r.CaseID, r.File, note)
	}
	return b.String()
}

func tokens(b int) string {
	t := b / 4
	if t >= 1000 {
		return fmt.Sprintf("%.1fk", float64(t)/1000)
	}
	return fmt.Sprintf("%d", t)
}

func firstClause(s string) string {
	for _, sep := range []string{" — ", ", so ", " with "} {
		if i := strings.Index(s, sep); i > 0 {
			return s[:i]
		}
	}
	if len(s) > 58 {
		return s[:58] + "…"
	}
	return s
}

func shortType(q string) string {
	if i := strings.LastIndex(q, "/"); i >= 0 {
		return q[i+1:]
	}
	return q
}

func Measure(asksDir, answersDir string) Ledger {
	var l Ledger
	if entries, err := os.ReadDir(asksDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			l.AskBytes += int(info.Size())
			switch e.Name() {
			case "PREAMBLE.md":
				l.PreambleByes = int(info.Size())
			case "INSTRUCTIONS.md", "report.md":

			default:
				l.Prompts++
				answer := strings.TrimSuffix(e.Name(), ".md") + ".json"
				if _, err := os.Stat(filepath.Join(answersDir, answer)); err == nil {
					l.AnsweredPrompts++
				}
			}
		}
	}
	if entries, err := os.ReadDir(answersDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if info, err := e.Info(); err == nil {
				l.AnswerBytes += int(info.Size())
				l.Answers++
			}
		}
	}
	l.Missing = l.Prompts - l.AnsweredPrompts
	l.Round = packRound(asksDir)
	return l
}

func packRound(asksDir string) int {
	b, err := os.ReadFile(filepath.Join(asksDir, "manifest.json"))
	if err != nil {
		return 1
	}
	var m struct {
		Round int `json:"round"`
	}
	if json.Unmarshal(b, &m) != nil || m.Round == 0 {
		return 1
	}
	return m.Round
}
