package prompts

import "strings"

// UnderstandPreamble is everything round one repeats, written once.
//
// The map of the flow used to be pasted into every prompt. Measured on a real
// 73-function payment service that was 34 KB per prompt, 94% of each one, and
// two prompts compared byte for byte came out 99% identical. The whole round-one
// pack reached a million tokens to ask about 73 functions.
//
// The shape was the real problem. The map grows with the function count, and
// there is roughly one prompt per function, so pasting it made the pack
// QUADRATIC: eight times the functions cost seventy-three times the tokens. A
// service twice the size of that one would not cost twice as much.
//
// Hoisting it makes the pack linear again. The agent reads the map once and
// every prompt refers to it by step number.
func UnderstandPreamble(paths []Path) string {
	var b strings.Builder

	b.WriteString(`# testigo — the flow, once

Read this file first. Every prompt in this pack refers to it by step number
rather than repeating it, so nothing here is duplicated in the questions.

## What was already proved

Everything below came from the Go type checker and SSA. It is fact, not a
reading, and you must not contradict it. If your reading of the source disagrees
with a line here, you have misread the source.

What is NOT here is why any of it exists. That is what the questions ask for.

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

## notes — describing one step

## Rules

1. Business terms, not mechanics. "Reserves the funds and records the hold" is
   useful. "Calls ExecContext with an INSERT statement" is not — the analyser
   already knows that and it is printed below.
2. Two sentences at most for ` + "`" + ` + "` + "`" + `purpose` + "`" + `" + ` + "`" + `. If it needs more, the function is doing
   more than one thing, and saying THAT is the useful note.
3. ` + "`" + ` + "` + "`" + `effects` + "`" + `" + ` + "`" + ` lists only what survives the function returning: rows written,
   messages published, money moved, provider state changed. Not local variables,
   not logging.
4. ` + "`" + ` + "` + "`" + `assumptions` + "`" + `" + ` + "`" + ` lists what this function needs its CALLER to have already
   guaranteed — validated input, an open transaction, an acquired lock, a
   checked balance. This is the field that finds bugs, because an assumption
   nobody enforces is a bug waiting for an unusual call order.
5. Describe what the code DOES, not what it should do. Bugs are reported
   elsewhere. A note that quietly describes intended behaviour hides the defect.

### Output for a notes question

` + "`" + `` + "`" + `` + "`" + `
{
  "step": "reserve funds",
  "purpose": "One or two sentences.",
  "effects": ["writes a pending payment row", "debits the payer ledger account"],
  "assumptions": ["caller has validated the amount is positive",
                  "caller holds an open transaction"],
  "confidence": "high" | "medium" | "low"
}
` + "`" + `` + "`" + `` + "`" + `

Use ` + "`" + `confidence: low` + "`" + ` freely. A low-confidence note is still useful — it tells a
person which parts of the map to check first — whereas a confident wrong note is
worse than none at all.


## externalEffect — one call that leaves the process

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

## transitions — one state machine

Note on ordering: the step numbers above are a breadth-first walk of the call
graph, not execution order. A compiler cannot know which branch runs. Use them
to see WHAT is reachable, not in what sequence.

## Output

Reply with one JSON object and nothing else.

` + "```" + `
{
  "initial_state": "StatusPending",
  "may_move_to": {
    "StatusPending":    ["StatusAuthorized", "StatusFailed"],
    "StatusAuthorized": ["StatusCaptured", "StatusFailed"],
    "StatusCaptured":   ["StatusRefunded"],
    "StatusFailed":     [],
    "StatusRefunded":   [],
    "StatusAbandoned":  []
  },
  "unsure": [
    { "from": "StatusFailed", "to": "StatusPending",
      "why": "retry may be intended to reuse the row rather than create a new payment" }
  ],
  "never_assigned_verdict": {
    "StatusRefunded":  "reachable_from_outside",
    "StatusAbandoned": "dead"
  },
  "final_states": ["StatusRefunded", "StatusFailed"],
  "final_state_exceptions": [
    { "from": "StatusCaptured", "to": "StatusRefunded",
      "why": "a chargeback can arrive up to 40 days after capture" }
  ],
  "notes": "anything a test author needs that the shape above cannot carry"
}
` + "```" + `

### On final states

A state is final when a payment in it will never legitimately change again. This
matters more than it sounds, because it produces one of the strongest tests
available: drive a payment into a final state, attempt every other transition,
and assert every one is refused.

That test is only correct if the final list is correct, which is why the
exceptions are asked for in the same breath. Real payment systems are full of
states that look final and are not:

- **captured** is finished, until a chargeback arrives weeks later
- **settled** is finished, until the transfer is recalled
- **refunded** is finished, until the refund itself is reversed
- **failed** is often NOT final at all — see below

A state you list as final with no exception becomes a hard assertion. If reality
can leave it, that test will fail the first time reality happens, and someone
will delete it instead of fixing the code. So put the exception in the list, with
the reason, and the generated test will allow that one path and forbid the rest.

Two transitions are worth extra thought before you answer, because published
payment state machines get both wrong more often than any others:

- **Is failure terminal?** Stripe's PaymentIntent returns to
  ` + "`requires_payment_method`" + ` after a failed payment so it can be retried.
  Implementations that treat failure as terminal look correct until a customer
  retries a declined card.
- **Is the terminal state really terminal?** A captured payment is done, but
  money can still leave through a refund, a chargeback, or a reversal. If this
  repository models any of those, the "terminal" state has outgoing edges.

`)

	return b.String()
}
