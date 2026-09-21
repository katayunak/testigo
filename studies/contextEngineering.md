# Context Engineering

How to get a better answer from a model for fewer tokens, measured on testigo.

This is not a prompt-writing style guide. Every claim here changed code in this
repository, and every number is from a real run.

---

## The premise

A model is expensive, fallible, and will answer anything you ask — including things
it cannot know. A compiler is free, exact, and has no opinion.

So the question is never "how do I write a better prompt?" It is **"what am I asking
a model that I could have proved instead?"**

That question produced testigo's two-phase split, and everything below follows from
it.

---

## 1. Output is what costs

Input and output are not priced alike. Across current frontier models, output runs
roughly **five times** the price of input.

testigo weights it that way everywhere:

```go
func (c Cost) Weighted() int { return c.Input + c.Output*OutputMultiplier }
```

This single change reorders every optimisation you would otherwise make. Trimming a
long prompt feels productive and is nearly worthless; removing one open-ended
question from the answer shape is worth five times as much per token.

Measured cost per field, from real runs:

| Answer shape | Output tokens per field |
|---|---|
| true/false | **5** |
| pick one from a list | 15 |
| free text (measured: `externalEffect`) | 21 |
| free text (measured: `moneyModel`) | 77 |
| free text (measured: `stateRoles`) | 131 |

A boolean is **twenty-six times cheaper** than the average free-text field. Most
things worth knowing are decidable, and asking for a paragraph when a bit would do is
the most common way to waste money on a model.

> **Rule:** if the answer space is finite, enumerate it. Ask for the bit, not the essay.

---

## 2. Ask only what something will read

The most effective cost control in this project was not a prompt change. It was
keeping a registry of which answer field is read by which consumer:

```go
{"moneyModel.money.type",  Live, "plan.FactsFrom -> Facts.MoneyType"},
{"stateRoles.roles",       Live, "prompts.TestCase via Transitions, IsFinal, Illegal"},
```

A field with no reader should not be in the answer shape. Writing the registry found
**35 fields** that were being requested, parsed, validated, stored — and never read
by anything. Every one of them was paid for on every run.

This is the failure mode of any schema that grows by accretion. Nobody adds a useless
field on purpose; they add a field for a plan that changes, and the field stays
because deleting it feels riskier than keeping it. A registry makes the omission
visible, and a test enforces it.

> **Rule:** name the consumer of every field you request. If you cannot, do not request it.

---

## 3. Delete the question before optimising it

Per-function "notes" asked the model, for each function in the flow: what does this
do in business terms, what does it assume, what are its effects, how confident are
you?

It was the most expensive ask in the system — **381k of 618k** weighted tokens in
round one on a real payment service, roughly 62% of the bill.

It was also the least useful:

- `effects` restated what SSA already proved, less reliably;
- `assumptions` was the genuinely valuable part, and is now asked directly as scenario questions;
- only the step label was ever read, as a diagram caption;
- no scenario required any of it.

The feature was removed entirely, along with the body-hash and staleness machinery
that existed only to carry notes between scans.

The lesson generalises past this project. The cheapest token is the one you never
send, and the biggest wins come from deleting a question rather than tightening it.
A question that is merely *interesting* is a question you are paying for.

> **Rule:** before improving a prompt, check whether anything downstream needs its answer at all.

---

## 4. Say the shared thing once

Nine prompts that each restate the same rules are eight copies you are paying for.

testigo hoists everything common into `testigo/asks/PREAMBLE.md`, written once per
round: the shared goal, the answer-shape rules, the migrations, and the flow rendered
step by step. Each ask then cites step numbers instead of reprinting the flow.

On the fixture: **~34.8k tokens saved against ~14.7k actually sent.** More than twice
the prompt pack's own size, recovered by not repeating.

The discipline is enforced rather than remembered — a test fails the build if any
block of text is identical across the asks of one kind, because that text belongs in
the preamble:

```
TestNothingConstantAcrossAKindIsRepeatedPerQuestion
```

That test caught a 1,887-byte block duplicated across every state-roles ask, months
after the rule was written down and forgotten.

> **Rule:** repetition across prompts is a build failure, not a style preference.

---

## 5. Supply the evidence; do not make the model go looking

A question that requires the model to search a codebase is expensive twice: it burns
tokens exploring, and the answer is unverifiable because you do not know what it read.

Compare:

> *Find the idempotency key in this repository.*

against what testigo actually sends: five scored candidates, the evidence for each,
and the file and line of every one.

```
  7  Payment.IdempotencyKey   + external  + queried  + name
  4  Payment.OrderID          + external  + name
  1  Payment.ID               + queried   + unique   − generated in-process
 -3  Payment.ReferenceID      + name                 − generated in-process
```

The second costs a fraction as much, and the answer is checkable because the option
set is known in advance. Static analysis did the searching for free.

> **Rule:** attach the evidence to the question. Searching is the cheapest thing to do without a model.

---

## 6. Bound the question so a wrong answer is visible

"Read this code and tell me what it does" is unbounded: a wrong answer looks exactly
like a right one, and you have no way to tell them apart.

"Here are six declared states and the lines that write them — what part does each
play?" is bounded. The answer space is finite, completeness is mechanical, and every
claim names a file and line that either exists or does not.

testigo validates on that basis. It cannot tell whether an answer is *correct*, but it
can reject an answer that is not about this repository at all:

- a state that does not exist, or a declared state left out;
- a claim with no `file:line`;
- the prompt's own example returned verbatim;
- an answer to a question that was never asked;
- a true/false question with no verdict — rejected, never read as `false`.

> **Rule:** prefer questions whose answers are checkable without trusting the answerer.

---

## 7. Scope the context to the task, not to the session

The intuition that a model does better with more context is wrong past a point, and
it is expensive in the meantime.

In round two, every test-generation prompt receives **only its own scenario's**
round-one answers. A state-machine test gets the states it must refuse. A
fault-injection test gets the verdicts for the seams it fakes. An idempotency test
gets the key facts. None of them sees the others.

This is cheaper, and it is also *better*: irrelevant context is not neutral. It
invites the model to use it, and a test that drifts toward a neighbouring scenario is
worse than one with a narrow brief.

> **Rule:** give each task its own context. Shared context is a default, not a decision.

---

## 8. Name the wrong answers in advance

Almost every bad generated test is bad in a predictable way: summing balances only at
the end, staggering goroutines with a sleep, generating the expected value by calling
the code under test.

So every scenario carries an `AntiGoals` list naming those specific failures. This is
far cheaper and far more effective than any quantity of "be careful" or "think step by
step", because it is concrete and checkable.

The sharpest instance is oracle provenance. A test whose expected value came from
reading the implementation **cannot fail on buggy code** — the bug becomes the
assertion and the suite goes green forever. Every prompt names its oracle, and
`currentBehavior` is refused outright for anything touching money.

> **Rule:** enumerate the specific wrong answers. Generic caution changes nothing.

---

## What it added up to

Round one on a real payment service:

| | weighted tokens |
|---|---|
| before | 123.9k |
| after removing notes and unread fields | 87.3k |
| after the cost-based planner | **30.7k** |

**A 75% reduction, with no test dropped and no scenario lost.**

Nothing here was a prompt-wording trick. Every gain came from one of four moves:
prove it instead of asking; do not ask what nothing reads; say the shared thing once;
and make the answer checkable.

---

## The short version

1. Weight output 5x. It is what you are actually buying.
2. Prove it with a compiler before you ask a model.
3. Name the consumer of every field, or delete the field.
4. Ask for a bit, not an essay, whenever the answer space is finite.
5. Hoist anything repeated, and let a test enforce it.
6. Attach the evidence; never make the model search.
7. Scope context per task.
8. Name the wrong answers, not just the right one.
