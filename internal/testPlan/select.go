package testPlan

import (
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
)

// Select turns the catalog into the list of cases worth generating for one flow.
//
// This is where the token saving happens, and it happens before a single token
// is spent. Twenty-two scenarios live in the catalog; a repository with no state
// machine, one entry point and concrete seams might match four. The other
// eighteen are never rendered, never sent, and never paid for.
//
// Blocked cases are kept rather than dropped. A repository where nothing can be
// fault-injected should be told that in the same list as the tests it did get,
// because "your seams are concrete so eleven of these are impossible" is more
// useful than a short list with no explanation.
func Select(f *flowEntity.Flow, b planEntity.Facts) []planEntity.TestCase {
	seamsByKind := map[flowEntity.SeamKind][]flowEntity.Seam{}
	var injectable []flowEntity.Seam
	for _, s := range f.Seams {
		seamsByKind[s.Kind] = append(seamsByKind[s.Kind], s)
		if s.Injectable {
			injectable = append(injectable, s)
		}
	}

	reachable := reachableFrom(f)

	var cases []planEntity.TestCase
	for _, sc := range Catalog {
		c := planEntity.TestCase{
			Scenario: sc,
			Role:     planEntity.RoleSpecification,
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
			c.Size = planEntity.SizeMedium
		}
		c.Scope = scopeFor(technique)

		// The seams this case actually needs: the right kinds, reachable from
		// the entry point it is about, and one entry per distinct target.
		//
		// Over-including here is the most expensive habit available. The facts
		// block is the largest part of a prompt, and pasting every seam in the
		// repository into all nineteen cases multiplies the biggest block by
		// nineteen while making each one harder to read.
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
		if sc.Requires.StateMachine && len(f.States) > 0 {
			m := f.States[0]
			c.States = &m
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

func scopeFor(t planEntity.Technique) planEntity.Scope {
	switch t {
	case planEntity.TechniqueEndToEnd:
		return planEntity.ScopeSystem
	case planEntity.TechniqueNarrowIntegration, planEntity.TechniqueConcurrency:
		return planEntity.ScopeService
	case planEntity.TechniqueFuzz, planEntity.TechniqueTable:
		return planEntity.ScopeFunction
	}
	return planEntity.ScopeUnit
}

// funcName turns a scenario ID into a Go test name that reads as a sentence.
func funcName(s planEntity.Scenario) string {
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

// target picks where the generated file goes.
//
// It lands in the entry point's package, because that is where the code under
// test lives and where a Go test needs to be to reach anything unexported. The
// testigo suffix makes generated files obvious in a directory listing and
// trivial to delete as a group.
func target(f *flowEntity.Flow, s planEntity.Scenario, size planEntity.Size) (pkg, file string) {
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
	if size != planEntity.SizeSmall {
		suffix = "_testigo_integration_test.go"
	}
	return pkgPath, dir + "/" + strings.ToLower(string(s.Family)) + suffix
}

// reachableFrom returns the nodes the FIRST entry point can reach.
//
// First rather than all, because a scenario is about one path. A webhook has its
// own idempotency question with its own answer, and merging both paths into one
// prompt produces a test that tries to be about both and is about neither.
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

// scopeSeams keeps one seam per distinct target, reachable from the entry point.
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
