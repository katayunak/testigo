package prompts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func ExternalEffect(f *flowEntity.Flow, target string, seams []flowEntity.Seam, paths []Path) *Prompt {

	p := New("testigo — what does this call do to the world?")

	var b strings.Builder
	fmt.Fprintf(&b, "Target: %s\n\n", target)
	b.WriteString("Called from:\n\n")
	for _, s := range seams {
		injectable := "NOT injectable — it is called on a concrete type, so no test can\n            make it fail, time out, or return a duplicate"
		if s.Injectable {
			injectable = "injectable through the interface " + s.Iface + "\n            — a test can substitute a failing or slow implementation"
		}
		fmt.Fprintf(&b, "  %s\n    %s:%d\n    kind: %s\n    %s\n\n",
			s.In.Symbol, s.In.File, s.Line, s.Kind, injectable)
	}

	p.Fact(Proof, "The call", b.String())

	var flow strings.Builder
	for _, path := range paths {
		flow.WriteString(indent(path.RenderHeader(), "  "))
	}
	p.Fact(Subject, "Where it sits in the flow", flow.String())

	return p.Answers("Five verdicts. The questions and the JSON shape are in PREAMBLE.md,\nunder \"externalEffect\". Send no prose.").Where("testigo/asks/PREAMBLE.md")
}

func ExternalEffectVerify(f *flowEntity.Flow, target string, seams []flowEntity.Seam, paths []Path) *Prompt {
	return ExternalEffect(f, target, seams, paths)
}

func SeamTargets(f *flowEntity.Flow) []string {
	seen := map[string]bool{}
	for _, s := range f.Seams {
		switch {
		case s.Kind == flowEntity.SeamClock, s.Kind == flowEntity.SeamRandom:

			continue
		case s.Injectable:

			seen[s.Target] = true
		case s.Kind == flowEntity.SeamDB:
			if isCommit(s.Target) {
				seen[s.Target] = true
			}
		default:

			seen[s.Target] = true
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func isCommit(target string) bool {
	name := target
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name == "Commit"
}
