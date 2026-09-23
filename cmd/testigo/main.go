package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/agent"
	"github.com/katayunak/testigo/internal/agent/domain"
	"github.com/katayunak/testigo/internal/agent/prompts"
	"github.com/katayunak/testigo/internal/config"
	"github.com/katayunak/testigo/internal/report"
	"github.com/katayunak/testigo/internal/scanningFlow"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
)

const (
	phaseScanningTheFlow = "ScanningTheFlow"
	phaseAskingTheAgent  = "AskingTheAgent"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "testigo:", err)
		os.Exit(1)
	}
}

const usage = `testigo — test your Go fintech flows

phase 1 · ScanningTheFlow

  testigo init    [dir]   write a starter testigo/config.json
  testigo entry   [dir]   add <pkg>#<Symbol> [label]
                          append an entry point to testigo/config.json
  testigo scan    [dir]   run ScanningTheFlow, write testigo/flow.json
  testigo flow    [dir]   print the flow as a Mermaid diagram

  testigo ask     [dir]   write the prompt pack for your agent (--round 1 or 2)
                          --explain shows what the planner asked for and what it skipped
                          --budget N caps the weighted tokens it will commit
  testigo collect [dir]   read the answers back, validate them, apply them
  testigo cases   [dir]   list the test plan and what it would cost, spending nothing
  testigo report  [dir]   everything found so far, and what it cost to find
                          --ask writes the prompt for an agent to produce REPORT.md
  testigo rules   [dir]   write a starter testigo/rules.json: what money movement means here
  testigo mcp     [dir]   serve round 1 and round 2 as MCP tools over stdio,
                          instead of files an agent reads and a human collects

Phase 1 (ScanningTheFlow) reads only. Everything testigo writes goes in testigo/;
your source files are never touched.
`

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	round := fs.Int("round", 1, "which round of questions to write: 1 collects context, 2 asks for tests")
	budget := fs.Int("budget", 0, "stop planning asks once this many weighted tokens are committed; 0 means no ceiling")
	explain := fs.Bool("explain", false, "print what the planner chose, what it skipped and why, then carry on")
	askFor := fs.Bool("ask", false, "report: write the prompt that has an agent produce testigo/REPORT.md")
	if err := fs.Parse(rest); err != nil {
		return err
	}

	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}

	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	switch cmd {
	case "entry":
		return cmdEntry(root, fs.Args())
	case "init":
		return cmdInit(root)
	case "scan":
		return cmdScan(root)
	case "flow":
		return cmdFlow(root)
	case "ask":
		return cmdAsk(root, *round, *budget, *explain)
	case "collect":
		return cmdCollect(root)
	case "cases":
		return cmdCases(root)
	case "rules":
		return cmdRules(root)
	case "report":
		return cmdReport(root, *askFor)
	case "mcp":
		return cmdMCP(root)
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func cmdInit(root string) error {
	p := config.Path(root)
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("%s already exists", p)
	}

	if err := os.MkdirAll(config.Dir(root), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(config.Example), 0o644); err != nil {
		return err
	}

	fmt.Printf("wrote %s\n", p)
	fmt.Println("declare your entry points in it before scanning:")
	fmt.Println("  edit the \"entries\" array by hand, or")
	fmt.Println("  testigo entry . add <pkg>#<Symbol> [label]")
	fmt.Println()
	fmt.Printf("everything testigo writes lives in %s/ — add it to .gitignore,\n", config.DirName)
	fmt.Printf("except %s/agentResponse.json, which holds the agent answers and is worth committing\n", config.DirName)

	return nil
}

func cmdScan(root string) error {
	cfg, err := config.Load(root)
	if err != nil {
		return fmt.Errorf("%w\n\nrun 'testigo init' first", err)
	}

	if err := cfg.RequireEntries(); err != nil {
		return err
	}

	res, err := scanningFlow.Scan(scanningFlow.Options{Root: root, Entries: cfg.EntryPoints()})
	if err != nil {
		return err
	}

	if err := config.SaveFlow(root, res.Flow); err != nil {
		return err
	}
	printSummary(res.Flow, config.FlowPath(root))
	return nil
}

func cmdFlow(root string) error {
	f, err := config.LoadFlow(root)
	if err != nil {
		return fmt.Errorf("%w — run 'testigo scan' first", err)
	}
	fmt.Println("```mermaid")
	fmt.Print(report.Flowchart(f))
	fmt.Println("```")
	for _, m := range f.States {
		fmt.Printf("\n## state machine: %s\n\n```mermaid\n%s```\n", m.Type, report.StateDiagram(m))
	}
	return nil
}

func printSummary(f *flowEntity.Flow, path string) {
	fmt.Printf("phase       1 · %s\n", phaseScanningTheFlow)
	fmt.Printf("module      %s\n", f.Module)
	fmt.Printf("entries     %d\n", len(f.Entries))
	fmt.Printf("nodes       %d functions reachable from the entry points\n", len(f.Nodes))

	inj, con := 0, 0
	for _, s := range f.Seams {
		if s.Injectable {
			inj++
		} else {
			con++
		}
	}
	fmt.Printf("seams       %d injectable, %d on concrete types\n", inj, con)
	for _, m := range f.States {
		fmt.Printf("states      %s: %d states, %d write sites\n", m.Type, len(m.States), len(m.Writes))
	}
	if f.GeneratedFiles > 0 {

		fmt.Printf("generated   %d file(s) marked DO NOT EDIT; %d finding(s) from them not reported\n",
			f.GeneratedFiles, f.GeneratedFindings)
	}

	if len(f.Findings) > 0 {
		counts := map[flowEntity.Severity]int{}
		for _, x := range f.Findings {
			counts[x.Severity]++
		}
		fmt.Printf("\nfindings    %d critical, %d high, %d medium, %d info\n\n",
			counts[flowEntity.SevCritical], counts[flowEntity.SevHigh], counts[flowEntity.SevMedium], counts[flowEntity.SevInfo])
		shown := f.Findings
		if len(shown) > 12 {
			shown = shown[:12]
		}
		for _, x := range shown {
			loc := x.Ref.File
			if loc == "" {
				loc = x.Ref.Pkg
			}
			fmt.Printf("  [%-8s] %-20s %s\n", x.Severity, x.ID, x.Title)
			fmt.Printf("             %s:%d\n", loc, x.Line)
		}
		if len(f.Findings) > len(shown) {
			fmt.Printf("  ... %d more in %s\n", len(f.Findings)-len(shown), path)
		}
	}

	var unknown []string
	if len(f.States) > 0 {
		unknown = append(unknown, "which state transitions are legal")
	}
	if con > 0 {
		unknown = append(unknown, "how the uninjectable seams behave under failure")
	}
	if len(unknown) > 0 {
		fmt.Printf("\nstill unknown (phase 2 asks an agent):\n")
		for _, u := range unknown {
			fmt.Printf("  - %s\n", u)
		}
	}
	fmt.Printf("\nwrote %s\n", path)
	_ = sort.Strings
}

func cmdAsk(root string, roundNum int, budget int, explain bool) error {
	flow, err := config.LoadFlow(root)
	if err != nil {
		return fmt.Errorf("%w — run 'testigo scan' first", err)
	}
	answers, err := agent.LoadAgentResponse(config.Dir(root))
	if err != nil {
		return err
	}
	rules, err := config.LoadRules(root)
	if err != nil {
		return err
	}

	round := domain.Round(roundNum)
	if round == domain.RoundGenerate {
		if reason := agent.BlockedReason(flow, answers); reason != "" {
			return fmt.Errorf("round 2 needs round 1 first: %s\n\n"+
				"Generating tests from half-collected context produces tests that look\n"+
				"complete and check the wrong thing, which is the whole reason the rounds\n"+
				"are separate", reason)
		}
	}

	asks, plan := agent.PlanWith(flow, answers, round, rules, budget)
	if explain && round == domain.RoundUnderstand {
		fmt.Println(plan.Explain())
	}
	if len(asks) == 0 {
		fmt.Println("nothing to ask — every question for this round is already answered")
		return nil
	}
	pack, err := agent.Write(config.Dir(root), round, asks, agent.Preamble(flow, round))
	if err != nil {
		return err
	}

	byKind := map[domain.Kind]int{}
	for _, a := range asks {
		byKind[a.Kind]++
	}
	fmt.Printf("phase       2 · %s\n", phaseAskingTheAgent)
	fmt.Printf("round       %d (%s)\n", roundNum, round)
	fmt.Printf("questions   %d\n", len(asks))

	shown := map[domain.Kind]bool{}
	for _, k := range []domain.Kind{
		domain.KindMoneyModel, domain.KindMainEntity,
		domain.KindStateRoles,
		domain.KindExternalEffect,
		domain.KindTestCase,
	} {
		shown[k] = true
		if byKind[k] > 0 {
			fmt.Printf("              %-16s %d\n", k, byKind[k])
		}
	}
	for k, n := range byKind {
		if !shown[k] && n > 0 {
			fmt.Printf("              %-16s %d\n", k, n)
		}
	}
	cost := agent.Estimate(round, asks, agent.Preamble(flow, round))
	fmt.Printf("est. cost   ~%s tokens across %d prompt(s)\n", thousands(cost.EstTokens), cost.Prompts)
	if cost.SavedByShared > 0 {
		fmt.Printf("            ~%s saved by hoisting the shared rules into PREAMBLE.md\n", thousands(cost.SavedByShared))
	}

	if round == domain.RoundGenerate {
		var blocked []string
		for _, c := range agent.Cases(flow, answers, rules) {
			if !c.Runnable() {
				blocked = append(blocked, fmt.Sprintf("  %-34s %s", c.Scenario.ID, c.Blocked))
			}
		}
		if len(blocked) > 0 {
			fmt.Printf("\nnot asked (%d scenario(s) do not apply here):\n", len(blocked))
			for _, b := range blocked {
				fmt.Println(b)
			}
		}
	}

	fmt.Printf("\nwrote %s\n", pack.Dir)
	fmt.Printf("\nPoint your agent at %s/INSTRUCTIONS.md, then run 'testigo collect'.\n", pack.Dir)
	return nil
}

func cmdCollect(root string) error {
	flow, err := config.LoadFlow(root)
	if err != nil {
		return fmt.Errorf("%w — run 'testigo scan' first", err)
	}
	answers, err := agent.LoadAgentResponse(config.Dir(root))
	if err != nil {
		return err
	}
	rules, err := config.LoadRules(root)
	if err != nil {
		return err
	}
	pack, err := agent.ReadPack(config.Dir(root))
	if err != nil {
		return fmt.Errorf("%w — run 'testigo ask' first", err)
	}

	_ = rules

	got, err := agent.Collect(config.Dir(root), flow, answers, pack.Asks)
	if err != nil {
		return err
	}

	fmt.Printf("phase       2 · %s\n", phaseAskingTheAgent)
	fmt.Printf("round       %d\n", int(pack.Round))
	fmt.Printf("answers     %s\n\n", got)

	for _, id := range got.Missing {
		fmt.Printf("  missing   %s\n", id)
	}
	for id, problem := range got.Invalid {
		fmt.Printf("  INVALID   %s: %v\n", id, problem)
	}
	if len(got.Missing) > 0 || len(got.Invalid) > 0 {
		fmt.Println()
	}

	if err := agent.SaveAgentResponse(config.Dir(root), answers); err != nil {
		return err
	}
	if err := config.SaveFlow(root, flow); err != nil {
		return err
	}

	if len(got.Cases) > 0 {
		return applyGeneratedTests(root, config.Dir(root), answers, got.Cases)
	}
	if reason := agent.BlockedReason(flow, answers); reason != "" {
		fmt.Printf("round 2 not ready: %s\n", reason)
	} else {
		fmt.Println("round 1 complete — run 'testigo ask --round 2' to generate tests")
	}
	return nil
}

func applyGeneratedTests(root, sidecarDir string, answers *domain.AgentResponse, cases []*domain.CaseAnswer) error {
	byFile := map[string][]string{}
	runs := map[string]*domain.TestRun{}
	var ids []string

	for _, c := range cases {
		r := &domain.TestRun{CaseID: c.CaseID,
			Func: c.FuncName, ExpectedToFail: c.ExpectedToFail}
		runs[c.CaseID] = r
		ids = append(ids, c.CaseID)

		if !c.Written() {
			r.Status = "blocked"
			r.Reason = c.BlockedReason
			fmt.Printf("  blocked   %s\n            %s\n", c.CaseID, c.BlockedReason)
			if c.Needed != "" {
				fmt.Printf("            needs: %s\n", c.Needed)
			}
			continue
		}
		out, err := agent.WriteTest(root, c)
		if err != nil {
			r.Status = "refused"
			r.Reason = err.Error()
			fmt.Printf("  REFUSED   %s: %v\n", c.CaseID, err)
			continue
		}
		r.File = out.Path
		switch {
		case !out.Compiles:
			r.Status = "no_build"
			r.Output = trimOutput(out.Output)
			fmt.Printf("  no build  %s -> %s\n%s\n", c.CaseID, out.Path, indentBlock(out.Output))
			fmt.Println("            feed that output back and re-answer this one case")
			continue
		case !out.Vets:
			r.Status = "vet_fail"
			r.Output = trimOutput(out.Output)
			fmt.Printf("  vet fail  %s -> %s\n%s\n", c.CaseID, out.Path, indentBlock(out.Output))
			continue
		}
		r.Status = "written"
		fmt.Printf("  written   %-34s %s\n", c.CaseID, out.Path)
		if c.ExpectedToFail != "" {
			fmt.Printf("            expected to fail: %s\n", c.ExpectedToFail)
		}
		byFile[out.Path] = append(byFile[out.Path], c.FuncName)
	}

	if len(byFile) == 0 {
		fmt.Println("\nnothing compiled — no tests were run")
	} else {
		fmt.Printf("\nrunning with -race...\n\n")
		var files []string
		for f := range byFile {
			files = append(files, f)
		}
		sort.Strings(files)
		for _, file := range files {
			out, passed := agent.RunTests(root, file, byFile[file])
			fmt.Println(out)
			failed := failedNames(out)
			for _, r := range runs {
				if r.File != file || r.Status != "written" {
					continue
				}
				ok := passed
				if !passed && len(failed) > 0 {
					ok = !failed[r.Func]
				}
				r.Passed = &ok
				if !ok {
					r.Output = trimOutput(out)
				}
			}
			if !passed {
				fmt.Println("A red result may be the point. Check it against any")
				fmt.Println("expected-to-fail note above before assuming the test is wrong.")
			}
		}
	}

	sort.Strings(ids)
	answers.TestRuns = nil
	for _, id := range ids {
		answers.TestRuns = append(answers.TestRuns, *runs[id])
	}
	return agent.SaveAgentResponse(sidecarDir, answers)
}

func failedNames(out string) map[string]bool {
	m := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "--- FAIL:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "--- FAIL:"))
		if i := strings.IndexAny(name, " \t("); i > 0 {
			name = name[:i]
		}
		m[name] = true
	}
	return m
}

func trimOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1200 {
		return s[:1200] + "\n… truncated"
	}
	return s
}

func indentBlock(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("            " + line + "\n")
	}
	return b.String()
}

func thousands(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func cmdCases(root string) error {
	flow, err := config.LoadFlow(root)
	if err != nil {
		return fmt.Errorf("%w — run 'testigo scan' first", err)
	}
	answers, err := agent.LoadAgentResponse(config.Dir(root))
	if err != nil {
		return err
	}
	rules, err := config.LoadRules(root)
	if err != nil {
		return err
	}
	cases := agent.Cases(flow, answers, rules)

	var runnable, blocked []testPlan.TestCase
	for _, c := range cases {
		if c.Runnable() {
			runnable = append(runnable, c)
		} else {
			blocked = append(blocked, c)
		}
	}

	if answers.MoneyModel == nil {
		fmt.Println("note: round 1 has not been answered, so this plan is the")
		fmt.Println("      unfiltered catalog. Answering it usually removes several.")
		fmt.Println()
	}

	fmt.Printf("%d of %d scenarios apply to this repository\n\n", len(runnable), len(cases))
	for _, c := range runnable {
		fmt.Printf("  %-8s %-34s %-18s %-6s %s\n",
			c.Scenario.Severity, c.Scenario.ID, c.Technique, c.Size, c.Scenario.Oracle)
	}
	if len(blocked) > 0 {
		fmt.Printf("\nnot applicable:\n\n")
		for _, c := range blocked {
			fmt.Printf("  %-34s %s\n", c.Scenario.ID, c.Blocked)
		}
	}

	asks := agent.Plan(flow, answers, domain.RoundGenerate)
	cost := agent.Estimate(domain.RoundGenerate, asks, agent.Preamble(flow, domain.RoundGenerate))
	fmt.Printf("\ngenerating these would cost roughly %s tokens of input\n", thousands(cost.EstTokens))
	return nil
}

func cmdRules(root string) error {
	if err := os.MkdirAll(config.Dir(root), 0o755); err != nil {
		return err
	}
	p := filepath.Join(config.Dir(root), config.RulesFileName)
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("%s already exists", p)
	}
	if err := os.WriteFile(p, []byte(config.RulesExample), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n\n", p)
	fmt.Println("Both fields are optional. money_movement.symbols is the one worth")
	fmt.Println("thought: name the function that actually commits money and testigo")
	fmt.Println("stops guessing which one it is. skip turns off catalogue scenarios")
	fmt.Println("that do not apply here, with the reason recorded beside each.")
	return nil
}

func cmdEntry(root string, args []string) error {
	if len(args) > 0 && args[0] != "add" {
		args = args[1:]
	}

	if len(args) < 2 || args[0] != "add" {
		return fmt.Errorf("usage: testigo entry [dir] add <pkg>#<Symbol> [label]\n\n" +
			"  testigo entry . add example.com/pay/api#(*Server).CreatePayment \"API create\"\n\n" +
			"Method syntax is (*Type).Method or Type.Method. Run 'testigo init' first.")
	}

	spec := args[1]
	pkg, symbol, ok := strings.Cut(spec, "#")
	if !ok || pkg == "" || symbol == "" {
		return fmt.Errorf("entry point must be <import-path>#<Symbol>, got %q", spec)
	}

	label := ""
	if len(args) > 2 {
		label = strings.Join(args[2:], " ")
	}

	cfg, err := config.Load(root)
	if err != nil {
		return fmt.Errorf("%w\n\nrun 'testigo init' first", err)
	}

	for _, e := range cfg.Entries {
		if e.Pkg == pkg && e.Symbol == symbol {
			return fmt.Errorf("%s#%s is already an entry point", pkg, symbol)
		}
	}

	cfg.Entries = append(cfg.Entries, config.Entry{Pkg: pkg, Symbol: symbol, Label: label})
	if err := config.Save(root, cfg); err != nil {
		return err
	}

	fmt.Printf("added %s#%s\n", pkg, symbol)
	fmt.Printf("%s now has %d entry point(s)\n\n", config.FileName, len(cfg.Entries))
	return nil
}

func cmdReport(root string, askFor bool) error {
	flow, err := config.LoadFlow(root)
	if err != nil {
		return fmt.Errorf("%w — run 'testigo scan' first", err)
	}

	answers, _ := agent.LoadAgentResponse(config.Dir(root))

	asksDir := filepath.Join(config.Dir(root), "asks")
	ledger := report.Measure(asksDir, filepath.Join(config.Dir(root), "answers"))

	if !askFor {
		fmt.Print(report.Report(flow, answers, ledger, asksDir))
		return nil
	}

	rules, err := config.LoadRules(root)
	if err != nil {
		return err
	}
	tok := prompts.Tokens{
		AskTokens:    ledger.AskBytes / 4,
		AnswerTokens: ledger.AnswerBytes / 4,
		Prompts:      ledger.Prompts,
	}
	if ledger.Prompts > 1 {
		tok.Saved = ledger.PreambleByes * (ledger.Prompts - 1) / 4
	}

	p := prompts.Report(flow, answers, agent.Cases(flow, answers, rules), tok)
	p.Trim(agent.MaxPromptChars)
	body := p.Render()

	if err := os.MkdirAll(asksDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(asksDir, "report.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}

	fmt.Printf("phase       3 · report\n")
	fmt.Printf("prompt      %s  (~%s tokens)\n", path, kilo(len(body)/4))
	fmt.Printf("tests       %d run(s) on record\n", len(answers.TestRuns))
	fmt.Printf("findings    %d\n", len(flow.Findings))
	fmt.Println()
	fmt.Printf("Point your agent at %s.\n", path)
	fmt.Println("It writes one file: testigo/REPORT.md")
	return nil
}

func kilo(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}
