package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/models"
)

func Flowchart(f *models.Flow) string {
	var b strings.Builder
	b.WriteString("flowchart TD\n")

	ids := make([]string, 0, len(f.Nodes))
	for id := range f.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	seamsBy := map[string][]models.Seam{}
	for _, s := range f.Seams {
		seamsBy[s.In.ID()] = append(seamsBy[s.In.ID()], s)
	}

	for _, id := range ids {
		n := f.Nodes[id]
		label := n.Ref.Symbol
		if n.Notes != nil && n.Notes.Step != "" {
			label = n.Notes.Step
		}
		var tags []string
		if n.Facts.HandlesMoney {
			tags = append(tags, "money")
		}
		if n.Facts.OpensTx {
			tags = append(tags, "tx")
		}
		if n.Facts.TouchesNet {
			tags = append(tags, "net")
		}
		if n.Facts.SpawnsGoroutine {
			tags = append(tags, "async")
		}
		if len(tags) > 0 {
			label += "<br/><small>" + strings.Join(tags, " · ") + "</small>"
		}
		shape := "[\"%s\"]"
		switch {
		case n.Kind == models.NodePositionEntry:
			shape = "([\"%s\"])"
		case n.Facts.TouchesDB:
			shape = "[(\"%s\")]"
		}
		fmt.Fprintf(&b, "  %s"+shape+"\n", mid(id), label)
	}

	for _, id := range ids {
		for _, to := range f.Nodes[id].Calls {
			if _, ok := f.Nodes[to]; ok {
				fmt.Fprintf(&b, "  %s --> %s\n", mid(id), mid(to))
			}
		}
	}

	// Uninjectable seams are drawn as dashed edges to a distinct node so the
	// untestable boundaries are visible at a glance rather than buried in a
	// findings table.
	drawn := map[string]bool{}
	for _, id := range ids {
		for _, s := range seamsBy[id] {
			if s.Injectable {
				continue
			}
			t := "SEAM_" + mid(s.Target)
			if !drawn[t] {
				fmt.Fprintf(&b, "  %s{{\"%s<br/><small>not injectable</small>\"}}\n", t, escape(s.Target))
				drawn[t] = true
			}
			fmt.Fprintf(&b, "  %s -.-> %s\n", mid(id), t)
		}
	}

	b.WriteString("  classDef entry stroke-width:2px\n")
	for _, id := range ids {
		if f.Nodes[id].Kind == models.NodePositionEntry {
			fmt.Fprintf(&b, "  class %s entry\n", mid(id))
		}
	}
	return b.String()
}

// StateDiagram renders the statically recovered state machine.
//
// Only transitions the compiler can prove are drawn. Which of them SHOULD be
// possible is a business rule, and the diagram deliberately does not guess: a
// confident wrong arrow is worse than a missing one.
func StateDiagram(m models.StateMachine) string {
	var b strings.Builder
	b.WriteString("stateDiagram-v2\n")
	written := map[string]bool{}
	for _, s := range m.States {
		fmt.Fprintf(&b, "  %s\n", s)
	}
	for _, w := range m.Writes {
		if w.To == "<dynamic>" || written[w.To+w.In.Symbol] {
			continue
		}
		written[w.To+w.In.Symbol] = true
		fmt.Fprintf(&b, "  [*] --> %s : %s\n", w.To, w.In.Symbol)
	}
	return b.String()
}

// mid makes a Mermaid-safe node id. Full import paths produce ids so long the
// diagram source is unreadable in a code review, which defeats the point of
// choosing a text format, so only the last path element is kept.
func mid(s string) string {
	if pkg, sym, ok := strings.Cut(s, "#"); ok {
		if i := strings.LastIndex(pkg, "/"); i >= 0 {
			pkg = pkg[i+1:]
		}
		s = pkg + "_" + sym
	}
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func escape(s string) string {
	return strings.NewReplacer(`"`, `&quot;`, "<", "&lt;", ">", "&gt;").Replace(s)
}
