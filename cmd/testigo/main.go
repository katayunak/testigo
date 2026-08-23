package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal"
	"github.com/katayunak/testigo/internal/askingAgent"
	"github.com/katayunak/testigo/internal/askingAgent/askEntity"
	"github.com/katayunak/testigo/internal/config"
	"github.com/katayunak/testigo/internal/report"
	"github.com/katayunak/testigo/internal/scanningFlow"
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/storage"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"

	model "github.com/katayunak/testigo/internal"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "testigo:", err)
		os.Exit(1)
	}
}

const usage = `testigo — test your Go fintech flows

phase 1 · ScanningTheFlow

  testigo init    [dir]   write a starter testigo.json
  testigo entry   [dir]   add <pkg>#<Symbol> [label]
                          append an entry point to testigo.json
  testigo scan    [dir]   run ScanningTheFlow, write .testigo/flow.json
  testigo flow    [dir]   print the flow as a Mermaid diagram

  testigo ask     [dir]   write the prompt pack for your agent (--round 1 or 2)
  testigo collect [dir]   read the answers back, validate them, apply them
  testigo cases   [dir]   list the test plan and what it would cost, spending nothing
  testigo rules   [dir]   write a starter testigo.rules.json: what money movement means here

Phase 1 (ScanningTheFlow) reads only. Nothing is written into your source files.
`

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	round := fs.Int("round", 1, "which round of questions to write: 1 collects context, 2 asks for tests")
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
		return cmdAsk(root, *round)
	case "collect":
		return cmdCollect(root)
	case "cases":
		return cmdCases(root)
	case "rules":
		return cmdRules(root)
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// cmdInit initialized the file for scanning the flow
func cmdInit(root string) error {
	p := filepath.Join(root, config.FileName)
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("%s already exists", p)
	}

	if err := os.WriteFile(p, []byte(config.Example), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s — edit the entries, then run 'testigo entries' for candidates\n", p)

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

	prev, err := storage.Load(root)
	if err != nil && !errors.Is(err, storage.ErrNoSidecar) {
		return err
	}
	stats := storage.Merge(prev, res.Flow, res.Index)
	if err := storage.Save(root, res.Flow); err != nil {
		return err
	}
	printSummary(res.Flow, stats, storage.Path(root))
	return nil
}

func cmdFlow(root string) error {
	f, err := storage.Load(root)
	if err != nil {
		return fmt.Errorf("%w — run 'testigo scan' first", err)
	}
	fmt.Println("```mermaid")
	fmt.Print(report.Flowchart(f))
	fmt.Println("```")
	for _, m := range f.Machines {
		fmt.Printf("\n## state machine: %s\n\n```mermaid\n%s```\n", m.Type, report.StateDiagram(m))
	}
	return nil
}

func printSummary(f *flowEntity.Flow, stats storage.MergeStats, path string) {
	fmt.Printf("phase       1 · %s\n", internal.PhaseScanningTheFlow)
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
	for _, m := range f.Machines {
		fmt.Printf("states      %s: %d states, %d write sites\n", m.Type, len(m.States), len(m.Writes))
	}
	fmt.Printf("notes       %s\n", stats)

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

	// The whole point of separating facts from context: say plainly what is
	// still unknown, rather than letting a confident-looking report imply the
	// analysis is complete.
	var unknown []string
	if len(f.Machines) > 0 {
		unknown = append(unknown, "which state transitions are legal")
	}
	if con > 0 {
		unknown = append(unknown, "how the uninjectable seams behave under failure")
	}
	if stats.NeedsAgent() > 0 {
		unknown = append(unknown, fmt.Sprintf("what %d unannotated step(s) mean in business terms", stats.NeedsAgent()))
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// cmdAsk writes the prompt pack. It never contacts a network and never needs a
// key: the pack is markdown and JSON on disk, so any agent can answer it and a
// person can answer it by hand when the agent gets one wrong.
func cmdAsk(root string, roundNum int) error {
	flow, err := storage.Load(root)
	if err != nil {
		return fmt.Errorf("%w — run 'testigo scan' first", err)
	}
	knowledge, err := askingAgent.LoadKnowledge(storage.Dir(root))
	if err != nil {
		return err
	}
	rules, err := config.LoadRules(root)
	if err != nil {
		return err
	}

	round := askEntity.Round(roundNum)
	if round == askEntity.RoundGenerate {
		if reason := askingAgent.BlockedReason(flow, knowledge); reason != "" {
			return fmt.Errorf("round 2 needs round 1 first: %s\n\n"+
				"Generating tests from half-collected context produces tests that look\n"+
				"complete and check the wrong thing, which is the whole reason the rounds\n"+
				"are separate", reason)
		}
	}

	asks := askingAgent.Plan(flow, knowledge, round)
	if len(asks) == 0 {
		fmt.Println("nothing to ask — every question for this round is already answered")
		return nil
	}
	pack, err := askingAgent.Write(storage.Dir(root), round, asks)
	if err != nil {
		return err
	}

	byKind := map[askEntity.Kind]int{}
	for _, a := range asks {
		byKind[a.Kind]++
	}
	fmt.Printf("phase       2 · %s\n", model.PhaseAskingTheAgent)
	fmt.Printf("round       %d (%s)\n", roundNum, round)
	fmt.Printf("questions   %d\n", len(asks))
	for _, k := range []askEntity.Kind{
		askEntity.KindBinding, askEntity.KindTransitions,
		askEntity.KindExternalEffect, askEntity.KindNotes,
		askEntity.KindTestCase,
	} {
		if byKind[k] > 0 {
			fmt.Printf("              %-16s %d\n", k, byKind[k])
		}
	}
	cost := askingAgent.Estimate(round, asks)
	fmt.Printf("est. cost   ~%s tokens across %d prompt(s)\n", thousands(cost.EstTokens), cost.Prompts)
	if cost.SavedByShared > 0 {
		fmt.Printf("            ~%s saved by hoisting the shared rules into PREAMBLE.md\n", thousands(cost.SavedByShared))
	}

	if round == askEntity.RoundGenerate {
		var blocked []string
		for _, c := range askingAgent.Cases(flow, knowledge, rules) {
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

// cmdCollect reads the answers, checks them against what the compiler proved,
// and applies only the ones that survive.
func cmdCollect(root string) error {
	flow, err := storage.Load(root)
	if err != nil {
		return fmt.Errorf("%w — run 'testigo scan' first", err)
	}
	knowledge, err := askingAgent.LoadKnowledge(storage.Dir(root))
	if err != nil {
		return err
	}
	rules, err := config.LoadRules(root)
	if err != nil {
		return err
	}
	pack, err := askingAgent.ReadPack(storage.Dir(root))
	if err != nil {
		return fmt.Errorf("%w — run 'testigo ask' first", err)
	}

	_ = rules // collect validates answers; rules shape the plan, not the validation

	got, err := askingAgent.Collect(storage.Dir(root), flow, knowledge, pack.Asks)
	if err != nil {
		return err
	}

	fmt.Printf("phase       2 · %s\n", model.PhaseAskingTheAgent)
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

	if err := askingAgent.SaveKnowledge(storage.Dir(root), knowledge); err != nil {
		return err
	}
	if err := storage.Save(root, flow); err != nil {
		return err
	}

	if len(got.Cases) > 0 {
		return applyGeneratedTests(root, got.Cases)
	}
	if reason := askingAgent.BlockedReason(flow, knowledge); reason != "" {
		fmt.Printf("round 2 not ready: %s\n", reason)
	} else {
		fmt.Println("round 1 complete — run 'testigo ask --round 2' to generate tests")
	}
	return nil
}

func applyGeneratedTests(root string, cases []*askEntity.CaseAnswer) error {
	var ran []struct {
		file  string
		names []string
	}
	byFile := map[string][]string{}

	for _, c := range cases {
		if !c.Written() {
			fmt.Printf("  blocked   %s\n            %s\n", c.CaseID, c.BlockedReason)
			if c.Needed != "" {
				fmt.Printf("            needs: %s\n", c.Needed)
			}
			continue
		}
		out, err := askingAgent.WriteTest(root, c)
		if err != nil {
			fmt.Printf("  REFUSED   %s: %v\n", c.CaseID, err)
			continue
		}
		switch {
		case !out.Compiles:
			fmt.Printf("  no build  %s -> %s\n%s\n", c.CaseID, out.Path, indentBlock(out.Output))
			fmt.Println("            feed that output back and re-answer this one case")
			continue
		case !out.Vets:
			fmt.Printf("  vet fail  %s -> %s\n%s\n", c.CaseID, out.Path, indentBlock(out.Output))
			continue
		}
		fmt.Printf("  written   %-34s %s\n", c.CaseID, out.Path)
		if c.ExpectedToFail != "" {
			fmt.Printf("            expected to fail: %s\n", c.ExpectedToFail)
		}
		byFile[out.Path] = append(byFile[out.Path], c.FuncName)
	}

	for file, names := range byFile {
		ran = append(ran, struct {
			file  string
			names []string
		}{file, names})
	}
	if len(ran) == 0 {
		fmt.Println("\nnothing compiled — no tests were run")
		return nil
	}

	fmt.Printf("\nrunning with -race...\n\n")
	for _, r := range ran {
		out, passed := askingAgent.RunTests(root, r.file, r.names)
		fmt.Println(out)
		if !passed {
			fmt.Println("A red result may be the point. Check it against any")
			fmt.Println("expected-to-fail note above before assuming the test is wrong.")
		}
	}
	return nil
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

// cmdCases shows the plan without writing a prompt or spending a token.
//
// Worth having its own command because the decision it supports is "is this
// worth paying for", and that decision has to be available BEFORE the money is
// spent. It also shows the blocked cases, which are the more interesting half:
// a repository where fifteen scenarios are blocked on concrete seams has just
// been told the most useful thing testigo knows about it.
func cmdCases(root string) error {
	flow, err := storage.Load(root)
	if err != nil {
		return fmt.Errorf("%w — run 'testigo scan' first", err)
	}
	knowledge, err := askingAgent.LoadKnowledge(storage.Dir(root))
	if err != nil {
		return err
	}
	rules, err := config.LoadRules(root)
	if err != nil {
		return err
	}
	cases := askingAgent.Cases(flow, knowledge, rules)

	var runnable, blocked []planEntity.TestCase
	for _, c := range cases {
		if c.Runnable() {
			runnable = append(runnable, c)
		} else {
			blocked = append(blocked, c)
		}
	}

	if knowledge.Binding == nil {
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

	asks := askingAgent.Plan(flow, knowledge, askEntity.RoundGenerate)
	cost := askingAgent.Estimate(askEntity.RoundGenerate, asks)
	fmt.Printf("\ngenerating these would cost roughly %s tokens of input\n", thousands(cost.EstTokens))
	return nil
}

// cmdRules scaffolds the business rules file.
//
// The one thing testigo cannot work out from code. "Money movement" is not the
// same event in a wallet, a switch, and a service-activation system, and a tool
// that assumed one definition would generate confident wrong tests for the other
// two. Two minutes writing this down removes a whole class of noise.
func cmdRules(root string) error {
	p := filepath.Join(root, config.RulesFileName)
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("%s already exists", p)
	}
	if err := os.WriteFile(p, []byte(config.RulesExample), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n\n", p)
	fmt.Println("Everything in it is optional. The field worth the most thought is")
	fmt.Println("money_movement.external_signal — set it only if money moves in this system")
	fmt.Println("because a message was SENT, not because a row changed. That single fact")
	fmt.Println("changes which tests make sense.")
	return nil
}

// cmdEntry appends one entry point to testigo.json.
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

	// skips adding redundant entry points
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
