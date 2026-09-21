# domain — the agent's vocabulary

Types only. No I/O, no prompts, no SSA. Everything the agent can be asked and
everything it can answer is declared here, which makes the whole phase-2 surface
reviewable in one package.

## Question — one thing testigo cannot prove

```go
type Question struct {
    ID           string
    Text         string
    Problem      string
    TrueOrFalse  bool
    References   []Proof
    Hints        []string
    RelatedPaths []string
}
```

`Problem` is the reason the question exists — the failure it guards against. It is in
the prompt because a question with its stakes attached gets a better answer than a
bare interrogative, and because it lets a reader judge whether the question is worth
asking at all.

`References` and `RelatedPaths` exist so the agent never has to go looking. Searching
is the most expensive thing a model can do and the least verifiable; static analysis
already did it for free.

A question is a **recheck** when it cites something: `Rechecking()` is
`len(References) > 0`. There is no separate mode flag — a "recheck" that cites nothing
is indistinguishable from a plain true/false question, so the distinction is derived
rather than declared.

> Recheck questions are built and tested, but **nothing currently produces one**.
> Wiring phase-1 findings into cited questions is unfinished design intent, not a bug.

## QuestionAnswer — and the asymmetry that saves money

```go
type QuestionAnswer struct {
    Verdict *bool
    Info    string
}
```

`Verdict` is a pointer so a missing answer is distinguishable from `false`. Reading
silence as `false` on "is this charge safe to retry?" is exactly the mistake that
bills a customer twice.

The asymmetry: **a bare `true` is a complete answer, and must not be justified.** An
explanation is required only when a verdict comes back `false` on something testigo
proved from the source — because overturning a proved fact is the one place an
explanation is worth paying for. Agreement is cheap; disagreement needs evidence.

## Payment kind — three axes

A system is classified on three axes rather than one label, because real systems are
combinations:

| Axis | Values |
|---|---|
| **Spine** | double-entry, wallet, stateless |
| **Motions** | top-up, payout, escrow, auth-capture, marketplace, one-shot… |
| **Overlays** | refund/reversal, reconciliation, FX |

`paymentKindDetect.go` infers this from the schema and the Go type names and records
*why*. The classification then selects scenarios, which select questions. Nothing else
decides what is asked.

## Answers, and safe defaults

`ExternalEffectAnswer` accepts `true`, `false` or `"unknown"` for each of its five
verdicts. Only a literal `true` counts as reversible, observable or deduplicated:

```go
func IsTrue(raw json.RawMessage) bool
```

Anything else is read the unsafe way. Assuming a charge is repeatable when it is not
bills someone twice; the reverse costs one extra test.
`TestUnknownExternalEffectIsTreatedAsIrreversible` keeps the defaults pointing there.

## Validation

Nothing here can tell whether an answer is *correct*. It can tell whether it is about
this repository at all: every state named must exist, every declared state needs a
role, a claim needs a `file:line`, the prompt's own placeholder coming back is
rejected, and a true/false question with no verdict is refused rather than defaulted.

## Files

| file | holds |
|---|---|
| `questions.go` | `Question`, `QuestionAnswer`, and how a question renders |
| `needs.go` | the kind → scenario → question maps, and `Needed` |
| `paymentKind.go` | the three axes and their combinations |
| `paymentKindDetect.go` | classifying a repository, with reasons |
| `paymentKindQuestions.go` | the per-kind question registry |
| `answer.go` | every answer shape, its validation, and `Proof` |
| `ask.go` | `Ask`, `Kind`, `Round`, and answer-file naming |
