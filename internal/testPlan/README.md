# testPlan — what to test, decided before anything is written

The order here is the opposite of the obvious one. A prompt is not written by hand and
then made to fit a test. A **TestCase is assembled from typed data, and the prompt is
rendered from it, last.**

Everything that decides what a test should be — the scenario, the technique, what
passing means, what a wrong version looks like — lives here as Go values, where it
costs nothing, can be unit tested, and can be reviewed without reading prose.

```
Scenario   what breaks, from published practice
    ×
Technique  how to express it, with its stated limits
    ↓
TestCase   bound to this repository's symbols, and sized
    ↓
prompt     rendered by agent/prompts — the last step, and the only one that costs
```

## Two axes, not one enum

The classic taxonomies disagree about what a "test type" even is, and the
disagreement is real rather than cosmetic:

| Taxonomy | Primary axis | Useful for generation? |
|---|---|---|
| Fowler / Cohn pyramid | scope — how much code | weakly; scope is a consequence, not an input |
| Google small/medium/large | resource access — what it may touch | **strongly; it is a checkable predicate** |
| Kent C. Dodds trophy | return on investment | no; it is a budget argument |

So `Technique` is the enum and `Size` is a field. Google's axis earns its place
because it is the only one that can be *enforced*.

## Size is enforced, not described

`sizeCheck.go` parses generated code and rejects a `small` test that sleeps, reads the
wall clock, opens a socket, touches disk, starts a process, opens a database, or uses
unseeded randomness.

"This is a unit test" is a claim in a comment nobody can check. "This file opens no
socket and never sleeps" is a predicate over the syntax tree. Catching a violation
costs one retry; missing it costs someone chasing a flake months later, long after
anyone remembers the test was generated.

Medium and large are defined by *permission* rather than absence, and permission is
not checkable, so they are tagged rather than enforced.

## OracleProvenance — the most important field here

An oracle is whatever tells a test what "correct" means. Where it came from decides
whether the test can find a bug at all.

| Provenance | Can it fail on buggy code? |
|---|---|
| `specification` — a published contract | yes |
| `invariant` — a property of the domain | yes |
| `metamorphic` — a relation between two runs | yes |
| `reference` — a second implementation | yes |
| `currentBehavior` — whatever the code does today | **no, by construction** |

The last one is the characteristic failure of generated tests: read the code, infer
the output, assert it. The bug becomes the assertion and the suite goes green forever.
Every rendered prompt names its oracle, and `currentBehavior` is refused outright for
anything touching money.

## The catalogue is data, and that is the token story

Every scenario is listed in [CATALOG.md](../../CATALOG.md).

**28 scenarios** in `catalog.go` across 7 families — money (6), consistency (5),
boundary (4), idempotency (4), state (4), failure (3), ordering (2) — each written by
hand from the published practice of companies that move money at scale, and each
traceable to a source in
[studies/](../../studies/howRealFintechSystemsTestPaymentFlows.md).

Owning a scenario costs nothing. Only the ones whose preconditions match a repository
are ever rendered, so the catalogue can grow without the prompt pack growing. On the
wallet fixture, 24 of the 28 apply; the rest are reported *with the reason they do
not apply*, which is itself information.

The sharpest filters come from **round one**, not from the compiler:

- a conservation test needs a function that reads a balance;
- a currency test needs a money type;
- an idempotency test needs a key.

The compiler can see that a function returns `int64`. It cannot see that the `int64`
is a balance. Asking that question first and filtering on the answer is what stops
testigo generating a conservation test for a system with no concept of a balance —
and it is the concrete payoff of splitting the rounds.

## The two fields that do the most work in a prompt

**`Acceptance`** says what passing *means*, so the agent has a target instead of a
vibe, and a reviewer has something concrete to check the generated code against.

**`AntiGoals`** names the specific wrong versions of this test. Almost every bad
generated test is bad in a predictable way — summing balances only at the end,
staggering goroutines with a sleep, generating the expected value by calling the code
under test. Naming that in advance is cheaper and far more effective than any amount
of "be careful".

## Files

| file | holds |
|---|---|
| `scenario.go` | the `Scenario` struct, `Requires`, `Facts`, and `Applies` |
| `catalog.go` | the 28 scenarios |
| `testCase.go` | a scenario bound to a repository |
| `testType.go` | Technique, Size, OracleProvenance, and each technique's stated limits |
| `select.go` | which scenarios apply here, and why the others do not |
| `sizeCheck.go` | the static predicate that makes `Size` real |

## Sources

Stripe idempotent requests and currencies · Adyen currency codes, four of which
deviate from ISO 4217 · Airbnb Orpheus · brandur.org idempotency keys and job drain ·
Uber money orders · Square Books · Nubank generative ledger testing · Jepsen bank
workload · Monzo coherence services and Stand-in · Starling catch-up processing ·
Shopify anomalies and Toxiproxy · TigerBeetle fuzzers · Google test sizes · Fowler's
pyramid. Collected in
[studies/howRealFintechSystemsTestPaymentFlows.md](../../studies/howRealFintechSystemsTestPaymentFlows.md).
