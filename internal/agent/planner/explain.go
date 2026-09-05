package planner

import (
	"fmt"
	"sort"
	"strings"
)

func (p Plan) Explain() string {
	var b strings.Builder

	fmt.Fprintf(&b, "ask plan   %d scenario(s) runnable, %d blocked\n", p.Demand.Runnable, p.Demand.Blocked)
	if p.Budget > 0 {
		fmt.Fprintf(&b, "budget     %s weighted (output counted %dx)\n", short(p.Budget), OutputMultiplier)
	} else {
		fmt.Fprintf(&b, "budget     none; output counted %dx\n", OutputMultiplier)
	}
	b.WriteString("\n")

	if chosen := p.Chosen(); len(chosen) > 0 {
		b.WriteString("ASKED\n")
		for _, g := range regroup(chosen) {
			label := g.kind
			if g.n > 1 {
				label = fmt.Sprintf("%s x%d", g.kind, g.n)
			}
			fmt.Fprintf(&b, "  %-24s %-12s %s\n", label, g.method.Human(), g.cost)
			if g.why != "" {
				fmt.Fprintf(&b, "  %-24s   %s\n", "", g.why)
			}
		}
		b.WriteString("\n")
	}

	type group struct {
		kind, why string
		n         int
		cost      Cost
	}
	groups := map[string]*group{}
	var order []string
	for _, c := range p.Choices {
		if c.Chosen {
			continue
		}
		key := c.Kind + "|" + c.Why
		g, ok := groups[key]
		if !ok {
			g = &group{kind: c.Kind, why: c.Why}
			groups[key] = g
			order = append(order, key)
		}
		g.n++
		g.cost = g.cost.Add(c.asked())
	}
	if len(order) > 0 {
		sort.Slice(order, func(i, j int) bool {
			return groups[order[i]].cost.Weighted() > groups[order[j]].cost.Weighted()
		})
		b.WriteString("NOT ASKED\n")
		for _, k := range order {
			g := groups[k]
			label := g.kind
			if g.n > 1 {
				label = fmt.Sprintf("%s x%d", g.kind, g.n)
			}
			fmt.Fprintf(&b, "  %-24s %-46s saves %s\n", label, trim(g.why, 46), short(g.cost.Weighted()))
		}
		b.WriteString("\n")
	}

	var trimTotal Cost
	byKind := map[string]Cost{}
	var trimOrder []string
	for _, c := range p.Choices {
		if !c.Chosen || c.Trimmable.Weighted() == 0 {
			continue
		}
		if _, seen := byKind[c.Kind]; !seen {
			trimOrder = append(trimOrder, c.Kind)
		}
		byKind[c.Kind] = byKind[c.Kind].Add(c.Trimmable)
		trimTotal = trimTotal.Add(c.Trimmable)
	}
	if len(trimOrder) > 0 {
		b.WriteString("STILL PAID FOR, NOTHING READS IT\n")
		sort.Slice(trimOrder, func(i, j int) bool {
			return byKind[trimOrder[i]].Weighted() > byKind[trimOrder[j]].Weighted()
		})
		for _, k := range trimOrder {
			var unread []string
			for _, f := range FieldsOf(k) {
				if !f.Use.Worth() {
					unread = append(unread, f.Path[len(k)+1:])
				}
			}
			fmt.Fprintf(&b, "  %-24s %-46s %s\n", k, trim(strings.Join(unread, ", "), 46), short(byKind[k].Weighted()))
		}
		b.WriteString("  drop these fields from the prompt to collect it\n\n")
	}

	t, a := p.Total(), p.Avoided()
	fmt.Fprintf(&b, "cost       %s\n", t)
	fmt.Fprintf(&b, "avoided    %s\n", a)
	if trimTotal.Weighted() > 0 {
		fmt.Fprintf(&b, "trimmable  %s more, by asking only for fields that are read\n", short(trimTotal.Weighted()))
	}
	if w := t.Weighted() + a.Weighted(); w > 0 {
		fmt.Fprintf(&b, "saving     %.0f%% of the unplanned cost\n", float64(a.Weighted())/float64(w)*100)
	}
	return b.String()
}

type kindGroup struct {
	kind   string
	why    string
	method Method
	n      int
	cost   Cost
}

func regroup(cs []Choice) []kindGroup {
	byKind := map[string]*kindGroup{}
	var order []string
	for _, c := range cs {
		g, ok := byKind[c.Kind]
		if !ok {
			g = &kindGroup{kind: c.Kind, why: c.Why, method: c.Method}
			byKind[c.Kind] = g
			order = append(order, c.Kind)
		}
		g.n++
		g.cost = g.cost.Add(c.Cost)
	}
	sort.Slice(order, func(i, j int) bool {
		return byKind[order[i]].cost.Weighted() > byKind[order[j]].cost.Weighted()
	})
	out := make([]kindGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *byKind[k])
	}
	return out
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func clip(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	out := append([]string{}, in[:n]...)
	return append(out, fmt.Sprintf("and %d more", len(in)-n))
}
