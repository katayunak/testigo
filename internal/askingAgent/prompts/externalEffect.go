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
func ExternalEffect(f *flowEntity.Flow, target string, seams []flowEntity.Seam, paths []Path) string {
	var b strings.Builder

	b.WriteString(`# testigo — what does this call do to the world?

A static analyser found a call that leaves this process. It can prove where the
call is made and whether a test could substitute it. It cannot see what happens
on the other side, and that is the only thing that decides whether doing it twice
is harmless or moves money twice.

You are NOT being asked whether this should be retried. That is a decision for
the team who owns this code, and some of them have deliberately decided not to
retry anything. You are being asked what is TRUE about the call.

## Rules

1. Answer about THIS call only. Other calls get their own task.
2. Read the implementation if it is in this repository. If it is a third-party
   client, reason from its documented behaviour and say which you did in ` + "`basis`" + `.
3. Answer "unknown" when you are not sure. Every default leans the same way:
   unknown is treated as irreversible and unobservable. Assuming an effect can
   be taken back when it cannot means a missing test and money moved twice; the
   reverse costs one unnecessary test.
4. Do not guess from the name. ` + "`Notify`" + ` sounds harmless and may settle a payment.

## The call

`)
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

	b.WriteString("## Where it sits in the flow\n\n")
	for _, p := range paths {
		b.WriteString(indent(p.Render(), "  "))
	}

	b.WriteString(`## The four questions that matter

**1. Does it change state outside this process?**
A read does not. A write to another system does. A message published to a broker
does, the moment it is delivered.

**2. Does a database ROLLBACK undo it?**
For anything that left the machine the answer is no, and that is the point. This
is the property that makes a crash between two steps dangerous.

**3. Can the outcome be checked afterwards?**
This is the question people forget, and it changes everything. If the provider
has a status endpoint, a timeout is recoverable: ask, then decide. If it does
not, a timeout is a permanent unknown, and the only safe design is to make the
call deduplicating before you make it at all.

**4. Does the far side deduplicate on a key you send?**
Passing a key is different from being naturally safe to repeat. It breaks the
moment somebody regenerates the key on the second attempt.

## Output

Reply with one JSON object and nothing else.

` + "```" + `
{
  "changes_external_state": true | false | "unknown",
  "reversible_by_rollback": true | false | "unknown",
  "outcome_observable":     true | false | "unknown",
  "accepts_dedup_key":      true | false | "unknown",
  "dedup_key_argument": "the parameter carrying the key, or null",
  "moves_money": true | false | "unknown",
  "undo": { "exists": false, "symbol": null, "evidence": "" },
  "failure_modes": ["timeout", "5xx", "connection_reset", "duplicate_response"],
  "basis": "read_implementation" | "vendor_documentation" | "inference",
  "notes": ""
}
` + "```" + `

On ` + "`moves_money`" + `: use THIS repository's meaning, which is not always a
transfer between accounts. In a service-activation system the money moves when a
status is reported to a settlement provider. In a wallet, it moves when the
balance row changes. If ` + "`testigo.rules.json`" + ` defines it, follow that; otherwise say
what you observed and explain your reading in ` + "`notes`" + `.

The timeout case deserves its own thought. A call that times out has an
UNKNOWN outcome, not a failed one. The far side may have processed it. If this call can
time out, and question 3 is false, say so plainly in ` + "`notes`" + `. That combination is
the most expensive shape in payments and it is invisible to every test that only
exercises clean success and clean failure.
`)
	return b.String()
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
