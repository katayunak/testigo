package prompts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// Step is one function in an entry point's path, with everything phase 1 proved
// about it attached.
//
// This is the unit every prompt is built from. The agent is never handed a raw
// file and asked to find its way: it is handed an ordered list of steps, each
// already annotated with what touches the database, what leaves the process,
// and where the status changes. The reading is done. Only the context is left.
type Step struct {
	Order int
	// Depth is how many calls deep this step sits from the entry point. Used to
	// indent the rendering, so nesting is visible: a provider call INSIDE the
	// function that opened the transaction is a different situation from one
	// beside it.
	Depth  int
	Ref    string // node ID
	Symbol string
	File   string
	Line   int
	Facts  flowEntity.Facts
	Seams  []flowEntity.Seam
	States []flowEntity.StateWrite
}

// Path is one entry point and the steps reachable from it.
type Path struct {
	Entry flowEntity.EntryPoint
	Label string
	Steps []Step
}

// Paths orders the flow into one readable sequence per entry point.
//
// The order is a breadth-first walk of the call graph. It is not the true
// execution order — a compiler cannot know which branch runs — and the prompts
// say so explicitly rather than letting the agent assume otherwise. What it IS
// is a complete list of what the entry point can reach, which is the part that
// matters for asking "where could this fail".
func Paths(f *flowEntity.Flow) []Path {
	seamsBy := map[string][]flowEntity.Seam{}
	for _, s := range f.Seams {
		seamsBy[s.In.ID()] = append(seamsBy[s.In.ID()], s)
	}
	statesBy := map[string][]flowEntity.StateWrite{}
	for _, m := range f.States {
		for _, w := range m.Writes {
			statesBy[w.In.ID()] = append(statesBy[w.In.ID()], w)
		}
	}

	// Within a function, sort by line. The order of the calls inside one body is
	// the strongest ordering information static analysis has, and leaving it
	// unsorted throws it away.
	for id := range seamsBy {
		sort.SliceStable(seamsBy[id], func(i, j int) bool { return seamsBy[id][i].Line < seamsBy[id][j].Line })
	}
	for id := range statesBy {
		sort.SliceStable(statesBy[id], func(i, j int) bool { return statesBy[id][i].Line < statesBy[id][j].Line })
	}

	var out []Path
	for _, entry := range f.Entries {
		id := entry.Pkg + "#" + entry.Symbol
		if _, ok := f.Nodes[id]; !ok {
			continue
		}
		label := entry.Label
		if label == "" {
			label = entry.Symbol
		}
		path := Path{Entry: entry, Label: label}

		// Depth-first, following each call in the order it appears INSIDE the
		// caller's body. Where a function is declared in the file is irrelevant. Node.Calls is already sorted by call-site position, so this
		// walk produces the sequence a person would read off the page:
		//
		//	CreatePayment
		//	  process
		//	    alreadySeen
		//	    Authorize
		//	    Post
		//
		// rather than the sideways sweep breadth-first gives, which puts
		// alreadySeen, Authorize and Post at the same level and loses the fact
		// that all three happen inside the transaction process opened.
		//
		// This is CALL-SITE order, not execution order, and every prompt says so.
		// A branch may skip a call, a loop may repeat one, and `go` runs one
		// concurrently. But source order is what the code says, and it is the
		// only ordering a compiler can honestly offer.
		seen := map[string]bool{}
		var walk func(nodeID string, depth int)
		walk = func(nodeID string, depth int) {
			if seen[nodeID] {
				return // recursion, or a helper reached from two places
			}
			seen[nodeID] = true
			node, ok := f.Nodes[nodeID]
			if !ok {
				return
			}
			path.Steps = append(path.Steps, Step{
				Order:  len(path.Steps) + 1,
				Depth:  depth,
				Ref:    nodeID,
				Symbol: node.Ref.Symbol,
				File:   node.Ref.File,
				Line:   node.Ref.Line,
				Facts:  node.Facts,
				Seams:  seamsBy[nodeID],
				States: statesBy[nodeID],
			})
			for _, callee := range node.Calls {
				walk(callee, depth+1)
			}
		}
		walk(id, 0)
		out = append(out, path)
	}
	return out
}

// Render writes a path as the fact block that goes into a prompt.
func (p Path) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ENTRY POINT: %s\n", p.Label)
	fmt.Fprintf(&b, "  %s#%s\n", p.Entry.Pkg, p.Entry.Symbol)
	b.WriteString("  Nesting shows what happens INSIDE what. Steps are ordered by where each\n")
	b.WriteString("  CALL is written inside its caller, not by where functions are declared in\n")
	b.WriteString("  the file. It is still not the order things RUN: a branch may skip a call, a\n")
	b.WriteString("  loop may repeat one, and `go` runs one alongside the rest.\n\n")
	for _, s := range p.Steps {
		pad := strings.Repeat("  ", s.Depth)
		fmt.Fprintf(&b, "%sstep %d  %s\n", pad, s.Order, s.Symbol)
		fmt.Fprintf(&b, "%s        %s:%d\n", pad, s.File, s.Line)
		if tags := factTags(s.Facts); len(tags) > 0 {
			fmt.Fprintf(&b, "%s        proved: %s\n", pad, strings.Join(tags, ", "))
		}
		// Seams are printed in line order, which is the point of this whole
		// rendering: "the provider call at line 64 happens after BeginTx at 52
		// and before Commit at 76" is exactly the sequence a crash test needs.
		for _, seam := range s.Seams {
			injectable := "NOT injectable (concrete type)"
			if seam.Injectable {
				injectable = "injectable via " + seam.Iface
			}
			fmt.Fprintf(&b, "%s        line %-4d leaves process: %s  [%s]  %s\n",
				pad, seam.Line, seam.Target, seam.Kind, injectable)
		}
		for _, w := range s.States {
			inTx := "OUTSIDE any transaction"
			if w.InTx {
				inTx = "inside a transaction"
			}
			fmt.Fprintf(&b, "%s        line %-4d sets status: %s  %s\n", pad, w.Line, w.To, inTx)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func factTags(f flowEntity.Facts) []string {
	var tags []string
	add := func(cond bool, tag string) {
		if cond {
			tags = append(tags, tag)
		}
	}
	add(f.OpensTx, "opens a transaction")
	add(f.CommitsTx, "commits")
	add(f.RollsBackTx, "rolls back")
	add(f.TouchesDB, "touches the database")
	add(f.TouchesNet, "makes a network call")
	add(f.ReadsClock, "reads the clock")
	add(f.Randomness, "generates randomness or an ID")
	add(f.SpawnsGoroutine, "starts a goroutine")
	add(f.HandlesMoney, "carries a money type")
	if len(f.WritesStatus) > 0 {
		tags = append(tags, "sets status to "+strings.Join(f.WritesStatus, "/"))
	}
	return tags
}

// RenderHeader names the entry point and points at PREAMBLE.md instead of
// reprinting the steps.
//
// Every round-one prompt used to carry the whole step list. On a real service
// that is 34 KB, and with 105 prompts in the pack it was paid for 105 times.
// The steps have not moved: they are in PREAMBLE.md, once, and the agent has
// already read them by the time it opens a question.
func (p Path) RenderHeader() string {
	return fmt.Sprintf("ENTRY POINT: %s\n  %s#%s\n  %d step(s) — the full list is in PREAMBLE.md\n",
		p.Label, p.Entry.Pkg, p.Entry.Symbol, len(p.Steps))
}
