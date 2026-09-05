package prompts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// ExternalEffect asks what one call does to the world, and whether it can be
// taken back.
//
// This question used to be "is a retry of this call safe?". That was wrong twice
// over. It presumed the repository HAS a retry policy, and plenty do not. And it
// asked an agent for a DESIGN decision — should this be retried — when what a
// test needs is an OBSERVATION about the code: does the effect survive a
// rollback, and can the outcome be checked afterwards.
//
// The observation is the durable half. A client pressing refresh is a repeat the
// server never opted into, so the effect of a second call matters whether or not
// anyone wrote a retry loop. Whether the code SHOULD retry is a decision for the
// team, and if they made one, that is a thing to test, not a thing to assume.
//
// The analyser can prove a call LEAVES THE PROCESS. It cannot see the other
// side. So it lists the exits and asks about each one.
func ExternalEffect(f *flowEntity.Flow, target string, seams []flowEntity.Seam, paths []Path) *Prompt {
	// No Goal and no Rule here on purpose.
	//
	// There are twenty-seven of these questions in one pack and the goal and the
	// rules are identical in every one. Attaching them to the prompt copies
	// thirteen hundred characters twenty-seven times — thirty-five kilobytes,
	// most of one question file, to say the same four things over and over. They
	// live in ExternalEffectPreamble instead, read once.
	//
	// This is the same mistake the flow map made, and the rule that comes out of
	// it is worth stating plainly: anything constant across a KIND belongs in
	// PREAMBLE.md, and only what varies per question belongs in the prompt.
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

	return p.Answers("The four questions, the JSON shape and the timeout note are in\nPREAMBLE.md, under \"externalEffect\".").Where("testigo/asks/PREAMBLE.md")
}

// SeamTargets picks the calls where "is a retry free?" is a real question.
//
// Every question costs money, so the filter matters as much as the prompt. The
// rule comes straight from the definition of a foreign mutation: a change a
// database ROLLBACK cannot undo.
//
// That immediately excludes most database traffic. A SELECT changes nothing. An
// INSERT inside a transaction the code controls is undone by rolling back, so
// retrying it is free. BeginTx on its own mutates nothing. Asking an agent
// whether retrying QueryRowContext makes the money move twice is paying for an
// answer everyone already knows.
//
// What survives:
//
//   - anything that leaves the machine — HTTP, gRPC, a broker, a cache. Once it
//     is gone, no rollback reaches it.
//   - COMMIT, because it is the exact line after which rollback stops working.
//   - every call through an interface the repository defines itself. Those are
//     genuinely unknown: `Ledger.Post` might write a row, or might call a
//     provider. That is the question worth paying for.
func SeamTargets(f *flowEntity.Flow) []string {
	seen := map[string]bool{}
	for _, s := range f.Seams {
		switch {
		case s.Kind == flowEntity.SeamClock, s.Kind == flowEntity.SeamRandom:
			// Their interesting property is testability, not retry safety.
			continue
		case s.Injectable:
			// The repository's own abstraction. We cannot see through it.
			seen[s.Target] = true
		case s.Kind == flowEntity.SeamDB:
			if isCommit(s.Target) {
				seen[s.Target] = true
			}
		default:
			// Leaves the machine.
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
