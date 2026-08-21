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
	"github.com/katayunak/testigo/internal/config"
	"github.com/katayunak/testigo/internal/report"
	"github.com/katayunak/testigo/internal/scanningFlow"
	"github.com/katayunak/testigo/internal/storage"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "testigo:", err)
		os.Exit(1)
	}
}

// Q: why "entries" and not "entry"? maybe entry --add is better
// A: `entries` is a QUERY. It scans the repo and prints what it found; it
//
//	changes nothing. `entry --add` would be a MUTATION — it writes to
//	testigo.json. Putting a query and a mutation behind one command is what
//	makes a CLI hard to trust, because you stop being sure whether running it
//	is safe.
//
//	Your instinct is right about the friction though: hand-editing JSON after
//	reading a list is annoying. The fix is a separate verb, not a flag:
//
//	    testigo entries              # list candidates (read)
//	    testigo entry add pkg#Sym    # append to testigo.json (write)
//	    testigo entry rm  pkg#Sym
//
//	Worth building once you have run this on two or three real repos and know
//	how often you actually edit the list. Not before — a CLI verb you guessed
//	at is harder to remove than to add.
//
// Q: why [dir]?
// A: testigo analyses a DIFFERENT repository than the one it lives in. The
//
//	binary sits in ~/go/kat/testigo; the code under test is in
//	~/go/780/jiring-switch. So every command takes the target repo as an
//	argument and defaults to "." when you leave it out:
//
//	    cd ~/go/780/jiring-switch && testigo scanningFlow
//	    testigo scanningFlow ~/go/780/jiring-switch      # same thing from anywhere
//
//	This is also why Options.Env exists: pointing at another repo means you
//	can pick up ITS go.work by accident, not yours.
const usage = `testigo — test your Go fintech flows

phase 1 · ScanningTheFlow

  testigo entries [dir]   suggest entry points for the payment flow
  testigo init    [dir]   write a starter testigo.json
  testigo scanningFlow    [dir]   run ScanningTheFlow, write .testigo/flow.json
  testigo flow    [dir]   print the flow as a Mermaid diagram

Phase 1 (ScanningTheFlow) reads only. Nothing is written into your source files.
`

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	cmd, rest := args[0], args[1:]
	// Q: what is the -json flag?
	// A: It was nothing. I declared it, never read it, and the compiler did not
	//    complain because assigning to a variable counts as using it. So it
	//    showed up in --help as a promise the program did not keep. Deleted.
	//
	//    The lesson is worth more than the flag: Go's unused-variable check does
	//    not catch a variable that is written but never meaningfully read. `go
	//    vet` will not either. Only reading the code catches this, which is what
	//    you just did.
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
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
	case "entries":
		return cmdEntries(root)
	case "init":
		return cmdInit(root)
	case "scanningFlow":
		return cmdScan(root)
	case "flow":
		return cmdFlow(root)
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

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

func cmdEntries(root string) error {
	cands, err := scanningFlow.SuggestEntries(root, nil, nil)
	if err != nil {
		return err
	}
	if len(cands) == 0 {
		fmt.Println("no candidates found — name the entry points by hand in testigo.json")
		return nil
	}
	fmt.Println("Candidate entry points (highest confidence first).")
	fmt.Println("Pick the ones that start a payment flow, including the webhook and the reconciliation job:")
	fmt.Println()
	if len(cands) > 25 {
		cands = cands[:25]
	}
	for _, c := range cands {
		fmt.Printf("  %2d  %s#%s\n", c.Score, c.Entry.Pkg, c.Entry.Symbol)
		fmt.Printf("      %s:%d  [%s]\n", c.File, c.Line, strings.Join(c.Evidence, ", "))
	}
	fmt.Println("\nyaml:")
	for _, c := range cands[:min(4, len(cands))] {
		fmt.Printf("  - pkg: %s\n    symbol: %s\n", c.Entry.Pkg, c.Entry.Symbol)
	}
	return nil
}

func cmdScan(root string) error {
	cfg, err := config.Load(root)
	if err != nil {
		return fmt.Errorf("%w\n\nrun 'testigo init' then 'testigo entries'", err)
	}
	res, err := scanningFlow.Scan(scanningFlow.Options{Root: root, Patterns: cfg.Patterns, Entries: cfg.EntryPoints()})
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
		return fmt.Errorf("%w — run 'testigo scanningFlow' first", err)
	}
	fmt.Println("```mermaid")
	fmt.Print(report.Flowchart(f))
	fmt.Println("```")
	for _, m := range f.Machines {
		fmt.Printf("\n## state machine: %s\n\n```mermaid\n%s```\n", m.Type, report.StateDiagram(m))
	}
	return nil
}

func printSummary(f *scanningFlow.Flow, stats storage.MergeStats, path string) {
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
		counts := map[scanningFlow.Severity]int{}
		for _, x := range f.Findings {
			counts[x.Severity]++
		}
		fmt.Printf("\nfindings    %d critical, %d high, %d medium, %d info\n\n",
			counts[scanningFlow.SevCritical], counts[scanningFlow.SevHigh], counts[scanningFlow.SevMedium], counts[scanningFlow.SevInfo])
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

	// The whole point of separating facts from judgment: say plainly what is
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
