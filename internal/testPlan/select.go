package testPlan

import (
	"slices"
	"sort"
	"strings"

	"github.com/katayunak/gofacts/fact"
)

func Select(f *fact.Flow, b Facts) []TestCase {
	seamsByKind := map[fact.SeamKind][]fact.Seam{}
	var injectable []fact.Seam
	for _, s := range f.Seams {
		seamsByKind[s.Kind] = append(seamsByKind[s.Kind], s)
		if s.Injectable {
			injectable = append(injectable, s)
		}
	}

	reachByEntry := reachableFromEachEntry(f)
	reachInEntryOrder := reachSetsInEntryOrder(f, reachByEntry)

	var cases []TestCase
	for _, sc := range Catalog {
		c := TestCase{
			Scenario: sc,
			FuncName: funcName(sc),
		}

		if ok, why := sc.Applies(f, b); !ok {
			c.Blocked = why
			cases = append(cases, c)
			continue
		}

		technique, why := sc.BestTechnique(f)
		if technique == "" {
			c.Blocked = why
			cases = append(cases, c)
			continue
		}
		var candidates []fact.Seam
		if len(sc.Requires.SeamKinds) > 0 {
			for _, k := range sc.Requires.SeamKinds {
				for _, s := range seamsByKind[k] {
					if s.Injectable || !sc.Requires.InjectableSeam {
						candidates = append(candidates, s)
					}
				}
			}
		} else if sc.Requires.InjectableSeam {
			candidates = injectable
		}
		seams := scopeSeams(candidates, reachInEntryOrder)
		if len(seams) == 0 && sc.Requires.needsASeam() {
			c.Blocked = noReachableSeam(sc.Requires)
			cases = append(cases, c)
			continue
		}

		keyRaceNeedsTheRealConstraint := sc.keyRaceNeedsTheRealConstraint(b)
		if keyRaceNeedsTheRealConstraint {
			seams = slices.DeleteFunc(seams, isDatabaseSeam)
		}
		c.Technique = technique
		props := technique.Props()
		c.Size = props.DefaultSize
		if sc.Requires.RealDatabase || keyRaceNeedsTheRealConstraint {
			c.Size = SizeMedium
		}

		c.Seams = seams
		c.Entries = entriesUnderTest(reachByEntry, c.Seams)
		if sc.Requires.StateMachine {
			c.States = bestStateMachine(f, b.StateMachines)
		}
		if sc.Requires.MultiTenant {
			if scheme, ok := f.Infra.TenantScheme(); ok {
				c.Tenancy = &scheme
			}
		}
		if sc.Requires.HashChain && len(f.HashChains) > 0 {
			chain := f.HashChains[0]
			c.HashChain = &chain
		}

		c.TargetPkg, c.TargetFile = target(f, c.Entries, sc, c.Size)
		cases = append(cases, c)
	}

	sort.SliceStable(cases, func(i, j int) bool {
		if cases[i].Runnable() != cases[j].Runnable() {
			return cases[i].Runnable()
		}
		return severityRank(cases[i].Scenario.Severity) < severityRank(cases[j].Scenario.Severity)
	})
	return cases
}

func noReachableSeam(r Requires) string {
	boundary := "boundary"
	if len(r.SeamKinds) > 0 {
		boundary = kindList(r.SeamKinds) + " " + boundary
	}
	if r.InjectableSeam {
		boundary = "injectable " + boundary
	}
	return "no " + boundary + " is reached from any entry point, so a test started from one can never get there"
}

func isDatabaseSeam(s fact.Seam) bool { return s.Kind == fact.SeamDB }

func severityRank(s fact.Severity) int {
	switch s {
	case fact.SevCritical:
		return 0
	case fact.SevHigh:
		return 1
	case fact.SevMedium:
		return 2
	}
	return 3
}

func funcName(s Scenario) string {
	parts := strings.Split(strings.ToLower(s.ID), "-")
	var b strings.Builder
	b.WriteString("Test")
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
}

func target(f *fact.Flow, entries []fact.EntryPoint, s Scenario, size Size) (pkg, file string) {
	dir := "."
	pkgPath := ""
	if len(entries) > 0 {
		pkgPath = entries[0].Pkg
		if n, ok := f.Nodes[entries[0].ID()]; ok {
			if i := strings.LastIndex(n.Ref.File, "/"); i >= 0 {
				dir = n.Ref.File[:i]
			}
		}
	}
	suffix := "_testigo_test.go"
	if size != SizeSmall {
		suffix = "_testigo_integration_test.go"
	}
	return pkgPath, dir + "/" + strings.ToLower(string(s.Family)) + suffix
}

func bestStateMachine(f *fact.Flow, roles map[string]fact.StateRoles) *fact.StateMachine {
	var best *fact.StateMachine
	bestScore := -1
	for i := range f.States {
		m := &f.States[i]
		if len(m.States) < 2 {
			continue
		}
		if score := lifecycleScore(roles[m.Type]); score > bestScore {
			bestScore = score
			best = m
		}
	}
	return best
}

func lifecycleScore(rs fact.StateRoles) int {
	if len(rs) == 0 {
		return 0
	}
	score := 1
	if _, ok := rs.Initial(); ok {
		score++
	}
	if len(rs.Finals()) > 0 {
		score++
	}
	return score
}

type entryReach struct {
	entry fact.EntryPoint
	nodes map[string]bool
}

func reachableFromEachEntry(f *fact.Flow) []entryReach {
	out := make([]entryReach, 0, len(f.Entries))
	for _, e := range f.Entries {
		out = append(out, entryReach{entry: e, nodes: walkCalls(f, e.ID())})
	}
	return out
}

func reachSetsInEntryOrder(f *fact.Flow, reachByEntry []entryReach) []map[string]bool {
	if len(reachByEntry) == 0 {
		everyNode := map[string]bool{}
		for id := range f.Nodes {
			everyNode[id] = true
		}
		return []map[string]bool{everyNode}
	}
	out := make([]map[string]bool, 0, len(reachByEntry))
	for _, r := range reachByEntry {
		out = append(out, r.nodes)
	}
	return out
}

func walkCalls(f *fact.Flow, start string) map[string]bool {
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		node, ok := f.Nodes[cur]
		if !ok {
			continue
		}
		for _, callee := range node.Calls {
			if !seen[callee] {
				seen[callee] = true
				queue = append(queue, callee)
			}
		}
	}
	return seen
}

func entriesUnderTest(reachByEntry []entryReach, seams []fact.Seam) []fact.EntryPoint {
	if len(reachByEntry) == 0 {
		return nil
	}
	if len(seams) == 0 {
		return []fact.EntryPoint{reachByEntry[0].entry}
	}
	var out []fact.EntryPoint
	for _, r := range reachByEntry {
		for _, s := range seams {
			if r.nodes[s.In.ID()] {
				out = append(out, r.entry)
				break
			}
		}
	}
	return out
}

func scopeSeams(in []fact.Seam, reachInEntryOrder []map[string]bool) []fact.Seam {
	seen := map[string]bool{}
	var out []fact.Seam
	for _, reachable := range reachInEntryOrder {
		for _, s := range in {
			if !reachable[s.In.ID()] || seen[s.Target] {
				continue
			}
			seen[s.Target] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}
