package testPlan

import (
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func Select(f *flowEntity.Flow, b Facts) []TestCase {
	seamsByKind := map[flowEntity.SeamKind][]flowEntity.Seam{}
	var injectable []flowEntity.Seam
	for _, s := range f.Seams {
		seamsByKind[s.Kind] = append(seamsByKind[s.Kind], s)
		if s.Injectable {
			injectable = append(injectable, s)
		}
	}

	reachable := reachableFrom(f)

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
		c.Technique = technique
		props := technique.Props()
		c.Size = props.DefaultSize
		if sc.Requires.RealDatabase {
			c.Size = SizeMedium
		}

		var candidates []flowEntity.Seam
		if len(sc.Requires.SeamKinds) > 0 {
			for _, k := range sc.Requires.SeamKinds {
				candidates = append(candidates, seamsByKind[k]...)
			}
		} else if sc.Requires.InjectableSeam {
			candidates = injectable
		}
		if len(f.Entries) > 0 {
			e := f.Entries[0]
			c.Entry = &e
		}
		c.Seams = scopeSeams(candidates, reachable)
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

		c.TargetPkg, c.TargetFile = target(f, sc, c.Size)
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

func severityRank(s flowEntity.Severity) int {
	switch s {
	case flowEntity.SevCritical:
		return 0
	case flowEntity.SevHigh:
		return 1
	case flowEntity.SevMedium:
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

func target(f *flowEntity.Flow, s Scenario, size Size) (pkg, file string) {
	dir := "."
	pkgPath := ""
	if len(f.Entries) > 0 {
		pkgPath = f.Entries[0].Pkg
		if n, ok := f.Nodes[f.Entries[0].Pkg+"#"+f.Entries[0].Symbol]; ok {
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

func bestStateMachine(f *flowEntity.Flow, roles map[string]flowEntity.StateRoles) *flowEntity.StateMachine {
	var best *flowEntity.StateMachine
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

func lifecycleScore(rs flowEntity.StateRoles) int {
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

func reachableFrom(f *flowEntity.Flow) map[string]bool {
	seen := map[string]bool{}
	if len(f.Entries) == 0 {
		for id := range f.Nodes {
			seen[id] = true
		}
		return seen
	}
	start := f.Entries[0].Pkg + "#" + f.Entries[0].Symbol
	queue := []string{start}
	seen[start] = true
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

func scopeSeams(in []flowEntity.Seam, reachable map[string]bool) []flowEntity.Seam {
	seen := map[string]bool{}
	var out []flowEntity.Seam
	for _, s := range in {
		if !reachable[s.In.ID()] || seen[s.Target] {
			continue
		}
		seen[s.Target] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}
