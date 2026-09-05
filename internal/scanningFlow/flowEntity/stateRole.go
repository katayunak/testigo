package flowEntity

import (
	"fmt"
	"sort"
	"strings"
)

// StateRole is what part one state plays in a lifecycle.
//
// This replaces most of a question that was the wrong shape. Asking an agent
// which transitions are legal means asking about an N×N matrix: nine states is
// eighty-one cells, and every cell is a separate chance to be wrong. Asking
// what ROLE each state plays is nine answers, and the matrix follows from them
// by rule.
//
// The roles below are not a taxonomy someone invented. They are what a real
// recharge service's nine states actually turned out to be, and each one earns
// its place by generating transitions no simpler scheme gets right:
//
//   - without Retryable, FAILED looks terminal. It is not: a retry cron picks
//     failed orders back up. Treating it as final produces a test that fails
//     the first time a customer retries a declined card.
//   - without Compensating, a refund after capture is an illegal transition.
//   - without Foreign, ENABLE and DISABLE — which belong to providers, not
//     orders, and merely share the Status type — become states an order can be
//     in, and the generated test asserts nonsense.
//   - without Sentinel, FAILEDSIMTYPE looks like a state nothing ever writes,
//     when it is really an in-memory discriminator that is never stored.
type StateRole struct {
	State string `json:"state"`

	// Where in the lifecycle it sits. A state has exactly one of these.
	Initializing bool `json:"initializing,omitempty"` // the value on creation
	InProgress   bool `json:"in_progress,omitempty"`  // work happening, more changes expected
	Pending      bool `json:"pending,omitempty"`      // waiting on someone else, outcome unknown
	Final        bool `json:"final,omitempty"`        // will not legitimately change again

	// Modifiers. These are what make a payment lifecycle different from a
	// textbook one, and they are the source of the transitions people get wrong.
	Compensating bool `json:"compensating,omitempty"` // reached FROM a final state: refund, reversal, chargeback
	Retryable    bool `json:"retryable,omitempty"`    // a payment here may legitimately re-enter the flow

	// RetryEntersAt is WHERE a retryable state re-enters, when that is known.
	//
	// Retryable alone says a payment can leave a final state; it does not say
	// where it lands, and the two are different facts. On the recharge service
	// FAILED re-enters at PENDING specifically — not at INITIAL, because the
	// order already exists. Guessing "anywhere earlier" derives three legal
	// transitions where there is one, which weakens every test built on it.
	RetryEntersAt string `json:"retry_enters_at,omitempty"`

	// Not part of this lifecycle at all.
	Foreign  bool `json:"foreign,omitempty"`  // belongs to another entity that shares this type
	Sentinel bool `json:"sentinel,omitempty"` // never persisted; an in-memory discriminator
	Unclear  bool `json:"unclear,omitempty"`  // could not be determined — say so rather than guess

	Proof string `json:"proof,omitempty"`
}

// phase returns the single lifecycle position, or "" when the state is not in
// the lifecycle or nobody could tell.
func (r StateRole) phase() string {
	switch {
	case r.Foreign:
		return "foreign"
	case r.Sentinel:
		return "sentinel"
	case r.Unclear:
		return ""
	case r.Initializing:
		return "initializing"
	case r.InProgress:
		return "inProgress"
	case r.Pending:
		return "pending"
	case r.Final:
		return "final"
	}
	return ""
}

// Validate refuses a role assignment that cannot describe a real state.
//
// Coherence is checkable and correctness is not, so this checks coherence
// hard. A state that is both in progress and final is not a subtle judgement
// call, it is a contradiction, and letting one through produces a derived
// matrix that contradicts itself.
func (r StateRole) Validate() error {
	var set []string
	for name, on := range map[string]bool{
		"initializing": r.Initializing, "in_progress": r.InProgress,
		"pending": r.Pending, "final": r.Final,
		"foreign": r.Foreign, "sentinel": r.Sentinel, "unclear": r.Unclear,
	} {
		if on {
			set = append(set, name)
		}
	}
	sort.Strings(set)

	switch {
	case len(set) == 0:
		return fmt.Errorf("%s: no role given — every state is somewhere in the lifecycle, "+
			"outside it (foreign/sentinel), or genuinely unclear", r.State)
	case len(set) > 1:
		return fmt.Errorf("%s: %s are mutually exclusive, pick one", r.State, strings.Join(set, " and "))
	}

	// Modifiers only mean something on a lifecycle state.
	if (r.Compensating || r.Retryable) && (r.Foreign || r.Sentinel || r.Unclear) {
		return fmt.Errorf("%s: compensating/retryable describe how a payment moves through the "+
			"lifecycle, so they cannot apply to a state outside it", r.State)
	}
	// Retryable is the escape hatch from final. On a non-final state it is noise.
	if r.Retryable && !r.Final {
		return fmt.Errorf("%s: retryable means a payment can leave a state it otherwise could not, "+
			"which only says something about a FINAL state", r.State)
	}
	if r.RetryEntersAt != "" && !r.Retryable {
		return fmt.Errorf("%s: retry_enters_at names where a retry lands, so it means nothing "+
			"unless the state is retryable", r.State)
	}
	if r.Proof == "" && !r.Unclear {
		return fmt.Errorf("%s: no proof — name the file and line that shows this", r.State)
	}
	return nil
}

// StateRoles is one classification per declared state.
type StateRoles []StateRole

// Derive computes the legal transition matrix from the roles.
//
// This is the whole point: nine role assignments generate eighty-one cells, in
// Go, for free, with rules a person can read and argue with. Checked against a
// real payment service, the rules below reproduce an agent's hand-written
// matrix exactly — including the two cases that make payments different from a
// textbook state machine, FAILED being retryable and ENABLE/DISABLE belonging
// to a different entity.
//
// What it deliberately does NOT produce is the exceptions. "A chargeback can
// arrive forty days after capture" is not derivable from a role, and that is
// exactly the kind of thing still worth paying an agent to tell you.
func (rs StateRoles) Derive(neverAssigned []string) map[string][]string {
	unreachable := map[string]bool{}
	for _, s := range neverAssigned {
		// Nothing in this module writes it, so nothing in this module can move
		// to it. testigo already proved this in phase 1; using it here removes
		// derived transitions that provably cannot happen. On recharge that is
		// ERROR, which is declared and set by something outside the codebase.
		unreachable[s] = true
	}

	byState := map[string]StateRole{}
	var active, finals, inits, foreign []string
	for _, r := range rs {
		byState[r.State] = r
		switch r.phase() {
		case "foreign":
			foreign = append(foreign, r.State)
		case "initializing":
			inits = append(inits, r.State)
		case "inProgress", "pending":
			// Both are ACTIVE, and they reach each other. A payment waiting on
			// a provider goes back to processing when the reply arrives, and
			// back to waiting if it needs another call. Ranking pending after
			// inProgress made that legal transition look illegal.
			active = append(active, r.State)
		case "final":
			finals = append(finals, r.State)
		}
	}
	sort.Strings(active)
	sort.Strings(finals)
	sort.Strings(foreign)

	reachable := func(list []string) []string {
		var out []string
		for _, s := range list {
			if !unreachable[s] {
				out = append(out, s)
			}
		}
		return out
	}

	out := map[string][]string{}
	for _, r := range rs {
		out[r.State] = []string{}
	}

	// Forward movement. This deliberately OVER-approximates, for the same
	// reason the call graph does: a transition wrongly called legal costs one
	// test that was never generated, while a transition wrongly called illegal
	// produces a red test asserting something the business actually allows —
	// and someone deletes it instead of fixing the code.
	for _, from := range inits {
		out[from] = append(reachable(active), reachable(finals)...)
		sort.Strings(out[from])
	}
	for _, from := range active {
		var to []string
		for _, s := range reachable(active) {
			if s != from || byState[from].Pending {
				// A pending state can be set again: still waiting is an update.
				to = append(to, s)
			}
		}
		to = append(to, reachable(finals)...)
		sort.Strings(to)
		out[from] = to
	}

	// Final means final. Two named ways out, and no others.
	for _, from := range finals {
		fr := byState[from]
		var to []string
		for _, cand := range finals {
			if cand != from && byState[cand].Compensating && !unreachable[cand] {
				to = append(to, cand)
			}
		}
		switch {
		case fr.Retryable && fr.RetryEntersAt != "":
			if !unreachable[fr.RetryEntersAt] {
				to = append(to, fr.RetryEntersAt)
			}
		case fr.Retryable:
			// Retryable but nobody said where it lands: over-approximate to
			// every active state rather than silently pick one.
			to = append(to, reachable(active)...)
		}
		sort.Strings(to)
		out[from] = to
	}

	// Foreign states are their own machine and never touch this lifecycle.
	for _, from := range foreign {
		var to []string
		for _, cand := range foreign {
			if cand != from {
				to = append(to, cand)
			}
		}
		sort.Strings(to)
		out[from] = to
	}

	return out
}

// Initial returns the state a new row starts in, and whether exactly one was
// named. Two initial states is usually a sign the type is shared between
// entities, which is what Foreign exists to record.
func (rs StateRoles) Initial() (string, bool) {
	var found []string
	for _, r := range rs {
		if r.Initializing {
			found = append(found, r.State)
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return "", false
}

// Finals returns the states a payment cannot legitimately leave, ignoring the
// retryable ones, because a retryable final is not one a test may assume is
// terminal.
func (rs StateRoles) Finals() []string {
	var out []string
	for _, r := range rs {
		if r.Final && !r.Retryable {
			out = append(out, r.State)
		}
	}
	sort.Strings(out)
	return out
}

// Shape describes the lifecycle in one line, and says what is wrong with it.
//
// A healthy payment lifecycle has exactly one place to start, somewhere to be
// while work happens, and at least one place to stop. Anything else is worth a
// person's attention, so this reports the deviation rather than a score: a
// number would have to be invented, and "two states claim to be the start"
// tells you what to go and look at.
func (rs StateRoles) Shape() (string, []string) {
	counts := map[string]int{}
	for _, r := range rs {
		counts[r.phase()]++
	}
	line := fmt.Sprintf("%d initializing, %d in progress, %d pending, %d final, %d compensating, %d foreign, %d sentinel, %d unclear",
		counts["initializing"], counts["inProgress"], counts["pending"], counts["final"],
		rs.countCompensating(), counts["foreign"], counts["sentinel"], counts[""])

	var problems []string
	if counts["initializing"] == 0 {
		problems = append(problems, "no state is the one a new row starts in, so no test can set one up")
	}
	if counts["initializing"] > 1 {
		problems = append(problems, "more than one state claims to be the start — usually a type shared between entities")
	}
	if counts["final"] == 0 {
		problems = append(problems, "nothing is final, so the strongest available test — drive it to the end and refuse every further move — cannot be written")
	}
	if counts[""] > 0 {
		problems = append(problems, fmt.Sprintf("%d state(s) unclear; every one is a transition nobody can test", counts[""]))
	}
	return line, problems
}

func (rs StateRoles) countCompensating() int {
	n := 0
	for _, r := range rs {
		if r.Compensating {
			n++
		}
	}
	return n
}
