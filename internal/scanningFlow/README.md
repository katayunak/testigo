## Flow Model

The `Flow` is Testigo's main data model for the result of the scanning phase.

It is also what gets persisted to `.testigo/flow.json`.

```text
Flow
├── Entries    → where the payment flow starts
├── Nodes      → functions in the flow
├── Seams      → external boundaries
├── Machines   → detected state machines
├── Findings   → deterministic problems
└── Orphans    → notes from functions that disappeared
```

## Seams

A `Seam` is a point where the application crosses the **process boundary** and talks to an external system.
After building the call graph, Testigo performs a second analysis pass over the reachable functions. The goal is not
just to know which functions execute, but to understand **what side effects they can reach and whether those side
effects can be controlled by tests**.

### I/O classification

Testigo recognizes common external boundaries:

| Kind     | Examples                                  | Why it matters                                 |
|----------|-------------------------------------------|------------------------------------------------|
| `db`     | `database/sql`, `pgx`, GORM, Ent, MongoDB | Database failures, transactions, consistency   |
| `http`   | `net/http`, gRPC, Resty                   | Provider calls, timeouts, HTTP failures        |
| `queue`  | Kafka, NATS, RabbitMQ                     | Retries, duplicate messages, delivery failures |
| `cache`  | Redis, Memcached                          | Cache failures and stale data                  |
| `clock`  | `time.Now`, `time.Sleep`, `time.After`    | Expiry and retry behavior                      |
| `random` | `crypto/rand`, UUID, ULID                 | Non-deterministic behavior                     |

The classification is based on the **actual SSA target**, not just the method name. For example, `http.Header.Get` is
not treated as an HTTP boundary, while `http.Client.Get` is.

Database cursor operations such as `Rows.Next` and `Rows.Scan` are intentionally ignored. The query itself is the useful
test seam; reporting every cursor operation would add noise without adding useful testing information.

### Transitive I/O reachability

Direct calls are not enough. A function may not perform I/O itself but may call another function that does.

`computeReach` propagates I/O information backwards through the call graph until reaching a fixed point. This lets
Testigo answer:

> "If this function executes, what kinds of external I/O can eventually happen?"

The propagation uses a worklist rather than repeatedly scanning the entire graph, so only functions whose information
changed need to be reconsidered.

Reachability is propagated transitively through **local application code**. Dependencies contribute only their direct
I/O classification. This boundary is important because the call graph uses CHA and therefore already over-approximates
interface dispatch. Propagating every dependency's transitive reachability would turn that conservative approximation
into large amounts of useless noise.

### Injectable seams

A **seam** is a concrete call site where the application crosses an external boundary.

For every seam Testigo records:

- the function containing the call
- the external system (`db`, `http`, `queue`, etc.)
- the actual target
- the source line
- whether the boundary is injectable
- the interface type when applicable

Interface method calls are treated as injectable because the implementation is supplied through a value that a test can
replace.

### How seams and state machines are actually found

Two different mechanisms, and it matters which is which.

**Proved by the compiler:** whether a call is an interface *invoke* or a static
call on a concrete type. SSA reports it directly. That is what `Injectable`
means, and it is not a guess.

**Found by name:** everything else. Which packages count as a database, which
type names are money, which type names are a lifecycle. These come from the
tables in `patterns/`, matched against package paths and identifier names with
regular expressions.

That second list is a set of HEURISTICS. They are the first thing to check when
a result surprises you, and a missing row is not a small problem: if a repository
uses go-pg and go-pg is absent from `patterns/io.go`, testigo reports that the
payment flow never touches a database. A confident wrong answer, from a missing
line in a table.

The tables live in one file per concern so they are easy to find and edit:

| file | matches |
|---|---|
| `patterns/io.go` | package prefixes to boundary kinds; the net/http client calls; cursor noise |
| `patterns/state.go` | lifecycle type names, strong and weak; final-state hints |
| `patterns/money.go` | amount fields, money types, currency fields, idempotency keys |
| `patterns/entryPoint.go` | payment verbs, callback words, consumer argument shapes |

`patterns/state.go` splits names into two lists on purpose. `PaymentStatus` is a
lifecycle. `ErrorCode` is an enum but not a lifecycle, and asking an agent which
transitions of an error code are legal is nonsense that costs money. Strong names
become state machines automatically; weak ones are reported for a person to
promote through `testigo.rules.json`.

Migrations, compose files and broker config are read the same way, by pattern,
in `infra.go`. Shallow on purpose — a real SQL parser would be more correct and
take a week, while these patterns cover what migration tools actually emit and
fail by finding nothing rather than by finding something false.

## Graph

### CHA over VTA

Testigo uses CHA (Class Hierarchy Analysis) instead of VTA (Variable Type Analysis). CHA says if a type could implement
the interface, consider its method as a possible target; Even though it's not used. This creates noise, but it does not
necessarily hide a possible path && help us test the implementations from every angle.

VTA can provide a more precise call graph in some cases, but that precision is not the primary goal here. Testigo needs
a conservative graph that is unlikely to hide a possible payment path.

### Indexing all nodes

Testigo also indexes every declared function in the module, not only functions reachable from the configured entry
points.
This matters for incremental analysis.
Suppose a function had agent notes during the previous scan but is temporarily disconnected from the payment flow after
a refactor.
If only reachable functions were indexed, Testigo could incorrectly treat that function as deleted.

The complete index lets the resolver distinguish between:

* Function still exists
  but is no longer reachable
* Function no longer exists

That distinction is important for preserving notes correctly.

### Finding the Owner of a Function

SSA represents closures as separate functions. Testigo uses *owner()* to walk through the SSA parent chain until it
reaches the declared function containing the closure.
This prevents the flow from becoming polluted with anonymous implementation details.

### Traversing the Graph

**Depth-first, following each call in the order it is written.**

The obvious question is why not breadth-first, and the honest first answer is
that for pure reachability it makes no difference — both find the same set of
functions. The difference is what the flow LOOKS like afterwards, and that turns
out to matter more than it sounds.

#### Neither order is execution order

A call graph is a graph. It says "process can call Authorize", not "process
calls Authorize third". Which branch runs, how many times a loop repeats, whether
a `go` statement finished before the next line — none of that is knowable
without running the code.

But there is ordering information available, and an earlier version threw it
away: **within a single function, every call site has a line number.** That is
the order the code is written in, and it is as close to execution order as static
analysis gets. `Node.Calls` is sorted by call-site position, so the walk can
follow it.

#### Why depth-first wins once order exists

Breadth-first visits everything one call away, then everything two calls away.
Applied to a payment flow, it flattens the structure:

```
BFS                          DFS, in source order
---                          ---
CreatePayment                CreatePayment
process                        process
alreadySeen                      alreadySeen        line 43
Authorize                        BeginTx            line 52
Post                             Authorize          line 64
Commit                           Post               line 73
                                 Commit             line 76
```

The left column loses the single most important fact about this flow: the
provider call and the ledger write happen **inside** the transaction that
`process` opened. Depth-first keeps the nesting, so a reader sees what is
contained by what.

#### The reason this is not cosmetic

Several generated tests depend on step ORDER existing at all:

- *crash at each step* asks "what if the process dies after the provider call but
  before COMMIT" — a question that only means something if the steps are in
  sequence
- *orphaned authorization* is defined as the gap between two specific adjacent
  steps
- *no network call inside a transaction* is about containment, which is exactly
  what the nesting shows

Breadth-first order makes all three harder to express and easier to get wrong.

#### What is honest about it

Every prompt says this is source order, not execution order, and names the three
ways they differ: a branch may skip a call, a loop may repeat one, and `go` runs
one alongside the rest. Over-claiming here would be worse than the flat listing,
because a reader who believes it is execution order will trust a sequence that
the code does not guarantee.

A function reached from two places appears once, at its first occurrence. Cycles
terminate on the same check.

### Node Classification

A reachable function is classified using two simple rules:

* If it is one of the configured entry points ---> Entry
* If it has no outgoing calls to local functions ---> Leaf

Otherwise ---> Internal

This gives later phases a simple representation of the payment flow without exposing the full complexity of the SSA
graph.

## Money Analysis

Testigo performs a deterministic static analysis of money-related code without an agent and without executing the
program.
The goal is not to prove that a payment implementation is correct. Instead, it looks for common patterns that are
dangerous in financial code:

- money stored in floating-point types
- money represented as a bare integer without a currency
- integer division of money without an explicit rounding rule
- function boundaries that accept or return money as floats

The analysis uses GO's AST and type information from `go/packages` / `go/types`.

### Money Findings

| Finding             | Severity | What it detects                                                         | Why it matters                                                                                                                      | Example                                |
|---------------------|----------|-------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------|
| `MONEY-FLOAT`       | Critical | Money stored, accepted, or returned as a floating-point value           | Binary floating-point cannot represent many decimal values exactly, which can cause rounding errors and inconsistent comparisons.   | `Amount float64`                       |
| `MONEY-NO-CURRENCY` | Medium   | A bare integer represents money without a currency alongside it         | The type system cannot prevent values from different currencies from being accidentally combined, such as USD cents with EUR cents. | `AmountCents int64` without `Currency` |
| `MONEY-DIV`         | High     | Integer money is divided without an explicit rounding or remainder rule | Integer division silently truncates the remainder, which can make money disappear when splitting, prorating, or calculating fees.   | `fee := amountCents / 3`               |

## Scanning the Repository

`Scan()` builds a **structured snapshot of the repository** that later phases can reason about.
It extracts compiler and AST information, builds the call graph, discovers payment-flow nodes and seams, and produces a
`Flow` plus an `Index`.

### Deterministic Findings

These findings are produced directly from the scanned source and static-analysis facts. They do not require an agent or
running the application.

| Finding           | Severity | What it detects                                                                      | Why it matters                                                                                                                                                |
|-------------------|----------|--------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `SEAM-CONCRETE`   | High     | I/O is called directly on a concrete type instead of through an injectable interface | Tests cannot easily replace the real dependency to simulate timeouts, failures, duplicates, or other external-system failures.                                |
| `TX-NO-ROLLBACK`  | High     | A function opens a database transaction but has no rollback path                     | An early return can leave the transaction open. Pairing `Begin` with an immediate `defer tx.Rollback()` protects every error path.                            |
| `TX-NET-CALL`     | Critical | A network call happens while a database transaction is open                          | A slow or unavailable provider can keep database locks open, causing contention, timeouts, and potentially a database outage under load.                      |
| `STATE-NEVER-SET` | Medium   | A declared terminal state is never assigned anywhere in the scanned module           | The state may be dead code, written externally, or represent a missing transition. Any read path handling that state may therefore be untested or incomplete. |
| `LOAD-ERROR`      | Info     | A loaded package contains type-checking or loading errors                            | Analysis for that package may be incomplete, so findings and flow information from it should not be treated as complete.                                      |

#### Why these are deterministic

These checks are based on facts Testigo can derive directly from the repository:

- call-graph and seam information
- transaction-related SSA facts
- declared and assigned states
- package-loading/type-checking errors

No agent is needed to decide whether the pattern exists.

The important distinction is that a finding is **evidence of a risk, not proof of a bug**. For example,
`STATE-NEVER-SET` cannot know whether a state is intentionally written by another service or accidentally missing. It
reports the fact and leaves the final interpretation to the engineer.
