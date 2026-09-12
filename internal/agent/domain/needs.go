package domain

import "sort"

var PaymentKindScenarios = map[PaymentType][]string{
	SpineDoubleEntry: {"DOUBLE-ENTRY-SUMS-TO-ZERO", "CONSERVATION-UNDER-CONCURRENCY",
		"LEDGER-OUTSIDE-CALLER-TRANSACTION", "LOST-UPDATE"},
	SpineWallet: {"CONSERVATION-UNDER-CONCURRENCY", "LOST-UPDATE", "MONEY-ROUND-TRIP-EXACT"},
	SpineStateless: {"TIMEOUT-UNKNOWN-OUTCOME", "ASYNC-RESPONSE-BEFORE-DURABILITY",
		"UNCLASSIFIED-ERROR-NOT-RETRIED"},

	MotionOneShot:      {"IDEM-REPLAY", "IDEM-CONCURRENT", "IDEM-CRASH-AT-STEP"},
	MotionAuthCapture:  {"ORPHANED-AUTHORIZATION", "FINAL-STATE-IS-FINAL", "ILLEGAL-TRANSITION-REFUSED"},
	MotionSubscription: {"IDEM-REPLAY", "IDEM-PAYLOAD-MISMATCH"},
	MotionUsageBilling: {"MONEY-ROUND-TRIP-EXACT", "SPLIT-SUMS-TO-TOTAL"},
	MotionTopup:        {"TIMEOUT-UNKNOWN-OUTCOME", "IDEM-REPLAY", "FAILURE-IS-NOT-TERMINAL"},
	MotionVoucher:      {"IDEM-REPLAY", "FINAL-STATE-IS-FINAL"},
	MotionPayout:       {"TIMEOUT-UNKNOWN-OUTCOME", "FINAL-STATE-IS-FINAL", "IDEM-CRASH-AT-STEP"},
	MotionMarketplace:  {"SPLIT-SUMS-TO-TOTAL", "CONSERVATION-UNDER-CONCURRENCY"},
	MotionEscrow:       {"FINAL-STATE-IS-FINAL", "ILLEGAL-TRANSITION-REFUSED"},
	MotionInstallments: {"SPLIT-SUMS-TO-TOTAL", "IDEM-REPLAY"},

	OverlayRefundReversal: {"FINAL-STATE-IS-FINAL", "FAILURE-IS-NOT-TERMINAL", "MONEY-ROUND-TRIP-EXACT"},
	OverlayReconciliation: {"RECONCILER-IS-IDEMPOTENT", "AT-LEAST-ONCE-DELIVERY-IS-SAFE",
		"WEBHOOK-ORDER-INDEPENDENT"},
	OverlayFX: {"CURRENCY-MIXING-REFUSED", "MINOR-UNIT-CONVERSION", "MONEY-ROUND-TRIP-EXACT"},
}

func (c Classification) Scenarios() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range c.All() {
		for _, id := range PaymentKindScenarios[t] {
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

var ScenarioQuestions = map[string][]Question{
	"IDEM-REPLAY": {
		*Discover("IDEM-REPLAY.stores_result",
			"Does this system store the RESULT of a completed request against its key, so a repeat returns the same body rather than doing the work again?",
			"A replay test asserts the second call returns the first call's answer. If nothing is stored there is nothing to replay, and the test would assert a behaviour the system never promised.").Bool(),
		*Discover("IDEM-REPLAY.replays_failures",
			"Is a stored FAILURE replayed too, or does a retry after a failure start over?",
			"These are opposite tests. Replaying a failure means the caller can never retry out of it; starting over means a failed charge can be attempted twice.").Bool(),
	},
	"IDEM-PAYLOAD-MISMATCH": {
		*Discover("IDEM-PAYLOAD-MISMATCH.rejects",
			"If the same key arrives with a DIFFERENT amount or recipient, is that an error?",
			"The published contract says same key plus different parameters is a conflict, not a replay. Returning the first result for a different request silently drops the second one.").Bool(),
	},
	"IDEM-CONCURRENT": {
		*Discover("IDEM-CONCURRENT.exactly_one",
			"When two requests carrying the same key arrive at once, is exactly one allowed to proceed?",
			"This decides what the concurrency test asserts. If the system only reads-then-writes, both can pass the read, and the test is expected to fail until a unique constraint exists.").Bool(),
		*Discover("IDEM-CONCURRENT.loser_behaviour",
			"What does the losing request receive — the winner's result, a conflict error, or a wait?",
			"The assertion differs for each. Guessing produces a test that passes on the wrong behaviour."),
	},
	"IDEM-CRASH-AT-STEP": {
		*Discover("IDEM-CRASH-AT-STEP.recovery",
			"After a crash between two steps, what brings the request to a terminal state — a retry from the caller, a cron, or nothing?",
			"If nothing does, the correct finding is that the request is stranded, and the test should prove that rather than pretend a recovery path exists."),
	},
	"TIMEOUT-UNKNOWN-OUTCOME": {
		*Discover("TIMEOUT-UNKNOWN-OUTCOME.requery",
			"Is there an endpoint or job that asks the provider what actually happened after a timeout?",
			"A timeout is an UNKNOWN, not a failure. Without a requery the only safe design is to make the call deduplicating before it is made, and the test has to assert that instead.").Bool(),
		*Discover("TIMEOUT-UNKNOWN-OUTCOME.requery_resubmits",
			"Does that requery ever RE-SUBMIT the order rather than only reading its status?",
			"A requery that resubmits turns a status check into a second charge. This is a real failure mode in top-up providers and it cannot be seen from the call site.").Bool(),
	},
	"UNCLASSIFIED-ERROR-NOT-RETRIED": {
		*Discover("UNCLASSIFIED-ERROR-NOT-RETRIED.default",
			"When the provider returns an error this code does not recognise, is it treated as retryable or as final?",
			"Retrying an unclassified error is how a single charge becomes several. The default direction is the whole test.").Bool(),
	},
	"CONSERVATION-UNDER-CONCURRENCY": {
		*Discover("CONSERVATION-UNDER-CONCURRENCY.invariant",
			"State the sum that must hold constant across concurrent transfers, in this repository's terms.",
			"A conservation test needs the exact quantity to sum. 'Money is conserved' is not executable; 'the sum of all account balances equals total deposits minus total withdrawals' is."),
	},
	"LOST-UPDATE": {
		*Discover("LOST-UPDATE.locking",
			"Is a balance updated with a read-then-write in application code, or with a single atomic statement or row lock?",
			"Read-then-write loses one of two concurrent updates. Which one it is decides whether the test is expected to pass or expected to fail.").Bool(),
	},
	"DOUBLE-ENTRY-SUMS-TO-ZERO": {
		*Discover("DOUBLE-ENTRY-SUMS-TO-ZERO.enforced_outside_go",
			"Is the balanced-legs rule enforced anywhere except the Go function that writes entries?",
			"An invariant that lives in one code path holds until someone runs an UPDATE by hand. If nothing else enforces it, the property test is the only guard there is.").Bool(),
	},
	"ILLEGAL-TRANSITION-REFUSED": {
		*Discover("ILLEGAL-TRANSITION-REFUSED.enforced_where",
			"Is an illegal status change refused in code, refused by the database, or not refused at all?",
			"The test drives the entity into a state and attempts the illegal move. If nothing refuses it, the test documents a real defect rather than a passing guard.").Bool(),
	},
	"FINAL-STATE-IS-FINAL": {
		*Discover("FINAL-STATE-IS-FINAL.exceptions",
			"Which final states can still be left, and what leaves them — a chargeback, a recall, a manual correction?",
			"A settled payment is finished until a chargeback arrives. A system that models the exception but tests it as impossible has a test that fails the first time reality happens."),
	},
	"FAILURE-IS-NOT-TERMINAL": {
		*Discover("FAILURE-IS-NOT-TERMINAL.retry_entry",
			"After a FAILED order, which state does a retry re-enter, and who triggers it?",
			"If FAILED is genuinely terminal the test asserts nothing follows it. If a cron re-attempts it, the opposite test is correct."),
	},
	"AT-LEAST-ONCE-DELIVERY-IS-SAFE": {
		*Discover("AT-LEAST-ONCE-DELIVERY-IS-SAFE.duplicate_message",
			"If the broker delivers the same message twice, is the second delivery a no-op?",
			"At-least-once delivery is the broker's contract, not a rare event. A handler that is not idempotent will double-apply on an ordinary redelivery.").Bool(),
	},
	"WEBHOOK-ORDER-INDEPENDENT": {
		*Discover("WEBHOOK-ORDER-INDEPENDENT.out_of_order",
			"If a 'succeeded' callback arrives before the 'pending' one, does the final state still end up correct?",
			"Providers do not guarantee order. A handler that applies whatever arrives last will overwrite a terminal state with a stale one.").Bool(),
	},
	"MONEY-ROUND-TRIP-EXACT": {
		*Discover("MONEY-ROUND-TRIP-EXACT.representation",
			"Is an amount ever converted to a float, a string, or a different unit on its way in or out — including in JSON, the database driver, or a provider payload?",
			"Every conversion is a chance to lose a minor unit. The round-trip test needs to know which hops exist to be worth running.").Bool(),
	},
	"MINOR-UNIT-CONVERSION": {
		*Discover("MINOR-UNIT-CONVERSION.boundaries",
			"Which currencies does this system handle that are not two-decimal — JPY, KWD, or similar?",
			"A conversion helper that assumes 100 minor units per major is wrong for both. The table test needs the real list to have any value."),
	},
	"SPLIT-SUMS-TO-TOTAL": {
		*Discover("SPLIT-SUMS-TO-TOTAL.remainder",
			"When a total does not divide evenly, who receives the remainder — the first party, the last, the platform?",
			"Somebody must take the leftover minor unit. If nobody does, the split silently loses money, and only a stated rule makes that assertable."),
	},
	"CURRENCY-MIXING-REFUSED": {
		*Discover("CURRENCY-MIXING-REFUSED.can_mix",
			"Can one transaction legally hold legs in more than one currency?",
			"Legs that sum to zero numerically and are nonsense financially. If mixing is legal something else has to make it balance, and the test has to know what.").Bool(),
	},
	"RECONCILER-IS-IDEMPOTENT": {
		*Discover("RECONCILER-IS-IDEMPOTENT.rerun",
			"If reconciliation runs twice over the same window, does the second run change anything?",
			"A reconciler that posts corrections without checking will post them again on the next run, which turns a repair job into a source of drift.").Bool(),
	},
	"ASYNC-RESPONSE-BEFORE-DURABILITY": {
		*Discover("ASYNC-RESPONSE-BEFORE-DURABILITY.acks_early",
			"Does the caller get a success response before the work is durably recorded?",
			"If the response goes out first, a crash in the window leaves the caller believing something happened that did not. That gap is the test.").Bool(),
	},
	"ORPHANED-AUTHORIZATION": {
		*Discover("ORPHANED-AUTHORIZATION.expiry",
			"If an authorization is never captured, what releases the hold, and after how long?",
			"An uncaptured hold ties up a customer's money. Whether anything releases it decides if this is a test or a finding."),
	},
	"NETWORK-CALL-INSIDE-TRANSACTION": {
		*Discover("NETWORK-CALL-INSIDE-TRANSACTION.intended",
			"Is the provider call inside the database transaction on purpose, or has it drifted there?",
			"A slow provider holding a database transaction open is how a connection pool is exhausted, and the answer decides whether the test asserts current or intended behaviour.").Bool(),
	},
	"LEDGER-OUTSIDE-CALLER-TRANSACTION": {
		*Discover("LEDGER-OUTSIDE-CALLER-TRANSACTION.shares_tx",
			"Does the ledger write join the caller's transaction, or open its own?",
			"A ledger that opens its own transaction commits even when the caller rolls back, which is how the books and the business disagree.").Bool(),
	},
}

var TechniqueQuestions = map[string][]Question{
	"faultInjection": {
		*Discover("technique.faultInjection.observable",
			"After the injected failure, what can a test READ to prove the system reacted correctly — a row, a status, a published message?",
			"A fault-injection test that only asserts an error was returned proves nothing about the state left behind, which is the part that matters."),
	},
	"concurrency": {
		*Discover("technique.concurrency.contended",
			"Name the exact row, key or counter two concurrent requests contend for.",
			"A concurrency test that does not collide passes having tested nothing. Naming the contended resource is what makes the collision reproducible."),
	},
	"stateMachine": {
		*Discover("technique.stateMachine.driver",
			"Which function moves the entity between states, and what does it need to be called with?",
			"A state-machine test has to drive the entity into a state before it can attempt an illegal move. Without the driver it can only assert against a struct it built by hand, which proves nothing about the real code."),
	},
	"property": {
		*Discover("technique.property.bounds",
			"What are the legal bounds of an amount here — minimum, maximum, and is zero or negative allowed?",
			"A property test generating values outside the legal domain reports failures that are not bugs, and the noise is how people learn to ignore it."),
	},
	"fuzz": {
		*Discover("technique.fuzz.valid_input",
			"What makes an input valid enough to reach the logic under test rather than bouncing off validation?",
			"A fuzzer that never gets past the validator explores nothing. The seed corpus needs to know the shape of an accepted request."),
	},
	"table": {
		*Discover("technique.table.boundaries",
			"Which specific values are the interesting boundaries for this rule?",
			"A table test is only as good as its rows. Boundaries a person knows and the compiler does not are the rows worth writing."),
	},
	"narrowIntegration": {
		*Discover("technique.narrowIntegration.real_dependency",
			"Which dependency has to be real for this test to mean anything, and which can be faked?",
			"Faking the thing under test makes the test tautological; making everything real makes it slow and flaky. Only someone who knows the system can draw that line."),
	},
	"metamorphic": {
		*Discover("technique.metamorphic.relation",
			"State a relation that must hold between two runs — same input twice, or input in a different order.",
			"A metamorphic test needs the relation stated before it can be asserted. Without one there is no oracle."),
	},
}

type Need struct {
	Question Question
	Because  []string
}

func Needed(c Classification, scenarios, techniques []string, answered map[string]bool) []Need {
	because := map[string][]string{}
	order := []Question{}
	seen := map[string]bool{}

	add := func(q Question, why string) {
		if answered[q.ID] {
			return
		}
		if !seen[q.ID] {
			seen[q.ID] = true
			order = append(order, q)
		}
		because[q.ID] = appendOnce(because[q.ID], why)
	}

	for _, t := range c.All() {
		for _, q := range PaymentTypeQuestions[t] {
			add(q, string(t))
		}
	}
	for _, s := range scenarios {
		for _, q := range ScenarioQuestions[s] {
			add(q, s)
		}
	}
	for _, t := range techniques {
		for _, q := range TechniqueQuestions[t] {
			add(q, t)
		}
	}

	out := make([]Need, 0, len(order))
	for _, q := range order {
		sort.Strings(because[q.ID])
		out = append(out, Need{Question: q, Because: because[q.ID]})
	}
	return out
}

var questionIndex map[string]Question

func QuestionByID(id string) (Question, bool) {
	if questionIndex == nil {
		questionIndex = map[string]Question{}
		for _, qs := range PaymentTypeQuestions {
			for _, q := range qs {
				questionIndex[q.ID] = q
			}
		}
		for _, qs := range ScenarioQuestions {
			for _, q := range qs {
				questionIndex[q.ID] = q
			}
		}
		for _, qs := range TechniqueQuestions {
			for _, q := range qs {
				questionIndex[q.ID] = q
			}
		}
	}
	q, ok := questionIndex[id]
	return q, ok
}
