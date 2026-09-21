# flowEntity — the shared vocabulary

The types phase 1 produces and everything else consumes. This package is kept
deliberately **dependency-light**: it imports almost nothing, so a consumer can talk
about a flow without importing the analyser that built it.

That is the reason it is not merged into `scanningFlow`. `testPlan`, `agent`,
`prompts`, `planner` and `report` all need these types; none of them needs SSA.

```
Flow
├── Entries          where the payment flow starts
├── Nodes            reachable functions, keyed by pkg#Symbol
├── Seams            calls that leave the process
├── States           detected state machines
├── MoneyTypes       scored candidates
├── IdempotencyKeys  scored candidates
├── Infra            migrations, tables, constraints
├── Docs             markdown found in the repo
└── Findings         what phase 1 proved
```

This is also the on-disk shape of `testigo/flow.json`.

## CodeRef — why everything carries one

```go
type CodeRef struct {
    Pkg    string
    Symbol string
    File   string
    Line   int
}
```

`Pkg + "#" + Symbol` is the identity used as a map key everywhere, and `File:Line` is
what a human needs to go look. A claim without a location cannot be checked, so
findings, nodes, seams and answers all carry one. The house rule that every claim
cites `file:line` is enforced here, by making it impossible to express one that does
not.

## Candidate — a guess that shows its work

```go
type Candidate struct {
    Name, Owner, Type string
    Score   int
    Proof   []string
    Against []string
}
```

Money types and idempotency keys are *scored*, not decided. `Proof` lists the evidence
for, `Against` the evidence against, and `Decided()` returns a winner only when the
score clears a floor **and** leads the runner-up by a margin.

A near-tie is not resolved here. It becomes a multiple-choice question for the agent
with the evidence attached — a far better question than "find the idempotency key",
and much cheaper, because the reasoning is already done.

A guess with its reasons printed is auditable. A guess without them is a lie with
good manners.

## Severity

`critical` · `high` · `medium` · `info`, and findings sort in that order. The main
README explains what each finding means and why it ranks where it does.

## StateRole

The richest type here, because state machines are where phase 1 and phase 2 meet.
Phase 1 finds the states and their write sites; the agent assigns each a **role**;
testigo then *derives* the legal transitions from the roles rather than asking for
them.

Deriving is the point. Asking a model to enumerate transitions of six states invites
36 answers and a lot of tokens. Asking for one role per state is six bounded answers,
and the transition matrix follows by construction.
