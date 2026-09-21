package testPlan

import (
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

var Catalog = []Scenario{

	{
		ID:     "IDEM-REPLAY",
		Name:   "A retry with the same key replays the first result",
		Family: FamilyIdempotency,
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
		Techniques: []Technique{TechniqueFaultInjection, TechniqueUnit},
		Oracle:     OracleSpecification,
		Requires:   Requires{IdempotencyKey: true, InjectableSeam: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "IDEM-PAYLOAD-MISMATCH",
		Name:   "The same key with different parameters is refused",
		Family: FamilyIdempotency,
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
		Techniques: []Technique{TechniqueFaultInjection, TechniqueUnit},
		Oracle:     OracleSpecification,
		Requires:   Requires{IdempotencyKey: true, InjectableSeam: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "IDEM-CONCURRENT",
		Name:   "Two simultaneous requests with one key produce one money movement",
		Family: FamilyIdempotency,
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
		Techniques: []Technique{TechniqueConcurrency, TechniqueNarrowIntegration},
		Oracle:     OracleSpecification,
		Requires:   Requires{IdempotencyKey: true, InjectableSeam: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "IDEM-CRASH-AT-STEP",
		Name:   "A crash at any step, then a retry, ends where a clean run ends",
		Family: FamilyIdempotency,
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
		Techniques: []Technique{TechniqueFaultInjection},
		Oracle:     OracleSpecification,
		Requires:   Requires{IdempotencyKey: true, InjectableSeam: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "TIMEOUT-UNKNOWN-OUTCOME",
		Name:   "A provider timeout is not treated as a failure",
		Family: FamilyFailure,
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
		Techniques: []Technique{TechniqueFaultInjection},
		Oracle:     OracleSpecification,
		Requires:   Requires{InjectableSeam: true, SeamKinds: []flowEntity.SeamKind{flowEntity.SeamHTTP, flowEntity.SeamQueue}},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "ORPHANED-AUTHORIZATION",
		Name:   "The provider is never committed without a local record",
		Family: FamilyFailure,
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
		Techniques: []Technique{TechniqueFaultInjection},
		Oracle:     OracleInvariant,
		Requires:   Requires{InjectableSeam: true, OpensTx: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "UNCLASSIFIED-ERROR-NOT-RETRIED",
		Name:   "An unrecognised error is not retried",
		Family: FamilyFailure,
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
		Techniques: []Technique{TechniqueTable, TechniqueFaultInjection},
		Oracle:     OracleSpecification,
		Requires:   Requires{InjectableSeam: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "CONSERVATION-UNDER-CONCURRENCY",
		Name:   "Money is conserved while transfers run in parallel",
		Family: FamilyConsistency,
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
		Techniques: []Technique{TechniqueNarrowIntegration, TechniqueProperty, TechniqueConcurrency},
		Oracle:     OracleInvariant,
		Requires:   Requires{BalanceFunc: true, MoneyFlows: true, RealDatabase: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "LOST-UPDATE",
		Name:   "Two concurrent debits on one account both take effect",
		Family: FamilyConsistency,
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
		Techniques: []Technique{TechniqueNarrowIntegration},
		Oracle:     OracleInvariant,
		Requires:   Requires{BalanceFunc: true, MoneyFlows: true, RealDatabase: true, SeamKinds: []flowEntity.SeamKind{flowEntity.SeamDB}},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "DOUBLE-ENTRY-SUMS-TO-ZERO",
		Name:   "Every set of ledger entries sums to zero",
		Family: FamilyConsistency,
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
		Techniques: []Technique{TechniqueProperty, TechniqueTable},
		Oracle:     OracleInvariant,
		Requires:   Requires{TransferFunc: true, MoneyFlows: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "LEDGER-OUTSIDE-CALLER-TRANSACTION",
		Name:   "A ledger write is rolled back with the transaction that caused it",
		Family: FamilyBoundary,
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
		Techniques: []Technique{TechniqueNarrowIntegration},
		Oracle:     OracleInvariant,
		Requires:   Requires{TransferFunc: true, OpensTx: true, RealDatabase: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "NETWORK-CALL-INSIDE-TRANSACTION",
		Name:   "No network call happens while a transaction is open",
		Family: FamilyBoundary,
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
		Techniques: []Technique{TechniqueFaultInjection, TechniqueUnit},
		Oracle:     OracleSpecification,
		Requires:   Requires{OpensTx: true, InjectableSeam: true, SeamKinds: []flowEntity.SeamKind{flowEntity.SeamHTTP, flowEntity.SeamQueue}},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "ILLEGAL-TRANSITION-REFUSED",
		Name:   "Transitions that should be impossible are refused",
		Family: FamilyState,
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
		Techniques: []Technique{TechniqueStateMachine, TechniqueTable},
		Oracle:     OracleSpecification,
		Requires:   Requires{StateMachine: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "FINAL-STATE-IS-FINAL",
		Name:   "A payment in a final state cannot move again",
		Family: FamilyState,
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
		Techniques: []Technique{TechniqueStateMachine, TechniqueTable},
		Oracle:     OracleSpecification,
		Requires:   Requires{StateMachine: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "FAILURE-IS-NOT-TERMINAL",
		Name:   "A failed payment can be retried",
		Family: FamilyState,
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
		Techniques: []Technique{TechniqueStateMachine, TechniqueFaultInjection},
		Oracle:     OracleSpecification,
		Requires:   Requires{StateMachine: true, InjectableSeam: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "WEBHOOK-ORDER-INDEPENDENT",
		Name:   "Provider events applied in any order reach the same state",
		Family: FamilyOrdering,
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
		Techniques: []Technique{TechniqueMetamorphic, TechniqueProperty},
		Oracle:     OracleMetamorphic,
		Requires:   Requires{MultipleEntries: true, StateMachine: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "AT-LEAST-ONCE-DELIVERY-IS-SAFE",
		Name:   "Delivering every message twice changes nothing",
		Family: FamilyOrdering,
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
		Techniques: []Technique{TechniqueMetamorphic, TechniqueFaultInjection},
		Oracle:     OracleInvariant,
		Requires:   Requires{SeamKinds: []flowEntity.SeamKind{flowEntity.SeamQueue}, InjectableSeam: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "MONEY-ROUND-TRIP-EXACT",
		Name:   "An amount survives parse, format and storage exactly",
		Family: FamilyMoney,
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
		Techniques: []Technique{TechniqueFuzz, TechniqueProperty},
		Oracle:     OracleInvariant,
		Requires:   Requires{MoneyFlows: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "MINOR-UNIT-CONVERSION",
		Name:   "Currency exponents are right, including the ones that break the rule",
		Family: FamilyMoney,
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
		Techniques: []Technique{TechniqueTable},
		Oracle:     OracleSpecification,
		Requires:   Requires{MoneyFlows: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "SPLIT-SUMS-TO-TOTAL",
		Name:   "Splitting an amount loses no cents",
		Family: FamilyMoney,
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
		Techniques: []Technique{TechniqueProperty, TechniqueTable},
		Oracle:     OracleInvariant,
		Requires:   Requires{MoneyFlows: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "CURRENCY-MIXING-REFUSED",
		Name:   "Amounts in different currencies cannot be combined",
		Family: FamilyMoney,
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
		Techniques: []Technique{TechniqueTable, TechniqueProperty},
		Oracle:     OracleInvariant,
		Requires:   Requires{MoneyFlows: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "RECONCILER-IS-IDEMPOTENT",
		Name:   "Running the reconciler twice changes nothing the second time",
		Family: FamilyConsistency,
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
		Techniques: []Technique{TechniqueNarrowIntegration, TechniqueFaultInjection},
		Oracle:     OracleInvariant,
		Requires:   Requires{MultipleEntries: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "ASYNC-RESPONSE-BEFORE-DURABILITY",
		Name:   "Success is not reported before the write is durable",
		Family: FamilyBoundary,
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
		Techniques: []Technique{TechniqueFaultInjection, TechniqueConcurrency},
		Oracle:     OracleInvariant,
		Requires:   Requires{Goroutine: true, InjectableSeam: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "BALANCE-NEVER-NEGATIVE",
		Name:   "A balance cannot be driven below zero",
		Family: FamilyMoney,
		CaseScenario: `Take a wallet holding a known balance. Issue enough concurrent debits that
their total exceeds it, and let them race.

Exactly as many must succeed as the balance can cover. The rest must be
refused, and the final balance must never be negative at any point a reader
could observe it.

This is the defining invariant of a wallet. A system that checks the balance
and then writes it in a separate statement lets two debits both pass the check
before either writes, and the customer spends money that was not there.`,
		LookingFor: "a read-then-write balance check that two concurrent debits can both pass",
		Acceptance: []string{
			"the number of successful debits never exceeds what the starting balance covers",
			"the final balance is zero or positive",
			"every refused debit leaves no transaction row behind",
			"the check and the write happen under one lock or one atomic statement",
		},
		AntiGoals: []string{
			"asserting only the final balance; a balance that dipped negative and recovered still allowed an overdraft",
			"checking the balance in Go before the transaction and calling that the guard",
			"running the debits sequentially, which cannot expose the race at all",
			"treating a post-hoc `if balance < 0 { rollback }` as prevention when the row was already written",
		},
		Techniques: []Technique{TechniqueConcurrency, TechniqueNarrowIntegration},
		Oracle:     OracleInvariant,
		Requires:   Requires{MoneyFlows: true, BalanceFunc: true, OpensTx: true, RealDatabase: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "TRANSFER-IS-ATOMIC",
		Name:   "Both legs of a transfer happen, or neither does",
		Family: FamilyConsistency,
		CaseScenario: `Transfer between two wallets, and make the second leg fail — the credit
errors, or the process dies between the debit and the credit.

The debit must not survive. Total money across both wallets must be identical
before and after.

A wallet transfer is two writes pretending to be one. If they are not in the
same transaction, or the transaction is committed between them, money is
destroyed and no error is reported to anyone.`,
		LookingFor: "a debit that commits without its matching credit",
		Acceptance: []string{
			"the sum of both balances is unchanged after the failure",
			"neither a debit row nor a credit row remains",
			"the caller receives an error rather than a success",
		},
		AntiGoals: []string{
			"faking the repository so both writes succeed, which tests nothing about atomicity",
			"asserting only that an error was returned, without checking the balances",
			"injecting the failure before the debit, where there is nothing to roll back",
		},
		Techniques: []Technique{TechniqueFaultInjection, TechniqueNarrowIntegration},
		Oracle:     OracleInvariant,
		Requires:   Requires{TransferFunc: true, OpensTx: true, InjectableSeam: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "DEADLOCK-IS-RECOVERED",
		Name:   "A deadlock between two transfers is retried, not corrupted",
		Family: FamilyConsistency,
		CaseScenario: `Run two transfers concurrently that touch the same two accounts in opposite
order — one A to B, the other B to A — against a real database, enough times
that Postgres's own deadlock detector eventually kills one of them.

This is not a test that the deadlock never happens. Locking two rows in
opposite orders across concurrent transactions WILL deadlock under real load;
that is expected, documented database behaviour, not a bug to prevent. The
bug this scenario finds is what happens next: the losing transaction must not
leave a half-applied debit, and the deadlock must not reach the caller as a
raw, unrecognised driver error it has no name for.

There are two legitimate fixes, and this scenario accepts either. One is to
always acquire the account locks in the same fixed order (sort the account
identifiers before locking) so the deadlock cannot occur in the first place.
The other is to catch the database's specific deadlock/serialization error
and retry the whole operation. What fails this test is neither: the deadlock
surfaces unhandled, or the account is left with one leg of the transfer
applied and the other lost.`,
		LookingFor: "a transaction that locks two or more account rows with no fixed ordering, and no retry around the whole operation when the driver reports a deadlock",
		Acceptance: []string{
			"both transfers eventually succeed, or the one that lost the deadlock returns a typed, retryable error rather than a raw driver error",
			"the account that lost the deadlock has no partially-applied write — its balance reflects only committed transfers",
			"running the same two transfers many times never leaves the two accounts' combined balance different from before",
		},
		AntiGoals: []string{
			"asserting only that no panic occurred, which passes even if one transfer silently vanishes",
			"running every transfer in the same account order, which can never trigger the deadlock this scenario exists to find",
			"treating the retry itself as the bug — a caught-and-retried deadlock is the correct outcome, not a failure",
		},
		Techniques: []Technique{TechniqueConcurrency, TechniqueNarrowIntegration},
		Oracle:     OracleInvariant,
		Requires:   Requires{OpensTx: true, RealDatabase: true, TransferFunc: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "TENANT-ROWS-DONT-LEAK",
		Name:   "One tenant's rows never answer for another's",
		Family: FamilyConsistency,
		CaseScenario: `The schema already scopes uniqueness by a discriminator column shared across
several tables — the same shape as formancehq/ledger's buckets, where
"create unique index ... on logs (ledger, idempotency_key)" lets two
different ledgers each have their own row keyed "idempotency_key = abc",
because the ledger column is part of what makes a row unique, not the whole
of it.

Seed two tenants with a row carrying the SAME business key value in each —
that only works at all because the schema allows it, which is what proves
this is really a multi-tenant table and not a coincidence. Then call this
tenant's own read path — the function every caller actually goes through,
not a hand-written query — asking for the other tenant's business key.

It must come back empty or not-found. It must never return the other
tenant's row. The schema being right is not evidence the code is: the
discriminator column already exists and evidently scopes uniqueness: the bug
this looks for is a query that was written before that column existed, or
copied from one that never had it, filtering on the business key alone.`,
		LookingFor: "a query on a shared table that filters by business key alone, with no discriminator column in its WHERE clause",
		Acceptance: []string{
			"a query scoped to tenant B never returns a row that belongs to tenant A, even though both share the same business key value",
			"the read is made through the application's own repository/query function, not a query built just for this test",
			"the negative case is checked too: tenant A's own query for its own business key still succeeds",
		},
		AntiGoals: []string{
			"using two tenants with different business keys, where a missing discriminator column would still happen to return the right row",
			"querying the database directly instead of through the code path every real caller uses, which proves the schema is fine but not that the code uses it",
			"assuming a NOT NULL or foreign key on the discriminator column is enough; neither one stops a WHERE clause that simply omits it",
		},
		Techniques: []Technique{TechniqueNarrowIntegration},
		Oracle:     OracleInvariant,
		Requires:   Requires{MultiTenant: true, RealDatabase: true},
		Severity:   flowEntity.SevCritical,
	},

	{
		ID:     "SELF-TRANSFER-REFUSED",
		Name:   "A wallet cannot transfer to itself",
		Family: FamilyMoney,
		CaseScenario: `Call the transfer path with the same wallet as both source and destination.

It must be refused before any write. If it is allowed, the balance must be
exactly unchanged and exactly one pair of offsetting rows must exist.

Two things go wrong when this is unguarded. If the code loads the source and
destination rows separately, it writes the same row twice and the second write
overwrites the first, inventing or destroying money. If it locks both rows in
order, it deadlocks against itself.`,
		LookingFor: "a same-wallet transfer that changes the balance or deadlocks",
		Acceptance: []string{
			"the request is refused, or the balance is provably unchanged",
			"no deadlock or timeout occurs",
			"the fee, if any, is not charged twice",
		},
		AntiGoals: []string{
			"asserting whatever the code does today rather than that money is conserved",
			"only testing two distinct wallets, which never reaches this path",
		},
		Techniques: []Technique{TechniqueTable, TechniqueUnit},
		Oracle:     OracleInvariant,
		Requires:   Requires{TransferFunc: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "BALANCE-LIMIT-ENFORCED",
		Name:   "A ceiling on the balance holds under concurrent credits",
		Family: FamilyBoundary,
		CaseScenario: `Where a maximum balance or a per-transaction limit exists, top the wallet up
to just under the ceiling, then issue concurrent deposits that would each fit
alone but together exceed it.

The ceiling must hold. Only deposits that genuinely fit may succeed.

A limit checked with a read and enforced with a later write is not a limit. It
is a suggestion that holds right up until two requests arrive together, which
is exactly when a regulatory balance cap matters.`,
		LookingFor: "a limit check that concurrent deposits can both pass before either writes",
		Acceptance: []string{
			"the final balance never exceeds the configured ceiling",
			"rejected deposits are rejected with the limit error, not a generic failure",
			"a deposit that exactly reaches the ceiling is allowed",
		},
		AntiGoals: []string{
			"testing one deposit at a time, which can never exceed the limit",
			"hard-coding the limit instead of reading the configured value, so the test passes when the config changes",
		},
		Techniques: []Technique{TechniqueConcurrency, TechniqueNarrowIntegration},
		Oracle:     OracleSpecification,
		Requires:   Requires{MoneyFlows: true, BalanceFunc: true, RealDatabase: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "WALLET-STATUS-GATES-MOVEMENT",
		Name:   "A disabled or deleted wallet refuses money",
		Family: FamilyState,
		CaseScenario: `For every non-active wallet status the code declares — disabled, deleted,
pending, blocked — attempt a credit and a debit.

Each must be refused, and the balance must be unchanged. Enumerate the states
from the source rather than picking the two obvious ones.

Systems usually guard the debit path and forget the credit path, so money can
be paid INTO a closed wallet and stranded there. A deleted wallet that still
accepts a deposit is money the customer cannot reach and the business cannot
account for.`,
		LookingFor: "a status that blocks withdrawals but still accepts deposits",
		Acceptance: []string{
			"every declared non-active status refuses both directions",
			"the balance is unchanged after each refused attempt",
			"the refusal names the status rather than returning a generic error",
		},
		AntiGoals: []string{
			"testing only the active and deleted states and skipping the ones in between",
			"asserting only the debit direction, which is the one that is usually already guarded",
		},
		Techniques: []Technique{TechniqueTable, TechniqueStateMachine},
		Oracle:     OracleSpecification,
		Requires:   Requires{StateMachine: true, MoneyFlows: true},
		Severity:   flowEntity.SevHigh,
	},

	{
		ID:     "READ-MODIFY-WRITE-NEEDS-A-LOCK",
		Name:   "A row read then written back must be locked in between",
		Family: FamilyConsistency,
		CaseScenario: `This repository reads a row, changes the value in Go, then writes the whole
row back. Run two of those at once against the same row.

Both must not succeed with the second silently discarding the first one's
change. Either one waits for the other, or one fails and retries.

The window between the read and the write is the bug. Two requests can both
read the same starting value, both compute from it, and the second write wins.
Nothing errors. The first change is simply gone, and no log records that it
happened.`,
		LookingFor: "two writers that both read the same value and the later write erasing the earlier one",
		Acceptance: []string{
			"after both finish, the row reflects both changes, or exactly one of them was refused",
			"the test shows which line read the value and which line wrote it back",
			"a row lock, a version column, or an atomic statement is what makes it pass — not timing",
		},
		AntiGoals: []string{
			"running the two writers one after the other, which cannot reproduce it",
			"adding a sleep to force the order, which tests the sleep rather than the code",
			"asserting no error was returned; the whole point is that this loses data quietly",
		},
		Techniques: []Technique{TechniqueConcurrency, TechniqueNarrowIntegration},
		Oracle:     OracleInvariant,
		Requires: Requires{RealDatabase: true, OpensTx: true,
			WritePatterns: []flowEntity.WritePattern{flowEntity.WriteUpdateInPlace}},
		Severity: flowEntity.SevCritical,
	},

	{
		ID:     "APPEND-ONLY-HISTORY-IS-IMMUTABLE",
		Name:   "Rows that are only ever added are never quietly changed",
		Family: FamilyConsistency,
		CaseScenario: `This table is only written by inserts. Nothing in the repository updates or
deletes a row once it exists.

Prove that stays true. Try to correct a value the way a caller would, and
show the correction arrives as a NEW row, with the old one still readable.

An append-only table is the audit trail. Its whole value is that yesterday's
answer is still there today. The moment one code path updates a row in place,
the history silently stops being a history, and no test notices because every
read still returns something sensible.`,
		LookingFor: "a path that edits or removes a row in a table the rest of the code treats as history",
		Acceptance: []string{
			"after a correction, both the original row and the correcting row are readable",
			"the row count only ever grows",
			"reading the state at an earlier point in time still gives the earlier answer",
		},
		AntiGoals: []string{
			"only asserting the latest value, which passes whether or not history was kept",
			"asserting the row count grew, without checking the original row is unchanged",
		},
		Techniques: []Technique{TechniqueNarrowIntegration, TechniqueProperty},
		Oracle:     OracleInvariant,
		Requires: Requires{RealDatabase: true,
			WritePatterns: []flowEntity.WritePattern{flowEntity.WriteInsertOnly}},
		Severity: flowEntity.SevHigh,
	},

	{
		ID:     "UPSERT-HIDES-A-SECOND-EFFECT",
		Name:   "An insert that turns into an update must not run the work twice",
		Family: FamilyIdempotency,
		CaseScenario: `This table is written with an insert that falls back to an update when the
row already exists. Send the same request twice.

The row must end up correct, and whatever the request DOES besides writing
that row — charging a card, sending a message, moving a balance — must happen
exactly once.

An upsert makes the database call safe to repeat. It does not make the rest of
the function safe to repeat. The row looks right afterwards, so the test
passes, while the second call already sent the second charge.`,
		LookingFor: "a repeated request whose row write is absorbed by the conflict clause while its side effect runs again",
		Acceptance: []string{
			"the external call is made exactly once across both requests",
			"the row holds the first request's result, not a blend of both",
			"the second caller is told it was a repeat rather than being given a fresh success",
		},
		AntiGoals: []string{
			"asserting only the row is correct — that is exactly what the upsert guarantees and it proves nothing",
			"counting rows instead of counting the side effect",
		},
		Techniques: []Technique{TechniqueFaultInjection, TechniqueNarrowIntegration},
		Oracle:     OracleSpecification,
		Requires: Requires{RealDatabase: true, InjectableSeam: true,
			WritePatterns: []flowEntity.WritePattern{flowEntity.WriteUpsert}},
		Severity: flowEntity.SevCritical,
	},
}
