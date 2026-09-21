# askingAgent — phase 2

Phase 1 proved everything a compiler can prove. This package asks about the rest.

## The rule every prompt follows

**Ask a bounded question with a verifiable answer.**

"Read this code and tell me what it does" is unbounded. The answer cannot be
checked, so a wrong answer looks exactly like a right one and the model has no
reason not to invent.

"Here are six declared states and four write sites the compiler found; which
transitions between them should be impossible?" is bounded. The answer space is
a finite matrix, completeness is mechanical — every declared state must appear —
and every claim has to name a file and line that either exists or does not.

Phase 1 exists to make these questions bounded. Everything it proved is pasted
into each prompt as fact, so the agent is never asked to re-derive what a
compiler already knows, and can never contradict it.

## Two rounds

| Round | Asks for | Writes code |
|---|---|---|
| 1 `understand` | binding, transitions, external effects, notes | no |
| 2 `generate` | tests | yes |

Round 2 is **gated** on round 1, not best-effort. If the agent decides the wrong
call is the money-moving one, a test written in the same response will faithfully
encode that mistake and pass forever. Splitting the rounds gives a person one
place to look and one thing to fix, before anything is generated from it.

## Transport: files

`testigo ask` writes `.testigo/asks/*.md`. Your agent reads them and writes
`.testigo/answers/*.json`. `testigo collect` validates and applies.

No API key, no network, no vendor. The prompts are markdown a person can read
and correct before spending anything, and the answers are JSON a person can
hand-write when the agent gets one wrong. Every other transport can be added
later behind the same two directories.

## What gets validated

Nothing here can tell whether an answer is *correct*. It can tell whether the
answer is about this repository at all, which catches most of what goes wrong:

- states must be states that exist in the code
- every declared state must be covered — a forgotten one is an error
- a named symbol without a `file:line` is rejected
- the placeholder names from the prompt's example are rejected
- a note is rejected if the function's body hash moved while it was being written
- a generated test containing `time.Sleep` is rejected
- a test marked blocked with no reason is rejected

## Safe defaults

`retry_safe` and `foreign_mutation` accept `true`, `false`, or `"unknown"`.
Anything that is not literally `true` is treated as **not retry safe**, and
anything not literally `false` is treated as **mutating foreign state**.

Assuming a charge is retry safe when it is not is how a customer gets billed
twice. The reverse only costs an unnecessary idempotency key. The defaults point
that way on purpose, and there is a test that keeps them pointing that way.

## Which seams get asked about

A foreign mutation is a change a database `ROLLBACK` cannot undo. That excludes
most database traffic: a `SELECT` changes nothing, an `INSERT` inside a
transaction the code controls is undone by rolling back.

What survives the filter:

- anything leaving the machine — HTTP, gRPC, broker, cache
- `COMMIT`, the exact line after which rollback stops working
- every call through an interface the repository defines itself, because
  `Ledger.Post` might write a row or might call a provider, and that is the
  question worth paying for

On the fixture this is the difference between 9 questions and 4.

## Why idempotency first

Its correctness condition is published and precise, so there is a real oracle
rather than a guess. Its failure points are enumerable — one per step that
leaves the process, which round 1 already listed. And unlike concurrency it
needs no flakiness budget, so red means a bug rather than bad luck.

The contract tested against, from the published behaviour of Stripe, Airbnb's
Orpheus, and GoCardless:

1. The first request's result is stored and replayed — including failures
2. Same key, different parameters is an error, not a replay
3. Concurrent same key: exactly one proceeds
4. Crash between any two steps, then retry, ends in the same terminal state
   with exactly one foreign mutation
5. Nothing recorded if execution never began
6. An unclassified error defaults to non-retryable

## Every generated test must prove it did something

A concurrency test where the goroutines never collided passes. A crash test
where the injected failure never fired passes. Both are green and both checked
nothing.

So the prompt requires each test to assert the interesting situation was
*reached*:

```go
if fake.CallCount() == 0 {
    t.Fatal("the fake provider was never called — this test proved nothing")
}
```

## Files

| file | holds |
|---|---|
| `ask.go` | `Ask`, `Kind`, `Round` |
| `answer.go` | answer types, validation, safe defaults, `Knowledge` |
| `plan.go` | which questions are worth asking right now |
| `pack.go` | writing and reading `.testigo/asks/` |
| `collect.go` | reading, validating and applying answers |
| `verify.go` | writing the test file and asking the toolchain if it is real |
| `flowSteps.go` | ordering the flow into the fact block prompts are built from |
| `prompt*.go` | one prompt each |
