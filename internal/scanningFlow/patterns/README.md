# patterns — name matching, and where it is not enough

Every rule in this package is a **heuristic**. The call graph, the type checking
and the invoke-vs-static distinction are proofs. These are guesses based on how
people usually name things.

They live in one file per concern because they are the rules most likely to be
wrong for your codebase and the ones you will want to edit first. A rule buried
in three hundred lines of SSA analysis is a rule nobody finds.

| file | matches |
|---|---|
| `io.go` | package prefixes to boundary kinds; net/http client calls; cursor noise |
| `state.go` | lifecycle type names; final-state hints; reopening words |
| `money.go` | amount fields, money types, currency fields, idempotency-key names |

A missing row is not a small problem. If a repository uses go-pg and go-pg is
absent from `io.go`, testigo reports that the payment flow never touches a
database. That is a confident wrong answer, which is worse than no answer. When
pointing testigo at a new repository, check its direct dependencies against these
tables first.

---

## The idempotency key: why the name is not the answer

This is the clearest case of a name matcher failing, and it is worth reading
before trusting any other table here.

`money.go` has an `IdempotencyField` vocabulary — `idempotency`, `idem`,
`request_id`, `reference_id`, `correlation_id`, `trace_id`, `order_id`, and more.
Now take an ordinary struct:

```go
type Order struct {
    ID             string
    ReferenceID    string
    TraceID        string
    OrderID        string
    IdempotencyKey string
}
```

**Five fields match.** Picking one by name is not a heuristic, it is a coin flip.

### Handing it to an agent does not fix it

The obvious move is to ask a model. It does not help: the model sees the same
five names and has the same information. A confident answer is still a guess,
only now it is harder to audit and it costs money.

### What actually separates them

An idempotency key is not a naming convention, it is a **behavior**. Three of
its properties are visible in source:

**1. It comes from outside.** The client supplies it. A field assigned from
`uuid.New()` in this process cannot be one — the whole point is that a retry
sends the *same* value, and a value generated per attempt is different every
attempt.

This single check eliminates most false candidates, and no name can give it to
you. `ReferenceID` looks perfect and is disqualified the moment you see
`ReferenceID: uuid()`.

**2. The code looks it up before doing the work.** A key that is stored but never
read prevents nothing.

**3. The database enforces uniqueness on it.** Already known, for free, from
reading the migrations.

### The scoring

`scanningFlow/candidates.go` collects evidence and shows its work:

| signal | weight |
|---|---|
| value arrives from outside the process | **+3** |
| passed to a database call, so it is looked up | **+3** |
| a migration puts UNIQUE on that column | **+2** |
| name is in the vocabulary | **+1** |
| **value generated in-process by uuid or rand** | **−4** |

Note that the name is worth the least of anything. It is a tiebreaker, not a
decision.

On the struct above:

```
  7  Payment.IdempotencyKey   + external  + queried  + name
  4  Payment.OrderID          + external  + name
  1  Payment.ID               + queried  + unique   − generated in-process
 -3  Payment.ReferenceID      + name                − generated in-process
 -3  Payment.TraceID          + name                − generated in-process
```

### Then it decides, or asks a much better question

A candidate wins only with a score of at least 4 **and** a lead of at least 3.
Both conditions matter: the score floor stops a name-only match (worth 1) from
ever winning alone, and the gap stops a near-tie from being treated as settled.

- **Clear winner** → recorded as a fact. No question, no tokens.
- **Close call** → the agent gets multiple choice over the scored list with the
  evidence attached. "Here are five candidates and why each scored what it did,
  pick one" is a far better question than "find the idempotency key", and it is
  cheap because the reasoning is already done.
- **Nothing found** → an open question, and a real finding if the answer is that
  no client-supplied key exists.

### The general rule

**Score the evidence, show the work, and only ask when the evidence ties.**

The same shape is used for money types in `candidates.go`, and for deciding
whether an enum is a lifecycle in `states.go` — a type is a state machine if it
is persisted in a struct field *and* reassigned in two or more functions, which
is why `SettlementMode` counts and `DeclineCode` does not, despite both being
named string types with constants.

A guess with its reasons printed is auditable. A guess without them is a lie
with good manners.
