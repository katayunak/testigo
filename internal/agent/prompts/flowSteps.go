package prompts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

type Step struct {
	Order int

	Depth  int
	Ref    string
	Symbol string
	File   string
	Line   int
	Facts  flowEntity.Facts
	Seams  []flowEntity.Seam
	States []flowEntity.StateWrite
}

type Path struct {
	Entry flowEntity.EntryPoint
	Label string
	Steps []Step
}

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

		seen := map[string]bool{}
		var walk func(nodeID string, depth int)
		walk = func(nodeID string, depth int) {
			if seen[nodeID] {
				return
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

func (p Path) RenderHeader() string {
	return fmt.Sprintf("ENTRY POINT: %s\n  %s#%s\n  %d step(s) — the full list is in PREAMBLE.md\n",
		p.Label, p.Entry.Pkg, p.Entry.Symbol, len(p.Steps))
}
