package testPlan

import (
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
)

// Catalog is every failure mode testigo knows how to test for.
//
// Written by hand, from the published practice of companies that move money at
// scale. This is the part that cannot be generated and should not be: it is the
// difference between testigo and a single prompt saying "write some tests", and
// it is the only thing standing between a generated suite and the failure where
// the test encodes the bug.
//
// Sources are on every entry. An entry with no source is an entry someone
// invented, and a reader should be able to tell at a glance which is which.
var Catalog = []planEntity.Scenario{

	// ── idempotency ────────────────────────────────────────────────────────

	{
		ID:     "IDEM-REPLAY",
		Name:   "A retry with the same key replays the first result",
		Family: planEntity.FamilyIdempotency,
		CaseScenario: `Send a payment request carrying an idempotency key. Let it complete. Send
the byte-identical request again with the same key.

The second request must not execute anything. It must return what the first
request returned, including when the first request FAILED — a stored 500
replays as a 500, it is not a fresh attempt.

This is worth testing because a client that times out cannot tell whether the
server processed the request, so it retries. If the retry executes, the
customer is applied twice, and nothing in the code looks wrong when you read
it.`,
		LookingFor: "a retry that re-executes instead of replaying, producing a second money movement",
		Acceptance: []string{
			"the number of calls to the payment provider is exactly one after both requests",
			"the second response is byte-identical to the first",
			"when the first attempt fails, the second request returns that same failure rather than retrying",
			"the ledger contains exactly one entry",
		},
		AntiGoals: []string{
			"asserting the second response equals whatever the code returns — that is true by construction and cannot fail",
			"only testing the success path; the replayed-failure rule is the half that is usually broken",
			"counting database rows instead of provider calls, which misses a duplicate money movement with a single row",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueFaultInjection, planEntity.TechniqueUnit},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{IdempotencyKey: true, InjectableSeam: true},
		Severity:   flowEntity.SevCritical,
		Source:     "Stripe idempotent requests; brandur.org/idempotency-keys",
	},

	{
		ID:     "IDEM-PAYLOAD-MISMATCH",
		Name:   "The same key with different parameters is refused",
		Family: planEntity.FamilyIdempotency,
		CaseScenario: `Send a request with key K and amount 100. Send a second request with the same
key K but amount 200.

The second must be rejected as a conflict. It must not replay the first
response, and it must not execute the new amount.

Both wrong answers are dangerous in different directions. Replaying silently
tells the caller their 200 succeeded when 100 was committed. Executing moves
money twice under one key, which defeats the entire mechanism.`,
		LookingFor: "an idempotency layer that keys on the key alone and never compares the request body",
		Acceptance: []string{
			"the second request returns a conflict, not the first response and not a success",
			"the provider is called exactly once, with the original amount",
			"the stored record still reflects the first request",
		},
		AntiGoals: []string{
			"skipping this because the code has no payload comparison — write it anyway and let it go red, that IS the finding",
			"asserting a specific HTTP status when the repository does not use HTTP; assert the refusal, not the transport",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueFaultInjection, planEntity.TechniqueUnit},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{IdempotencyKey: true, InjectableSeam: true},
		Severity:   flowEntity.SevHigh,
		Source:     "Stripe error-low-level; GoCardless returns 409 invalid_state with conflicting_resource_id",
	},

	{
		ID:     "IDEM-CONCURRENT",
		Name:   "Two simultaneous requests with one key produce one money movement",
		Family: planEntity.FamilyIdempotency,
		CaseScenario: `Release N goroutines from a barrier so they all issue the same request with the
same idempotency key at the same instant.

Exactly one must proceed. The others must receive a conflict or the first
result. The provider must be called exactly once.

The barrier matters more than N. Two sequential requests always pass, even on
code that checks for an existing key and then inserts, because the window
between the check and the insert is microseconds wide. Only a genuine collision
opens it.`,
		LookingFor: "check-then-insert idempotency, where both requests pass the existence check before either writes",
		Acceptance: []string{
			"exactly one goroutine observes success",
			"the provider call count is exactly one",
			"exactly one row exists for the key",
			"the test passes under -race",
			"the test asserts that a collision actually occurred, and fails loudly if the goroutines never overlapped",
		},
		AntiGoals: []string{
			"using time.Sleep to stagger the goroutines, which removes the collision the test exists to create",
			"running two goroutines and calling it concurrency — the window needs tens of attempts to open reliably",
			"passing green without checking that any two goroutines actually raced; that result proves nothing",
			"writing it against an in-memory map when the real enforcement is a database constraint, since a map cannot exhibit the same race",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueConcurrency, planEntity.TechniqueNarrowIntegration},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{IdempotencyKey: true, InjectableSeam: true},
		Severity:   flowEntity.SevCritical,
		Source:     "Airbnb Orpheus; brandur.org/idempotency-keys lease with 409",
	},

	{
		ID:     "IDEM-CRASH-AT-STEP",
		Name:   "A crash at any step, then a retry, ends where a clean run ends",
		Family: planEntity.FamilyIdempotency,
		CaseScenario: `For every step in the flow that leaves the process, run the request with that
step configured to fail, then retry with the same key against healthy
dependencies.

The terminal state must equal the terminal state of a clean run, and the number
of foreign mutations must be exactly one across both attempts.

Table-driven, one case per step. The steps are enumerable — phase 1 listed them
— which is what makes this the highest-value generated test available: it is
exhaustive over the real failure surface rather than a sample of it.`,
		LookingFor: "a partially completed flow that a retry completes twice, or leaves stuck forever",
		Acceptance: []string{
			"for each injected failure point, the final state matches the clean-run final state",
			"the provider mutation count across both attempts is exactly one",
			"no case leaves the payment in a state the state machine does not declare",
			"each case asserts that the injected failure actually fired",
		},
		AntiGoals: []string{
			"injecting only at the first step; the interesting crashes are the late ones, after money has already moved",
			"treating a timeout the same as an error — they need separate cases, because a timeout has an unknown outcome",
			"asserting only that no error was returned, which is satisfied by a flow that silently did nothing",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueFaultInjection},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{IdempotencyKey: true, InjectableSeam: true},
		Severity:   flowEntity.SevCritical,
		Source:     "brandur.org/idempotency-keys recovery points; Airbnb Orpheus",
	},

	// ── failure and timeouts ───────────────────────────────────────────────

	{
		ID:     "TIMEOUT-UNKNOWN-OUTCOME",
		Name:   "A provider timeout is not treated as a failure",
		Family: planEntity.FamilyFailure,
		CaseScenario: `Configure the payment provider fake to accept the call, record it as SUCCEEDED
on its side, and then time out without answering. Let the code handle it. Then
retry the request the way a client would.

The customer must move once, not twice.

This is the single most expensive bug shape in payments and it is invisible to
every test that only exercises clean success and clean failure. A timeout does
not mean the request failed; it means the outcome is UNKNOWN. Code that maps
timeout onto failure and retries is moving the money twice for one purchase.`,
		LookingFor: "a timeout classified as a retryable failure, when the provider actually processed the request",
		Acceptance: []string{
			"the provider records exactly one successful authorization across both attempts",
			"the code either sends an idempotency key the provider deduplicates on, or reconciles the unknown outcome before retrying",
			"the payment does not end in a state that contradicts the provider's record",
			"the test asserts the timeout path was actually taken",
		},
		AntiGoals: []string{
			"faking a timeout as an immediate error return — the whole point is that the call SUCCEEDED on the other side",
			"asserting an error was returned; the code returning an error is fine, moving the money twice is not",
			"using a real sleep to produce the timeout, instead of a fake that returns a deadline error",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueFaultInjection},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{InjectableSeam: true, SeamKinds: []flowEntity.SeamKind{flowEntity.SeamHTTP, flowEntity.SeamQueue}},
		Severity:   flowEntity.SevCritical,
		Source:     "Airbnb Orpheus retryable classification; Stripe timeout guidance",
	},

	{
		ID:     "ORPHANED-AUTHORIZATION",
		Name:   "The provider is never committed without a local record",
		Family: planEntity.FamilyFailure,
		CaseScenario: `Let the provider call succeed, then make the very next durable write fail — the
COMMIT, or the ledger insert.

After the dust settles, the provider believes it holds an authorization. Check
whether this system knows about it.

If it does not, that money is orphaned: the customer sees a hold, support sees
nothing, and only a reconciliation job will ever find it. The test does not
demand the write succeed. It demands that the failure is RECORDED somewhere a
reconciler can find.`,
		LookingFor: "money moved at the provider with no durable local trace, because the crash landed between the two",
		Acceptance: []string{
			"after the failure, either the local state records the provider reference, or a durable record exists for a reconciler to pick up",
			"the system does not report success to the caller",
			"a second attempt does not authorize again",
		},
		AntiGoals: []string{
			"asserting the transaction rolled back and stopping there — the rollback is exactly the problem, since it erases the only trace of the provider call",
			"treating this as the same test as IDEM-CRASH-AT-STEP; that one is about converging, this one is about not losing proof",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueFaultInjection},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{InjectableSeam: true, OpensTx: true},
		Severity:   flowEntity.SevCritical,
		Source:     "Shopify anomalies as first-class rows; Uber reconciliation events",
	},

	{
		ID:     "UNCLASSIFIED-ERROR-NOT-RETRIED",
		Name:   "An unrecognised error is not retried",
		Family: planEntity.FamilyFailure,
		CaseScenario: `Return an error from the provider that the code has never seen — an unknown
status, an unmapped vendor code, a wrapped error with no type.

The code must NOT retry it. The default for an unclassified error is
non-retryable.

Airbnb states this rule explicitly, and the reasoning is one sentence: one
unclassified error retried is one duplicate money movement. The safe default costs a
manual investigation; the unsafe default costs a customer's money.`,
		LookingFor: "a retry loop with a default branch that retries anything it does not recognise",
		Acceptance: []string{
			"the provider is called exactly once when the error is unrecognised",
			"the payment ends in a state that a human or a reconciler will notice",
			"errors that ARE classified as retryable are still retried, so the test proves the classification works rather than that retries were disabled",
		},
		AntiGoals: []string{
			"only testing unknown errors; without a retryable case alongside it, a code path that never retries anything passes",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueTable, planEntity.TechniqueFaultInjection},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{InjectableSeam: true},
		Severity:   flowEntity.SevHigh,
		Source:     "Airbnb Orpheus: retryable vs non-retryable, defaulting to non-retryable",
	},

	// ── consistency and concurrency ────────────────────────────────────────

	{
		ID:     "CONSERVATION-UNDER-CONCURRENCY",
		Name:   "Money is conserved while transfers run in parallel",
		Family: planEntity.FamilyConsistency,
		CaseScenario: `Seed a set of accounts with a known total. Run many concurrent transfers
between random pairs. While they run, read ALL balances repeatedly.

Every read must sum to the original total, and no balance may go negative.

The detail that makes this test work is that the reads happen MID-FLIGHT, not at
the end. A test that checks the total only after everything settles passes on
code that debits and credits non-atomically, because the two halves have both
landed by the time it looks. Reading during the storm is what catches the gap
between them.

This is Jepsen's bank workload, which is the canonical implementation.`,
		LookingFor: "a debit and credit that are not atomic together, so money briefly or permanently vanishes",
		Acceptance: []string{
			"every mid-flight read of all balances sums to the seeded total",
			"no balance is ever observed negative",
			"the test passes under -race",
			"the test asserts that reads actually interleaved with writes rather than running before or after them",
		},
		AntiGoals: []string{
			"summing only at the end, which is the version that passes on broken code",
			"running it against an in-memory map when the real system uses a database, since the map cannot exhibit the isolation behaviour that causes the bug",
			"asserting a specific final balance per account; the invariant is the total, not the distribution",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueNarrowIntegration, planEntity.TechniqueProperty, planEntity.TechniqueConcurrency},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{BalanceFunc: true, MoneyFlows: true, RealDatabase: true},
		Severity:   flowEntity.SevCritical,
		Source:     "Jepsen bank workload; Uber zero-sum money orders; Nubank generative ledger testing",
	},

	{
		ID:     "LOST-UPDATE",
		Name:   "Two concurrent debits on one account both take effect",
		Family: planEntity.FamilyConsistency,
		CaseScenario: `Start an account at 100. Run two concurrent operations that each read the
balance, subtract 60, and write it back.

Either one must fail, or the final balance must be -20 if overdraft is allowed.
What must NOT happen is a final balance of 40, which means one update was lost.

This needs a REAL database. Under READ COMMITTED, both transactions read 100,
both write 40, and the second silently overwrites the first. No in-memory fake
reproduces that, so a unit-test version of this scenario passes on broken code
and is worse than not writing it.

Expect roughly 50 concurrent attempts to open the window reliably. Two will
almost always pass.`,
		LookingFor: "a read-then-write on a balance with no SELECT FOR UPDATE and no SERIALIZABLE isolation",
		Acceptance: []string{
			"the sum of applied changes equals the change in the stored balance",
			"either an operation fails cleanly, or both are applied — never one silently discarded",
			"the test runs against a real database instance, not a fake",
		},
		AntiGoals: []string{
			"writing this as a small test with a mutex-protected map, which cannot exhibit the bug",
			"using two goroutines; the window is too narrow to hit consistently at that count",
			"asserting an exact final balance, which depends on which transaction wins a legitimate race",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueNarrowIntegration},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{BalanceFunc: true, MoneyFlows: true, RealDatabase: true, SeamKinds: []flowEntity.SeamKind{flowEntity.SeamDB}},
		Severity:   flowEntity.SevCritical,
		Source:     "PostgreSQL concurrency control docs; Jepsen",
	},

	{
		ID:     "DOUBLE-ENTRY-SUMS-TO-ZERO",
		Name:   "Every set of ledger entries sums to zero",
		Family: planEntity.FamilyConsistency,
		CaseScenario: `Generate random sets of ledger entries and try to record them. Any set whose
amounts do not sum to zero must be REJECTED at the point of construction, before
anything is written.

Uber validates this before the money order is persisted, with the stated rule
that no money can ever be created or destroyed. Checking it at the domain
constructor rather than only at the database means no code path anywhere can
build an unbalanced movement.`,
		LookingFor: "a code path that writes an unbalanced set of entries, creating or destroying money",
		Acceptance: []string{
			"any generated entry set summing to non-zero is rejected",
			"any set summing to zero is accepted",
			"rejection happens before persistence, not after",
		},
		AntiGoals: []string{
			"only testing hand-picked balanced examples; the generated unbalanced ones are the point",
			"checking the total after writing, which tests the query rather than the guard",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueProperty, planEntity.TechniqueTable},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{TransferFunc: true, MoneyFlows: true},
		Severity:   flowEntity.SevCritical,
		Source:     "Uber money orders; Square Books; Formance ledger integrity",
	},

	{
		ID:     "LEDGER-OUTSIDE-CALLER-TRANSACTION",
		Name:   "A ledger write is rolled back with the transaction that caused it",
		Family: planEntity.FamilyBoundary,
		CaseScenario: `Open the flow's transaction, let the ledger write happen, then force the
transaction to roll back.

The ledger entry must be gone. If the ledger holds its own connection instead of
using the caller's transaction, it will not be — and the money stays posted while
the payment that caused it does not exist.

This is a real and common shape: a repository takes *sql.DB rather than a
transaction handle, and every write it makes is outside whatever the caller
opened.`,
		LookingFor: "a repository that writes through its own handle rather than the caller's transaction",
		Acceptance: []string{
			"after rollback, no ledger entry exists for the payment",
			"after commit, exactly one exists",
		},
		AntiGoals: []string{
			"faking the ledger, which hides the entire bug — this scenario is about which handle the real implementation uses",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueNarrowIntegration},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{TransferFunc: true, OpensTx: true, RealDatabase: true},
		Severity:   flowEntity.SevCritical,
		Source:     "brandur.org/job-drain on transactional staging; standard outbox reasoning",
	},

	{
		ID:     "NETWORK-CALL-INSIDE-TRANSACTION",
		Name:   "No network call happens while a transaction is open",
		Family: planEntity.FamilyBoundary,
		CaseScenario: `Wrap the database handle and the HTTP client in fakes that know about phases.
Run the flow. If a network call happens between BEGIN and COMMIT, fail the test
immediately.

Airbnb's Orpheus states this as a hard rule: no service interaction in the
pre-RPC and post-RPC phases, no database interaction in the RPC phase. Holding a
transaction open across a network round trip pins database locks for the length
of somebody else's timeout, which turns a slow provider into a database outage.

This is a runtime witness for what phase 1 already found statically, and it is
the version that keeps being true as the code changes.`,
		LookingFor: "a provider call made while database locks are held",
		Acceptance: []string{
			"no call to the network fake occurs while the transaction fake reports an open transaction",
			"the test fails with the specific call that violated it, not a generic assertion",
		},
		AntiGoals: []string{
			"asserting on timing or duration, which is flaky and measures the wrong thing",
			"skipping it because phase 1 already reported it statically — a static finding is fixed once, a test keeps it fixed",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueFaultInjection, planEntity.TechniqueUnit},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{OpensTx: true, InjectableSeam: true, SeamKinds: []flowEntity.SeamKind{flowEntity.SeamHTTP, flowEntity.SeamQueue}},
		Severity:   flowEntity.SevHigh,
		Source:     "Airbnb Orpheus phase rules; brandur atomic phases",
	},

	// ── state machine ──────────────────────────────────────────────────────

	{
		ID:     "ILLEGAL-TRANSITION-REFUSED",
		Name:   "Transitions that should be impossible are refused",
		Family: planEntity.FamilyState,
		CaseScenario: `For every ordered pair of states the round-one answers marked illegal: drive a
payment into the source state, attempt to move it to the target state, and
assert the attempt is refused and the stored state is unchanged.

The state set is complete because the compiler guarantees it, so this table is
exhaustive over the machine rather than a sample of it. Pairs the agent marked
unsure are excluded — a guess must not become a failing test.`,
		LookingFor: "a status field assigned directly with no guard, so any state can follow any other",
		Acceptance: []string{
			"each illegal attempt returns an error or is otherwise refused",
			"the persisted state after a refused attempt equals the state before it",
			"legal transitions in the same table still succeed, proving the guard is selective rather than absent",
		},
		AntiGoals: []string{
			"including pairs marked unsure, which converts a guess into a red test someone has to investigate",
			"only testing illegal pairs; without legal ones, code that rejects everything passes",
			"asserting an error message string rather than the refusal and the unchanged state",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueStateMachine, planEntity.TechniqueTable},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{StateMachine: true},
		Severity:   flowEntity.SevHigh,
		Source:     "Stripe PaymentIntent lifecycle; Braintree transaction statuses",
	},

	{
		ID:     "FINAL-STATE-IS-FINAL",
		Name:   "A payment in a final state cannot move again",
		Family: planEntity.FamilyState,
		CaseScenario: `For every state round one marked FINAL: drive a payment into it, then attempt
every other transition the machine declares. Each attempt must be refused and
the stored state must be unchanged.

Then do the opposite for the declared exceptions. For each one, drive the
payment into that state and make the exception transition happen. It must be
allowed.

Both halves are needed. Without the first, a payment can be captured and then
quietly set back to pending by a stray webhook. Without the second, the test
forbids a chargeback and someone deletes it the first time a real one arrives.`,
		LookingFor: "a status field written directly with no check that the payment is still open",
		Acceptance: []string{
			"every non-exception transition out of a final state is refused",
			"the stored state after a refused attempt equals the state before it",
			"every declared exception transition IS allowed",
			"the ledger is unchanged by a refused transition",
		},
		AntiGoals: []string{
			"treating a state with a declared exception as fully final; the exception exists because reality has that path",
			"testing only that final states refuse, with no exception case, which passes on code that freezes everything forever",
			"inferring the final list from an empty may_move_to entry, since that can also mean the agent could not work it out",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueStateMachine, planEntity.TechniqueTable},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{StateMachine: true},
		Severity:   flowEntity.SevCritical,
		Source:     "Stripe PaymentIntent lifecycle; Adyen CHARGEBACK and REFUNDED_REVERSED webhook codes",
	},

	{
		ID:     "FAILURE-IS-NOT-TERMINAL",
		Name:   "A failed payment can be retried",
		Family: planEntity.FamilyState,
		CaseScenario: `Drive a payment to failure through a declined card. Then attempt the payment
again with a valid method.

It must be possible. Stripe's PaymentIntent returns to requires_payment_method
after a failure precisely so the payment can be retried.

Implementations that treat failure as terminal look correct in every test until
a real customer's card is declined once and they can never pay.`,
		LookingFor: "a failed state modelled as terminal, stranding a customer who retries after a decline",
		Acceptance: []string{
			"after a decline, a second attempt with a valid method succeeds",
			"the successful retry produces exactly one money movement",
			"the failed attempt left no money movement behind",
		},
		AntiGoals: []string{
			"asserting the code's current behaviour — if it treats failure as terminal, this test SHOULD go red",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueStateMachine, planEntity.TechniqueFaultInjection},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{StateMachine: true, InjectableSeam: true},
		Severity:   flowEntity.SevHigh,
		Source:     "Stripe PaymentIntent lifecycle: status returns to requires_payment_method",
	},

	// ── ordering ───────────────────────────────────────────────────────────

	{
		ID:     "WEBHOOK-ORDER-INDEPENDENT",
		Name:   "Provider events applied in any order reach the same state",
		Family: planEntity.FamilyOrdering,
		CaseScenario: `Take the set of provider events for one payment — authorized, captured,
refunded, or whatever this system handles. Apply them in every permutation.
Then apply some of them twice.

Every permutation must reach the same final state, and duplicate delivery must
change nothing.

Providers explicitly do not guarantee webhook ordering, and delivery is
at-least-once. A handler that assumes order works until the day two events
arrive within the same second.

One caution: do not assume this property holds everywhere. Nubank documents a
real case where ordering genuinely changes the outcome — a late payment
exceeding the amount owed should create a prepaid balance, not a negative one.
If this system has such a case, this scenario does not apply to it.`,
		LookingFor: "an event handler that depends on arrival order, or double-applies a redelivered event",
		Acceptance: []string{
			"all permutations of the event set converge to the same final state",
			"applying any event twice leaves the state unchanged",
			"the ledger total is the same across every permutation",
		},
		AntiGoals: []string{
			"asserting order-independence where the domain is genuinely order-dependent; check first, and if it is, write an ordering-sensitive test instead",
			"testing only the happy order plus one reversal, rather than the permutations",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueMetamorphic, planEntity.TechniqueProperty},
		Oracle:     planEntity.OracleMetamorphic,
		Requires:   planEntity.Requires{MultipleEntries: true, StateMachine: true},
		Severity:   flowEntity.SevHigh,
		Source:     "Monzo Stand-in advice syncing, order-tolerant; Nubank ordering counterexample",
	},

	{
		ID:     "AT-LEAST-ONCE-DELIVERY-IS-SAFE",
		Name:   "Delivering every message twice changes nothing",
		Family: planEntity.FamilyOrdering,
		CaseScenario: `Run the flow, capture every message it publishes, and deliver each one to its
consumer a second time.

Downstream state must be identical after the duplicates.

This is the test that makes at-least-once delivery acceptable. Every broker in
production is at-least-once; the question is never whether duplicates arrive,
only whether they are harmless when they do.`,
		LookingFor: "a consumer that applies an effect per message rather than per unique event",
		Acceptance: []string{
			"downstream state after double delivery equals state after single delivery",
			"the ledger total is unchanged by the duplicates",
		},
		AntiGoals: []string{
			"asserting the consumer detected the duplicate; it may legitimately reprocess, as long as the effect is the same",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueMetamorphic, planEntity.TechniqueFaultInjection},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{SeamKinds: []flowEntity.SeamKind{flowEntity.SeamQueue}, InjectableSeam: true},
		Severity:   flowEntity.SevHigh,
		Source:     "brandur.org/job-drain; Wise tw-tkms outbox",
	},

	// ── money arithmetic ───────────────────────────────────────────────────

	{
		ID:     "MONEY-ROUND-TRIP-EXACT",
		Name:   "An amount survives parse, format and storage exactly",
		Family: planEntity.FamilyMoney,
		CaseScenario: `Fuzz an amount through every conversion the codebase performs — parse, format,
serialise, store, read back — and assert the value that comes out equals the
value that went in, exactly.

If any float touches that path, the fuzzer finds it within seconds and hands
back a concrete failing input. That turns a static warning about float64 into a
reproducible defect with a number attached, which is the difference between a
report someone argues with and one they fix.`,
		LookingFor: "precision lost in a conversion, so summing many line items drifts from the true total",
		Acceptance: []string{
			"the round-tripped value equals the input exactly, with no tolerance",
			"the fuzz corpus is seeded with values that expose binary floating point: 0.1, 0.07, 1e15+1, and the largest amount the domain allows",
		},
		AntiGoals: []string{
			"comparing with an epsilon tolerance, which is how the bug is normally hidden rather than found",
			"testing only round numbers, which survive float arithmetic and prove nothing",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueFuzz, planEntity.TechniqueProperty},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{MoneyFlows: true},
		Severity:   flowEntity.SevCritical,
		Source:     "Stripe currencies: all amounts in minor units",
	},

	{
		ID:     "MINOR-UNIT-CONVERSION",
		Name:   "Currency exponents are right, including the ones that break the rule",
		Family: planEntity.FamilyMoney,
		CaseScenario: `Table test over currencies with different exponents, and specifically over the
ones that are exceptions:

  USD 2, JPY 0, BHD 3 (three decimals), KWD 3
  ISK and UGX  — moved to zero-decimal, but compatibility requires two decimals
                 that are always 00, so 5 ISK is 500
  HUF and TWD  — charged as two-decimal, paid out as zero-decimal, so payouts
                 must be divisible by 100
  CLP, CVE, IDR — Adyen's exponent DIFFERS from ISO 4217

Those last rows are the ones that catch real bugs, because a developer reaches
for a generic ISO 4217 table and it is wrong for exactly these.`,
		LookingFor: "a single hardcoded exponent of 2, or an ISO table used where the provider's differs",
		Acceptance: []string{
			"each currency converts to and from minor units correctly",
			"the ISO-deviating currencies are present as rows",
			"an unknown currency code is rejected rather than defaulting to 2",
		},
		AntiGoals: []string{
			"generating the expected values by calling the code under test",
			"testing only USD and EUR, which share the common case and hide every exception",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueTable},
		Oracle:     planEntity.OracleSpecification,
		Requires:   planEntity.Requires{MoneyFlows: true},
		Severity:   flowEntity.SevHigh,
		Source:     "Stripe currencies; Adyen currency codes, where four currencies deviate from ISO 4217",
	},

	{
		ID:     "SPLIT-SUMS-TO-TOTAL",
		Name:   "Splitting an amount loses no cents",
		Family: planEntity.FamilyMoney,
		CaseScenario: `Generate an amount and a number of parts. Split it. Sum the parts.

The sum must equal the original exactly, and the parts must differ by at most
one minor unit.

100 split three ways is 34, 33, 33 — not 33, 33, 33. The remainder has to go
somewhere, and the version of this code that drops it produces a ledger that
will not balance at month end, from an error too small for anyone to notice in a
single transaction.`,
		LookingFor: "integer division that truncates, discarding the remainder",
		Acceptance: []string{
			"the parts sum exactly to the original amount for every generated input",
			"no two parts differ by more than one minor unit",
			"a negative amount or a zero part count is rejected rather than producing nonsense",
		},
		AntiGoals: []string{
			"testing only amounts that divide evenly, which is the case that always works",
			"allowing a tolerance on the sum",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueProperty, planEntity.TechniqueTable},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{MoneyFlows: true},
		Severity:   flowEntity.SevHigh,
		Source:     "Stripe UGX rounding with the difference credited to the customer balance",
	},

	{
		ID:     "CURRENCY-MIXING-REFUSED",
		Name:   "Amounts in different currencies cannot be combined",
		Family: planEntity.FamilyMoney,
		CaseScenario: `Attempt to add, compare and net amounts whose currencies differ.

Every one must be refused. An amount without a currency is not money, it is a
number, and nothing stops a caller adding EUR minor units to USD minor units if
the type system is not doing that work.`,
		LookingFor: "arithmetic on bare integers where the currency lives in a separate field nobody checks",
		Acceptance: []string{
			"adding, subtracting or comparing across currencies returns an error or panics deliberately",
			"same-currency arithmetic still works, proving the check is selective",
		},
		AntiGoals: []string{
			"skipping this because the type has no currency field — that absence IS the finding, and the test should say so",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueTable, planEntity.TechniqueProperty},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{MoneyFlows: true},
		Severity:   flowEntity.SevHigh,
		Source:     "Adyen and Stripe both require currency alongside every amount",
	},

	// ── reconciliation ─────────────────────────────────────────────────────

	{
		ID:     "RECONCILER-IS-IDEMPOTENT",
		Name:   "Running the reconciler twice changes nothing the second time",
		Family: planEntity.FamilyConsistency,
		CaseScenario: `Seed deliberately divergent state. Run the reconciliation job. Assert it
converged. Then run it AGAIN and assert nothing changed.

The second run is the assertion people forget, and repair jobs that
double-correct are a real bug class: the first pass fixes a missing entry, the
second pass adds it again because it is looking at stale criteria.`,
		LookingFor: "a repair job that applies its correction every time it runs",
		Acceptance: []string{
			"after the first run, state matches the expected reconciled state",
			"the second run produces no writes at all",
			"the ledger total is identical after both runs",
		},
		AntiGoals: []string{
			"asserting only that the first run converged, which is the half that usually works",
			"letting the reconciler mutate ledger entries directly rather than appending compensating ones",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueNarrowIntegration, planEntity.TechniqueFaultInjection},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{MultipleEntries: true},
		Severity:   flowEntity.SevHigh,
		Source:     "Monzo coherence services; Shopify anomaly remediation; Starling catch-up processing",
	},

	{
		ID:     "ASYNC-RESPONSE-BEFORE-DURABILITY",
		Name:   "Success is not reported before the write is durable",
		Family: planEntity.FamilyBoundary,
		CaseScenario: `Make the durable write fail, and check what the caller was told.

If the handler starts a goroutine and returns 200 before that goroutine
commits, the caller believes the payment succeeded and there is no record of it.
Phase 1 flags a handler that spawns a goroutine; this is the runtime version.

Starling's rule is the fix: persist the work item BEFORE processing it, so a
crash leaves something for the catch-up job to find.`,
		LookingFor: "a success response returned before the state that justifies it is committed",
		Acceptance: []string{
			"when the durable write fails, the caller does not receive success",
			"or, if the response is deliberately optimistic, a durable record exists that a retry or catch-up job can act on",
		},
		AntiGoals: []string{
			"asserting on response timing rather than on the ordering of the effects",
			"using a sleep to wait for the goroutine; synchronise on a channel the fake closes",
		},
		Techniques: []planEntity.Technique{planEntity.TechniqueFaultInjection, planEntity.TechniqueConcurrency},
		Oracle:     planEntity.OracleInvariant,
		Requires:   planEntity.Requires{Goroutine: true, InjectableSeam: true},
		Severity:   flowEntity.SevCritical,
		Source:     "Starling: persist work items before processing; Uber writeback ordering",
	},
}

// ByID returns a scenario from the catalog.
func ByID(id string) (planEntity.Scenario, bool) {
	for _, s := range Catalog {
		if s.ID == id {
			return s, true
		}
	}
	return planEntity.Scenario{}, false
}
