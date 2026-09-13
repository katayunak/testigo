# scanningFlow — phase 1

Everything here is free and offline. It runs the Go type checker and SSA over the
repository and writes down what can be **proved**, so phase 2 never has to ask.

```
packages.Load ──► SSA ──► CHA call graph
                              │
       entry points ──────────┤
                              ▼
                        reachable functions
                              │
        ┌─────────────┬───────┴────────┬──────────────┐
        ▼             ▼                ▼              ▼
      seams      state machines     money        findings
        │             │                │              │
        └─────────────┴────────┬───────┴──────────────┘
                               ▼
                        testigo/flow.json
```

## Proof versus heuristic — the distinction that matters most

Two different mechanisms produce the output, and confusing them is how a static
analyser starts lying confidently.

**Proved by the compiler.** Which functions are reachable. Whether a call is an
interface *invoke* or a static call on a concrete type — that is what `Injectable`
means, and it is not a guess. Which types exist, what a field's type is, where a
constant is assigned.

**Found by name.** Which packages count as a database. Which type names are money.
Which enums are a lifecycle. These live in [`patterns/`](patterns/README.md) as
tables of regular expressions.

A missing row in those tables is not a small problem: if a repository uses go-pg and
go-pg is absent from `patterns/io.go`, testigo reports that the payment flow never
touches a database. A confident wrong answer, from one missing line. When pointing
testigo at a new repository, check its direct dependencies against those tables
first.

## The call graph

CHA — Class Hierarchy Analysis. It is conservative: if a type *could* implement an
interface, its method is a possible target. That adds edges that cannot happen at
runtime, which is the right trade here, because missing a real path through a payment
flow is worse than carrying a few impossible ones.

`discover` walks it from the entry points declared in `testigo/config.json`. Nothing
in this package looks for entry points — there is no ranking and no name matching.

It computes a **set**, not a sequence. Every reachable local function is visited once
and emits all its out-edges when it is, so the order the worklist drains in cannot be
observed in the output. Swapping the queue for a stack produces a byte-identical
`flow.json`, and `scan_test.go` asserts that rather than assuming it.

The ordering that *does* survive lives on the edges: each recorded call carries the
position of the call expression inside the caller's body, so a rendered step list
reads in the order the statements actually run.

## Seams

A seam is a call that crosses the process boundary.

| Kind | Examples | Why it matters |
|---|---|---|
| `db` | `database/sql`, pgx, GORM, go-pg | transactions, constraint violations, lock contention |
| `http` | `net/http`, gRPC | provider calls, timeouts, partial failure |
| `queue` | Kafka, NATS, RabbitMQ | retries, duplicates, delivery order |
| `cache` | Redis, Memcached | stale reads |
| `clock` | `time.Now`, `time.Sleep` | expiry and retry behaviour |
| `random` | `crypto/rand`, UUID | determinism |

Classification uses the **actual SSA target**, not the method name: `http.Client.Get`
is an HTTP boundary, `http.Header.Get` is not. Cursor operations like `Rows.Next` are
ignored on purpose — the query is the useful seam, and reporting every scan adds
noise without adding a test.

`computeReach` propagates seam kinds backwards through local code to a fixed point, so
a function that performs no I/O itself still knows which kinds it can eventually
cause.

### Is the boundary real? Sink reachability

A package prefix only nominates a *candidate*. A `db`, `http`, `queue` or `cache`
candidate survives only if `computeSinks` shows it can reach a genuine round trip: a
`net` connection, `crypto/tls`, the `database/sql` exec and query calls, the
`net/http` client, or `os/exec`.

Because CHA over-approximates, the reverse walk refuses three kinds of edge:

| Not followed | Why |
|---|---|
| invokes on plumbing interfaces — `error`, `io.Reader`/`io.Writer`, `context.Context`, `fmt.Stringer`, the marshalers — and on anonymous interfaces | they resolve to every implementation in the program, including socket reads |
| calls through function values with a generic signature, such as `func(rune) rune` | CHA links them to every function of that shape |
| edges from library code back into your application | a library's own I/O cannot depend on the callbacks you register |

A callback that names a *library* type is followed — go-pg runs every query inside
`func(context.Context, *pool.Conn) error`, and that is exactly how it reaches its
connection. Skipping all function values instead of just generic ones silently lost
real database seams; this distinction was found by that bug.

Sends that hand work to a background goroutine — `Publish`, `Send`, `Request`,
`Enqueue` — are kept by name, because their socket write is on no static path at all.
NATS `EncodedConn.Publish` is the proof: a background flusher owns the write, so no
call graph will ever connect it to a socket.

`clock` and `random` seams exist for determinism, not I/O, and are never filtered.

On a real service this took seams from **165 to 69** and effect-seam targets from 35
to 15. All 12 real seams survived; all 12 false ones — `grpc/status.New`,
`metadata.MD.Get`, go-pg's query builders, `(error).Error` — were dropped. It is a
filter, not a proof.

## State machines

A type is a lifecycle if it is persisted in a struct field **and** reassigned in two
or more functions. That is why `SettlementMode` counts and `DeclineCode` does not,
despite both being named string types with constants.

Names alone are not enough: `PaymentStatus` is a lifecycle, `ErrorCode` is an enum,
and asking an agent which transitions of an error code are legal is nonsense that
costs money. Strong names become machines automatically; weak ones are reported for a
person to promote.

## Money

Deterministic AST and type analysis, no agent:

| Finding | Severity | Detects |
|---|---|---|
| `MONEY-FLOAT` | critical | money stored, accepted or returned as a float |
| `MONEY-DIV` | high | integer money divided with no stated rounding rule |
| `MONEY-NO-CURRENCY` | medium | a bare integer amount with no currency beside it |

## Structural findings

`SEAM-CONCRETE`, `TX-NO-ROLLBACK`, `TX-NET-CALL`, `STATE-NEVER-SET`,
`IDEM-KEY-NOT-UNIQUE`, `LOAD-ERROR`. The main [README](../../README.md) explains what
each means and why it ranks where it does.

A finding is **evidence of a risk, not proof of a bug**. `STATE-NEVER-SET` cannot know
whether another service writes that state; it reports the fact and leaves the
judgement to a person.

## Files

| file | holds |
|---|---|
| `scan.go` | the entry point: loads packages, orchestrates, assembles findings |
| `graph.go` | SSA, the CHA call graph, reachability, node classification |
| `seams.go` | seam classification, reach, sink gating |
| `symbol.go` | how a function is named from its AST declaration |
| `states.go` | lifecycle detection and write sites |
| `money.go`, `candidates.go` | money and idempotency-key scoring |
| `schema.go`, `infra.go` | migrations, constraints, tables |
| `docs.go` | markdown docs in the repo, scored by relevance |
