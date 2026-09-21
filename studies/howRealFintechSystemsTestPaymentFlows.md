# How Real Fintech Systems Test Payment Flows

This document summarizes the research behind `testigo` Phase 2.

The goal is simple:

> Learn from real payment systems what must be tested, then turn those lessons into testable rules for `testigo`.

All findings below are based on primary or first-hand sources. When we could not find reliable evidence for something, we say so instead of guessing.

---

## 1. Idempotency

Payment systems must handle retries safely.

`Stripe` publishes a clear idempotency contract that can be used directly as a test oracle:

* The result of the first request is saved and returned for later requests with the same idempotency key.
* This is true even when the first request returned `500`.
* Reusing a key with different parameters is an error.
* Keys can be up to 255 characters.
* Keys are normally removed after about 24 hours.
* If execution never started, no result is saved.

Sources:

* [Stripe — Idempotent requests](https://docs.stripe.com/api/idempotent_requests)
* [GoCardless — Idempotency keys](https://gocardless.com/blog/idempotency-keys)

### What this means for `testigo`

A good generated test should not only test:

```text
request → success
```

It should also test:

```text
request
  → success
  → same request again
  → same result
  → no second payment
```

And:

```text
request with key X
  → request with key X + different parameters
  → error
```

---

## 2. Idempotency Is More Than Caching a Response

`Brandur Leach`, formerly of `Stripe`, describes a more advanced approach.

A long-running operation is split into recovery points:

```text
started
   ↓
ride_created
   ↓
charge_created
   ↓
finished
```

If the process crashes, the next attempt can continue from the last completed point instead of starting everything again.

The design also separates local database work from external calls:

* Local work is grouped into atomic phases.
* The phases use `SERIALIZABLE` transactions.
* External network calls are not made inside those phases.
* A `locked_at` lease prevents concurrent execution of the same operation.

Sources:

* [Brandur Leach — Idempotency Keys](https://brandur.org/idempotency-keys)
* [Rocket Rides reference implementation](https://github.com/brandur/rocket-rides-atomic)

### What this means for `testigo`

Failures should happen **in the middle of a workflow**, not only before or after it.

For example:

```text
create payment
    ↓
save payment
    ↓
call provider
    ↓
CRASH
```

The retry should not create another payment.

---

## 3. Airbnb: Separate the Work Around RPCs

`Airbnb` built a similar idea into `Orpheus`.

The work is split into:

```text
pre-RPC
   ↓
RPC
   ↓
post-RPC
```

The rules are strict:

* No network calls in `pre-RPC`.
* No network calls in `post-RPC`.
* No database calls in `RPC`.

One important detail is where idempotency data is stored.

`Airbnb` keeps it on the master, not on replicas.

Why?

Imagine:

```text
Request
  ↓
Master records payment
  ↓
Replica has not caught up yet
  ↓
Retry reads replica
  ↓
"Payment does not exist"
  ↓
Payment happens again
```

Replica lag can therefore become a money correctness bug.

`Airbnb` also classifies errors as retryable or non-retryable. Unknown errors are treated as non-retryable by default.

The reason is simple:

> A wrong retry can create a second payment.

Source:

* [Airbnb — Avoiding Double Payments in a Distributed Payments System](https://medium.com/airbnb-engineering/avoiding-double-payments-in-a-distributed-payments-system-2981f6b070bb)

---

## 4. Money Must Be Conserved

A payment system must not create or destroy money by accident.

`Uber` describes money movements as immutable money orders. The entries must add up to zero before the operation is written.

Example:

```text
Account A: -100
Account B: +100
----------------
Total:        0
```

This gives us a very useful invariant:

```text
total money before == total money after
```

Source:

* [Uber — Uber's Payments Platform](https://www.uber.com/us/en/blog/ubers-payments-platform/)

---

## 5. Immutable Ledgers

`Square`'s `Books` uses immutable double-entry accounting.

Old entries are not changed.

If a correction is needed, a new compensating entry is added.

For example:

```text
Original:
-100

Correction:
+20

Net:
-80
```

This keeps the full history.

`Books` also uses a monotonic version number for each book.

Useful properties to test include:

```text
old entries never change
```

and:

```text
new version > old version
```

Source:

* [Square — Books](https://developer.squareup.com/blog/books-an-immutable-double-entry-accounting-database-service/)

---

## 6. Property-Based Testing

`Nubank` has publicly described using generative testing for ledger logic.

Instead of writing a small number of fixed scenarios, the system generates many random states and operations.

For example:

```text
random initial state
        ↓
random operations
        ↓
run thousands of cases
        ↓
check invariants
```

The important part is not the random data itself.

The important part is that the system checks properties that must always be true.

For example:

```text
total money is unchanged
balance is never invalid
ledger history is consistent
```

Source:

* [InfoQ — Nubank Architecture](https://www.infoq.com/presentations/nubank-architecture/)

### What this means for `testigo`

Instead of generating only:

```text
Test transfer()
```

`testigo` should be able to generate:

```text
random transfer
random retry
random failure
random ordering
        ↓
check invariants
```

---

## 7. XRP Ledger: Turn Invariants Into Tests

`XRP Ledger` has named invariants that are checked after transactions and before commit.

If an invariant fails, the transaction has no effect.

This gives us a useful testing pattern:

```text
invariant
    ↓
generate a mutation that breaks it
    ↓
run mutation
    ↓
assert rejection
```

For example:

```text
Invariant:
balance must not become negative

Generated test:
withdraw more than the available balance

Expected:
transaction rejected
```

Source:

* [XRP Ledger — Invariant Checking](https://xrpl.org/docs/concepts/consensus-protocol/invariant-checking)

This is a strong model for `testigo`:

> For every important invariant, try to generate a case that breaks it.

---

## 8. Jepsen: Test Money While the System Is Running

`Jepsen` has a well-known `bank` workload.

It randomly transfers money between accounts while also reading account balances.

The basic properties are:

```text
sum(all balances) == constant
balance >= 0
```

But there is an important detail:

The system checks balances **during execution**, not only at the end.

This matters.

A broken system may eventually return to a correct final state:

```text
initial:
A=100
B=100

during:
A=80
B=130

final:
A=100
B=100
```

A test that only checks the final state misses the problem.

Source:

* [Jepsen — Bank workload](https://jepsen-io.github.io/jepsen/jepsen.tests.bank.html)

### What this means for `testigo`

Important properties should be checked after operations, not only after the whole test finishes.

---

## 9. Failure Injection

Real payment systems assume that failures will happen.

`Starling Bank` has described running chaos experiments in production.

Their systems are designed around persistent work:

* Every entity has a UUID.
* Work is saved before processing.
* Services check whether a step already ran before running it again.

Source:

* [InfoQ — Starling Bank](https://www.infoq.com/presentations/starling-bank/)

This leads to an important testing rule:

> Test what happens when a failure happens after the work has already started.

Not only:

```text
request → failure
```

but:

```text
request
  ↓
some work completed
  ↓
failure
  ↓
retry
```

---

## 10. Network Failures

`Shopify` created `Toxiproxy` to make network failures easy to test.

It sits between the service and another service:

```text
Your service
     ↓
 Toxiproxy
     ↓
Provider
```

It can simulate things such as:

* connection failures
* latency
* dropped connections
* network interruptions

Source:

* [Toxiproxy](https://github.com/Shopify/toxiproxy)

This is useful for payment tests because external calls are one of the most dangerous failure points.

---

## 11. Timeout Does Not Mean Failure

This is one of the most important rules in payment testing.

Consider:

```text
Your service
     ↓
Provider
     ↓
payment processed
     ↓
response lost
```

Your service sees:

```text
timeout
```

But the payment may already exist.

So:

```text
timeout != failure
```

A timeout means:

> We do not know what happened.

There are at least two possible outcomes:

```text
A:
request never reached provider
→ payment did not happen

B:
provider processed request
→ response was lost
→ payment did happen
```

If the client treats every timeout as failure and blindly retries, it may create a duplicate payment.

A critical generated scenario is therefore:

```text
send payment
    ↓
provider processes it
    ↓
response is lost
    ↓
client gets timeout
    ↓
client retries
    ↓
assert: only one payment exists
```

This is exactly the kind of scenario that normal success/failure tests miss.

---

## 12. Money Representation

Money should not normally be stored as floating-point numbers.

`Stripe` represents amounts using the currency's minor unit.

For example:

```text
USD:
$10 → 1000

JPY:
¥10 → 10
```

But currency rules are not always as simple as a generic ISO 4217 table.

Some currencies have special rules.

Examples from `Stripe` and `Adyen` include:

```text
ISK
UGX
HUF
TWD
CLP
CVE
IDR
```

`Adyen` also has some currency rules that differ from the usual ISO 4217 exponent.

For example:

```text
CLP → 2 decimals
CVE → 0 decimals
IDR → 0 decimals
ISK → 2 decimals
```

These are exactly the kinds of edge cases that can break a generic currency implementation.

Sources:

* [Stripe — Currencies](https://docs.stripe.com/currencies)
* [Adyen — Currency Codes](https://docs.adyen.com/development-resources/currency-codes)

### What this means for `testigo`

Currency should be part of generated test data.

Do not generate only:

```text
USD
EUR
```

Include currencies with unusual rules.

---

## 13. Payment State Machines

Payment systems are state machines.

`Stripe PaymentIntent` is a good example.

A failed payment is not always a final state.

For example:

```text
requires_payment_method
        ↓
    processing
        ↓
     succeeded
```

But failure can move the payment back:

```text
processing
    ↓
 failure
    ↓
requires_payment_method
```

So:

```text
failed != finished
```

A simple implementation may incorrectly treat every failure as terminal.

The bug may not appear until a customer tries the payment again.

Source:

* [Stripe — PaymentIntent lifecycle](https://docs.stripe.com/payments/paymentintents/lifecycle)

`Adyen` also exposes important state changes through webhook events such as:

```text
CAPTURE_FAILED
REFUND_FAILED
REFUNDED_REVERSED
```

These events matter because money can change state after the system thought the operation was finished.

---

## 14. Fuzzing

`TigerBeetle` describes several useful fuzzing patterns.

### Round-trip testing

Test:

```text
deserialize(serialize(x)) == x
```

This is useful for data structures and persistence formats.

### Negative-space testing

Start with valid data, slightly corrupt it, and make sure the system rejects it safely.

The expected result is:

```text
invalid input
    ↓
reject
```

not:

```text
invalid input
    ↓
panic
```

### Chaos testing

Call APIs in random orders and check invariants after every operation.

For example:

```text
capture
refund
retry
cancel
capture
...
```

The goal is to find cases that normal, carefully ordered tests never try.

Source:

* [TigerBeetle — A Tale of Four Fuzzers](https://tigerbeetle.com/blog/2025-11-28-tale-of-four-fuzzers/)

---

## 15. Swarm Testing

`Swarm testing` changes how test configurations are generated.

Instead of testing one configuration with every feature enabled, generate many configurations and randomly disable about half of the features in each one.

For example:

```text
Test 1: A B C F
Test 2: B C D
Test 3: A C E F G
Test 4: B F G
```

This sounds simple, but it can expose bugs much more often.

One study found a stack overflow in:

```text
uniform random testing:
1 in 370,000 tests
```

while swarm testing found it around:

```text
1 in 16 tests
```

The same study found more distinct compiler crash bugs with swarm testing.

Sources:

* [ISSTA 2012 — Swarm Testing](https://agroce.github.io/issta12.pdf)
* [TigerBeetle — Swarm Testing Data Structures](https://tigerbeetle.com/blog/2025-04-23-swarm-testing-data-structures/)

### What this means for `testigo`

Do not always generate tests with every option enabled.

Generate many different configurations.

This creates different paths through the system and increases the chance of finding unusual bugs.

---

## 16. Code Coverage Is Not Enough

`Antithesis` makes an important point about coverage.

A test can execute the code for a race condition without actually creating the race.

So:

```text
code coverage != scenario coverage
```

For example:

```text
goroutine A
goroutine B
```

may both run, but the scheduler may never create the problematic ordering.

The test is green, but the interesting situation never happened.

`testigo` therefore needs assertions that check whether the important situation actually occurred.

For example:

```text
assert:
two requests overlapped
```

not only:

```text
assert:
both request functions ran
```

Source:

* [Antithesis — Sometimes Assertions](https://antithesis.com/docs/best_practices/sometimes_assertions/)

---

## 17. Reference Models

`Jepsen`'s `TigerBeetle` analysis did not rely only on simple invariants.

It used a reference model of about 1,600 lines to compare the real system with expected behavior.

It also checked specific error codes and had a dedicated idempotency workload.

Source:

* [Jepsen — TigerBeetle 0.16.11](https://jepsen.io/analyses/tigerbeetle-0.16.11)

The basic idea is:

```text
             same operations
             /            \
            ↓              ↓
     Reference Model    Real System
            │              │
            └──── compare ─┘
```

This is powerful because the model does not need to implement the real system's internals.

It only needs to describe the correct behavior.

### What this means for `testigo`

For complex flows, generating expected output from a small reference model can be much stronger than writing expected values by hand.

---

## 18. Formal Methods

We looked for credible primary sources showing the use of:

```text
TLA+
Alloy
P
```

for payment or ledger logic at:

```text
Monzo
Revolut
Wise
N26
Starling
Nubank
Mercury
```

We did not find enough evidence to make that claim.

This does **not** mean these companies do not use formal methods.

It only means:

> We did not find a reliable public source that proves it.

The stronger evidence is for:

```text
reference models
invariants
property-based testing
generative testing
fault injection
reconciliation
```

So these are the techniques `testigo` should learn from first.

---

## 19. Two-Phase Commit

We also did not find evidence that `Monzo`, `Starling`, or `Uber` recommend `2PC` as the main solution for payment consistency.

The published designs instead focus more on:

```text
idempotent operations
+
strong consistency where needed
+
retries
+
catch-up jobs
+
reconciliation
```

So the useful finding is:

> Real payment systems tend to avoid making distributed transactions the center of their design.

This does not mean `2PC` is always wrong.

It means that the payment systems studied here solve these problems mainly with idempotency, durable state, retries, and reconciliation instead.

---

# What This Means for `testigo`

The research points to a different definition of a good payment test.

A basic test looks like:

```text
request
  ↓
success
  ↓
assert result
```

A production-level test asks much harder questions:

```text
What if the request is retried?

What if the provider processed it but the response was lost?

What if the process crashes halfway through?

What if two requests run at the same time?

What if a webhook arrives twice?

What if events arrive in an unexpected order?

What if a refund is later reversed?

What if the database and provider disagree?

What if the same idempotency key is used with different parameters?

What if an invariant is broken?

What if the system is restarted after part of the operation completed?
```

The main testing model becomes:

```text
                    Payment Flow
                         │
          ┌──────────────┼──────────────┐
          ↓              ↓              ↓
     State Machine   Idempotency   Money Invariants
          │              │              │
          └──────────────┼──────────────┘
                         ↓
                  Failure Injection
                         │
             ┌───────────┼───────────┐
             ↓           ↓           ↓
          timeout       crash      retry
             │           │           │
             └───────────┼───────────┘
                         ↓
                  Reference Model
                         │
                         ↓
              Property / Fuzz Testing
                         │
                         ↓
                   Invariants
```

The key idea is:

> `testigo` should not only test what the code does when everything goes well. It should generate situations that real payment systems must survive.

That means the valuable tests are not only:

```text
success
failure
```

but:

```text
success + retry
failure + retry
timeout + retry
crash + recovery
concurrent requests
duplicate webhook
out-of-order webhook
partial execution
random operation order
random configuration
invariant violation
```

The goal of Phase 2 is therefore not simply to generate **more tests**.

It is to generate tests around the **properties that must never break**.

For payment systems, the most important ones are:

```text
No duplicate money movement
No unexpected money creation or loss
Safe retries
Correct recovery after partial execution
Valid state transitions
Consistent ledger history
Safe handling of unknown outcomes
Correct behavior under concurrency
```

These properties are the real test oracles.

And that is the main lesson from the systems studied here:

> **Good payment testing is less about testing happy paths and more about proving that bad things cannot corrupt money or state.**
