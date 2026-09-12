package prompts

import (
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func UnderstandPreamble(f *flowEntity.Flow, paths []Path) string {
	var b strings.Builder

	b.WriteString(`# testigo — the flow, once

Read this file first. Every prompt in this pack refers to it by step number
rather than repeating it, so nothing here is duplicated in the questions.

## What was already proved

Everything below came from the Go type checker and SSA. It is fact, not a
reading, and you must not contradict it. If your reading of the source disagrees
with a line here, you have misread the source.

What is NOT here is why any of it exists. That is what the questions ask for.
`)

	b.WriteString(Migrations(f))

	b.WriteString(`
## The flow

`)

	for _, p := range paths {
		b.WriteString(indent(p.Render(), "  "))
		b.WriteString("\n")
	}

	b.WriteString(`## How to read a step

  step 4  (*Server).process
          api/server.go:35
          proved: opens a transaction, commits, makes a network call
          line 58   leaves process: (psp.Gateway).Authorize  [http]  injectable
          line 44   sets status: StatusPending  inside a transaction

Indentation is nesting: a provider call INSIDE the function that opened the
transaction is a different situation from one beside it. The order is where each
CALL is written inside its caller — not where functions are declared, and not
the order things RUN, because a branch may skip a call and a loop may repeat one.

` + "`injectable`" + ` means the call goes through an interface, so a test can substitute
it and make it fail. ` + "`NOT injectable`" + ` means it is a concrete type and no test can
make it fail on demand — which usually matters more than it sounds, because most
serious payment bugs only appear when something downstream fails.

## externalEffect — one call that leaves the process

A static analyser found a call that leaves this process. It can prove where the
call is made and whether a test could substitute it. It cannot see what happens
on the other side, and that is the only thing that decides whether doing it twice
is harmless or moves money twice.

You are NOT being asked whether this should be retried. That is a decision for
the team who owns this code, and some of them have deliberately decided not to
retry anything. You are being asked what is TRUE about the call.

### Rules for every externalEffect answer

1. Answer about THIS call only. Other calls get their own task.
2. Read the implementation if it is in this repository. If it is a third-party
   client, reason from its documented behaviour and say which you did in ` + "`basis`" + `.
3. Answer "unknown" when you are not sure. Every default leans the same way:
   unknown is treated as irreversible and unobservable. Assuming an effect can
   be taken back when it cannot means a missing test and money moved twice; the
   reverse costs one unnecessary test.
4. Do not guess from the name. ` + "`Notify`" + ` sounds harmless and may settle a payment.

## The four questions that matter

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

Five verdicts and nothing else.

` + "```" + `
{
  "changes_external_state": true | false | "unknown",
  "reversible_by_rollback": true | false | "unknown",
  "outcome_observable":     true | false | "unknown",
  "accepts_dedup_key":      true | false | "unknown",
  "moves_money":            true | false | "unknown"
}
` + "```" + `

` + "`\"unknown\"`" + ` is read as the unsafe answer: not reversible, not observable, not
deduplicated. Send no prose — no notes, no failure list, no basis. A sentence
costs five times what a verdict does and nothing reads it.

On ` + "`moves_money`" + `: use THIS repository's meaning, which is not always a
transfer between accounts. In a service-activation system the money moves when a
status is reported to a settlement provider. In a wallet, it moves when the
balance row changes. If ` + "`testigo/rules.json`" + ` defines it, follow that.

A call that times out has an UNKNOWN outcome, not a failed one — the far side may
have processed it. That is what ` + "`outcome_observable`" + ` is for: answer it false if
this call can time out and nothing can be asked afterwards. That combination is
the most expensive shape in payments and it is invisible to every test that only
exercises clean success and clean failure.

`)
	b.WriteString(SharedRules())
	b.WriteString(StateRolesPreamble)
	b.WriteString(QuestionsPreamble)

	return b.String()
}
