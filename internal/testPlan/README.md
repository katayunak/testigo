# testPlan — the entity layer

The order here is the opposite of the obvious one. A prompt is not written by
hand and then made to fit a test. A **TestCase is assembled from typed data, and
the prompt is rendered from it, last.**

Everything that decides what the test should be — the scenario, the technique,
what passing means, what a wrong test looks like — lives here as Go values, where
it costs nothing, can be unit tested, and can be reviewed without reading prose.

```
Scenario  (what breaks, from published practice)
    ×
Technique (how to express it, with its stated limits)
    ↓
TestCase  (bound to this repo's symbols, sized, scoped)
    ↓
prompt    (rendered by askingAgent — the last step, and the only one that costs)
```

## Three axes, not one enum

The classic taxonomies disagree about what a "test type" even is, and the
disagreement is real rather than cosmetic:

| Taxonomy | Primary axis | Useful for generation? |
|---|---|---|
| Fowler / Cohn pyramid | scope — how much code | weakly; scope is a consequence, not an input |
| Google small/medium/large | resource access — what it may touch | **strongly; it is a checkable predicate** |
| Kent C. Dodds trophy | ROI | no; it is a budget argument |

So `Technique` is the enum, and `Size` and `Scope` are fields. Google says the
two are independent for exactly the reason we need them to be: *"the most
important qualities we want from our test suite are speed and determinism,
regardless of the scope of the test."*

## Size is enforced, not described

`sizeCheck.go` parses generated code and rejects a `small` test that sleeps,
reads the wall clock, opens a socket, touches disk, starts a process, opens a
database, or uses unseeded randomness.

That is the whole reason Google's axis earns its place. "This is a unit test" is
a claim in a comment nobody can check. "This file opens no socket and never
sleeps" is a predicate over the syntax tree. Catching a violation costs one
retry; missing it costs someone chasing a flake months later, long after anyone
remembers the test was generated.

Medium and Large are defined by *permission* rather than absence, and permission
is not checkable, so they are not enforced — only tagged.

## OracleProvenance — the most important field

An oracle is whatever tells the test what "correct" means. Where it came from
decides whether the test can find a bug at all.

| Provenance | Can it fail on buggy code? |
|---|---|
| `specification` — a published contract | yes |
| `invariant` — a property of the domain | yes |
| `metamorphic` — a relation between two runs | yes |
| `reference` — a second implementation | yes |
| `currentBehavior` — whatever the code does today | **no, by construction** |

The last one is the characteristic failure of generated tests: read the code,
infer the output, assert it. A bug becomes the assertion and the suite goes green
forever. Every rendered prompt names its oracle, and `currentBehavior` is refused
for anything touching money.

## Scenarios are data, and that is the token story

22 scenarios live in `catalog.go`, written by hand from the published practice of
companies that move money at scale. Every one carries a source; an entry with no
source is one somebody invented, and a reader should be able to tell at a glance.

Owning a scenario costs nothing. Only the ones whose preconditions match a
repository are ever rendered, so the catalog can grow without the prompt pack
growing. On the fixture, 22 scenarios become 19 cases and ~11k tokens; the other
three are reported with the reason they do not apply.

The sharpest filters come from **round one**, not from the compiler:

- a conservation test needs a function that reads a balance
- a currency test needs a money type
- an idempotency test needs a key

The compiler can see that a function returns an `int64`. It cannot see that the
`int64` is a balance. Asking the binding question first and filtering on the
answer is what stops testigo generating a conservation test for a repository
with no concept of a balance — and it is the concrete payoff of splitting the
rounds.

## The two fields that do the most work in a prompt

**`Acceptance`** says what passing *means*, so the agent has a target instead of
a vibe and a reviewer has something to check the generated code against.

**`AntiGoals`** names the specific wrong versions of this test. Almost every bad
generated test is bad in a predictable way — summing balances only at the end,
staggering goroutines with a sleep, generating the expected value by calling the
code under test — and naming that way in advance is cheaper and far more
effective than any amount of "be careful".

## Files

| file | holds |
|---|---|
| `testType.go` | Technique, Size, Scope, Role, OracleProvenance, and per-technique limits |
| `scenario.go` | the Scenario struct and applicability |
| `catalog.go` | the 22 scenarios |
| `testCase.go` | binding a scenario to a repository, and selection |
| `sizeCheck.go` | the static predicate that makes Size real |

## Sources

Stripe idempotent requests and currencies · Adyen currency codes, four of which
deviate from ISO 4217 · Airbnb Orpheus · brandur.org idempotency keys and job
drain · Uber money orders · Square Books · Nubank generative ledger testing ·
Jepsen bank workload · Monzo coherence services and Stand-in · Starling
catch-up processing · Shopify anomalies and Toxiproxy · TigerBeetle fuzzers ·
Google test sizes · Fowler's pyramid.
