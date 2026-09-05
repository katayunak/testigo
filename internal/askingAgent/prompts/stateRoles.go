package prompts

import (
	"fmt"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// StateRoles asks what part each state plays, instead of asking which of N×N
// transitions are legal.
//
// The old question asked an agent to fill in a matrix. Nine states is
// eighty-one cells, every cell a separate chance to be wrong, and the answer
// costs output tokens — which bill at five times input. This asks for one
// classification per state, and testigo derives the matrix in Go.
//
// It is not only cheaper, it is more accurate, and the proof is a real run.
// An agent given the matrix question about a recharge service produced a
// correct answer and buried two facts in its notes that the shape could not
// hold: that ENABLE and DISABLE belong to providers rather than orders, and
// that FAILEDSIMTYPE is never stored at all. Both are roles here, so both
// become structured answers a validator can check rather than prose nobody
// reads.
//
// maxWriteSites is how many examples per state the question carries. Three is
// enough to see what kind of code sets a state; the hundredth is noise a reader
// pays for.
const maxWriteSites = 3

func StateRoles(f *flowEntity.Flow, m flowEntity.StateMachine) *Prompt {
	var b strings.Builder

	fmt.Fprintf(&b, "Type:  %s\n", m.Type)
	if m.Field != "" {
		fmt.Fprintf(&b, "Field: %s\n", m.Field)
	}

	b.WriteString("\nDeclared states — this list is COMPLETE, the type system guarantees no\nother value can exist:\n\n")
	written := map[string][]flowEntity.StateWrite{}
	for _, w := range m.Writes {
		written[w.To] = append(written[w.To], w)
	}
	never := map[string]bool{}
	for _, s := range m.NeverAssigned {
		never[s] = true
	}

	for _, s := range m.States {
		fmt.Fprintf(&b, "  %s\n", s)
		switch {
		case never[s]:
			// Already proved. Saying it here stops the agent going to look.
			b.WriteString("      nothing in this module ever assigns it\n")
		default:
			// A few write sites, not all of them.
			//
			// The old question printed every one. On a real service that is 122
			// lines for a single state, and the 123rd tells a reader nothing the
			// first three did not: the question is what ROLE the state plays,
			// and three examples answer it. The rest are in flow.json for
			// anyone who wants them.
			shown := written[s]
			if len(shown) > maxWriteSites {
				shown = shown[:maxWriteSites]
			}
			for _, w := range shown {
				tx := "outside any transaction"
				if w.InTx {
					tx = "inside a transaction"
				}
				fmt.Fprintf(&b, "      set in %s  (%s:%d, %s)\n", w.In.Symbol, w.In.File, w.Line, tx)
			}
			if extra := len(written[s]) - len(shown); extra > 0 {
				fmt.Fprintf(&b, "      ...and %d more write site(s), in testigo/flow.json\n", extra)
			}
		}
	}

	// The goal and the rules are in StateRolesPreamble: they are identical for
	// every state machine in the repository, and this pack has four.
	p := New("testigo — what part does each state play?").
		Fact(Proof, "The type and its declared states", b.String())

	var decide strings.Builder
	decide.WriteString(`Exactly ONE of these:

  initializing   the value a new row is created with
  in_progress    work is happening; more changes are expected
  pending        waiting on someone else; the outcome is not known yet
  final          a payment here will not legitimately change again
  foreign        NOT part of this lifecycle — it shares the type but belongs to
                 another entity. Look at which functions set it: if they operate
                 on a provider, a contact or a config row rather than on the
                 thing this flow moves, the state is foreign.
  sentinel       never persisted. An in-memory discriminator used to route a
                 branch, which happens to be declared as a constant of this type.
  unclear        you could not tell. This is a real answer and costs nothing.

Then, only where they apply:

  compensating   reached FROM a final state — a refund, a reversal, a chargeback.
                 This is the one legitimate way out of a finished payment.
  retryable      a payment in this FINAL state may legitimately re-enter the
                 flow. Say where it lands in ` + "`retry_enters_at`" + `.

## The two that are usually got wrong

**Is failure really final?** Stripe returns a PaymentIntent to
` + "`requires_payment_method`" + ` after a decline so it can be retried. If anything
here picks failed rows back up — a retry job, a cron, a manual replay — the
state is ` + "`final` + `retryable`" + `, and ` + "`retry_enters_at`" + ` names the state it
comes back as. Marking it plainly final produces a test that goes red the first
time a customer retries a declined card, and somebody deletes the test.

**Is the finished state really finished?** Captured is done until a chargeback
arrives weeks later. If this repository models refunds or reversals, those are
` + "`compensating`" + ` and the finished state can reach them.

`)

	p.Fact(Subject, "What to decide, per state", decide.String())

	// Only the part that VARIES. The JSON example is identical for every state
	// machine in the repository and lives in StateRolesPreamble; copying it here
	// would be 1.2 KB times the number of state machines to say the same thing.
	var out strings.Builder
	fmt.Fprintf(&out, `The shape is in PREAMBLE.md, under "stateRoles". Every declared state above
must appear exactly once in `+"`roles`"+`.

There are %d state(s). That is %d role assignments, not %d matrix cells.
`, len(m.States), len(m.States), len(m.States)*len(m.States))

	return p.Answers(out.String()).Where("testigo/flow.json")
}

// StateRolesPreamble is the shared half, written once into PREAMBLE.md.
const StateRolesPreamble = `
## stateRoles — classifying one state machine

Classify every state of ONE type. Do NOT list which transitions are legal:
testigo derives those from the roles you give, by rule, in code you can read.

### The shape

` + "```" + `
{
  "roles": [
    { "state": "INITIAL",  "initializing": true,
      "proof": "repository/order.go:29 — set on insert" },
    { "state": "PROGRESS", "in_progress": true,
      "proof": "service/order.go:95" },
    { "state": "SUCCESS",  "final": true,
      "proof": "repository/order.go:442 — further inserts refused" },
    { "state": "FAILED",   "final": true, "retryable": true, "retry_enters_at": "PENDING",
      "proof": "get_status/retry.go:31 — a cron re-attempts failed orders" },
    { "state": "ENABLE",   "foreign": true,
      "proof": "provider.go:18 — set on providers, not on the thing this flow moves" },
    { "state": "TEMPCODE", "sentinel": true,
      "proof": "service/order.go:122 — routes a branch, never written to the database" }
  ],
  "exceptions": [
    { "from": "SUCCESS", "to": "REFUNDED",
      "why": "a chargeback can arrive up to 40 days after settlement" }
  ],
  "notes": "anything the roles above cannot carry"
}
` + "```" + `

` + "`exceptions`" + ` is for transitions the roles do not imply and you know to be
real. Leave it empty if there are none — testigo derives the ordinary ones and
only needs the surprises.

### The rules

1. Use only the state names listed in the question. They are the complete set —
   the type system guarantees no other value exists. Do not invent, abbreviate
   or normalise them.
2. Every declared state appears exactly once in ` + "`roles`" + `.
3. Every role carries proof naming a file and line, except ` + "`unclear`" + `.
4. Do not describe which transitions are legal. testigo derives them from the
   roles, over-approximating on purpose: a transition wrongly called legal costs
   one test that never gets written, while one wrongly called illegal produces a
   red test asserting something the business allows, and somebody deletes it
   instead of fixing the code.
5. Where the code and the correct business rule disagree, describe the RULE. A
   role read off the implementation makes the generated test prove only that the
   code does what the code does.
6. ` + "`unclear`" + ` is free and correct. A guess here becomes a failing test that
   wastes an afternoon, or a missing one that lets a real bug through.
`
