# prompts — the only place text is written

One file per prompt. Everything a prompt says about *what* to ask comes from typed
data in `domain/` and `testPlan/`; this package decides only how it reads.

## The builder

```go
New(title).
    Goal(...).
    Rule(...).
    Fact(Proof, "FACTS — proved by the compiler, do not re-derive", ...).
    Ask(questions...).
    Answers(shape).
    Where("testigo/flow.json")
```

Facts are labelled by strength. `Proof` means the compiler established it and the
agent must not re-derive it; `Supporting` means it is context. Saying which is which
stops the agent spending output re-deriving something already known, and stops it
treating a hint as established.

## PREAMBLE.md — say the shared thing once

Written once per round, holding everything the asks of that round would otherwise
repeat: the shared goal, the answer-shape rules, the migrations, and the flow
rendered step by step. Each ask then cites step numbers instead of reprinting the
flow.

On the fixture this saves **~34.8k tokens against ~14.7k actually sent** — more than
twice the prompt pack's own size, recovered purely by not repeating.

It is enforced, not remembered:

```
TestNothingConstantAcrossAKindIsRepeatedPerQuestion
```

Any block of text identical across the asks of one kind fails the build, because that
text belongs in the preamble. This test caught a 1,887-byte block duplicated across
every state-roles ask.

## Round two: each case gets only its own facts

`TestCase` renders one test prompt, and it receives only that scenario's round-one
answers:

```
FACTS — proved by the compiler, do not re-derive
ANSWERED IN ROUND ONE — about this case only, do not re-ask
```

A state test gets the states it must refuse and the illegal pairs. A fault-injection
test gets the five verdicts for the seams it fakes. An idempotency test gets the key
facts. No case sees a neighbouring scenario's answers.

This is cheaper, and it is better: irrelevant context is not neutral. It invites the
model to use it, and a test that drifts toward a neighbouring scenario is worse than
one with a narrow brief.

## Every generated test must prove it did something

A concurrency test whose goroutines never collided passes. A fault-injection test
whose fault never fired passes. Both are green and both checked nothing, which is
worse than no test because it reads as coverage.

So every generated test asserts that its situation was actually reached:

```go
if fake.CallCount() == 0 {
    t.Fatal("the fake provider was never called — this test proved nothing")
}
```

## Files

| file | renders |
|---|---|
| `prompt.go` | the builder, fact strengths, and rendering |
| `preamble.go` | the shared per-round preamble |
| `moneyModel.go` | which field is money and what moves it |
| `mainEntity.go` | the entity the flow moves, and its retry identifier |
| `questions.go` | the scenario and technique questions |
| `stateRoles.go` | one machine's states and their roles |
| `externalEffect.go` | five verdicts about one seam |
| `testCase.go` | round two: one test per runnable scenario |
| `report.go` | the prompt that produces `REPORT.md` |
| `flowSteps.go`, `migrations.go` | the flow and schema, rendered once for the preamble |
