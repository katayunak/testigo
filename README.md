# TESTIGO
Test your *GO Fintech* code with some help from your *AGENT!!*

## Install and run

```sh
go build -o testigo ./cmd/testigo

# phase 1 — facts. no agent, no tokens, no network.
./testigo init    ~/work/payments   # write testigo.json  <- do this first
$EDITOR ~/work/payments/testigo.json          # declare your entry points
./testigo rules   ~/work/payments   # optional: what money movement means here
./testigo scan    ~/work/payments   # extract, write .testigo/flow.json
./testigo flow    ~/work/payments   # print the Mermaid diagram

# phase 2 — the context the code cannot carry. costs tokens.
./testigo cases   ~/work/payments   # what would be tested and what it would cost. free.
./testigo ask     ~/work/payments   # write the prompt pack for your agent
./testigo collect ~/work/payments   # read the answers back and validate them
./testigo ask --round 2 ~/work/payments   # generate tests from those answers
```
## Entry points — do this first

Nothing works until you have declared at least one. `testigo init` writes the
file; you fill it in.

```json
{
  // Every function where a payment flow can START.
  //
  // Symbol syntax:  Func  |  Type.Method  |  (*Type).Method

  "entries": [
    { "pkg": "example.com/pay/internal/api",
      "symbol": "(*Server).CreatePayment",
      "label": "API create" },

    { "pkg": "example.com/pay/internal/webhook",
      "symbol": "(*Handler).ProviderCallback",
      "label": "provider webhook" },

    { "pkg": "example.com/pay/internal/recon",
      "symbol": "(*Job).Reconcile",
      "label": "reconciliation job" },

    { "pkg": "example.com/pay/internal/consumer",
      "symbol": "(*Consumer).HandleSettlement",
      "label": "settlement queue" }
  ]
}
```

Edit that by hand, or append from the command line:

```sh
./testigo entry . add example.com/pay/internal/api#'(*Server).CreatePayment' "API create"
```

**testigo does not guess entry points, and will not.** It used to — anything
named Pay, Charge, Webhook or Handle came back in a ranked list — and that was
deleted, along with the `patterns/entryPoint.go` table behind it.

An entry point is a *declaration*, not a discovery. Which functions start a
payment flow is something your team knows and the code does not say: a handler
called `ProcessRequest` may be the entire flow, and one called `CreatePayment`
may be a wrapper nobody has called in a year. A generated list invites someone to
accept it without reading, and an entry point accepted without reading is a whole
path through the system that silently never gets analysed. A tool that is quietly
wrong is worse than a tool that asks.

So `testigo scan` fails on an empty list rather than analysing nothing and
reporting success.

List **all** of them. A payment flow has more than one: the API handler starts
it, but the provider webhook, the reconciliation job and the queue consumer all
rejoin the same state machine, and following only the first hides exactly the
bugs worth finding.


## PHASE ONE: ScanningTheFlow
Testigo saves tokens as much as possible, therefore first phase requires NO AGENT.
It uses the source code facts && GO's powerful tools like *SSA(Static Single Assignment)* && *AST(Abstact Syntax Tree)*
to create a *sidecar file*. You'll find this file as .testigo/flow.json.

### GO's Actual Analysis Pipeline
`GO source code`
→ `go/ast`
→ `go/types`
→ `go/ssa`
→ `callgraph`

**go/ast** → Testigo uses this for source structure 

**go/ssa** → Testigo uses this for control/data-flow analysis

**callgraph** → Testigo uses this for possible calls

### How the sidecar gets built

Six steps, in order. Everything here is a fact the compiler handed over, except
the last one, which matches names against a table and is a heuristic.

**1. Load.** `go/packages` type-checks the whole module. A package that fails to
compile is reported and skipped, not fatal — real repos usually have one.

**2. Index.** Every declared function gets a *code reference*: package, symbol,
and a hash of its syntax tree. That hash is how a later run knows whether a
function changed. It walks the tree, not the text, so comments and formatting
never affect it.

**3. Build the call graph.** SSA, then CHA (class hierarchy analysis). CHA
assumes any implementing type could receive an interface call. That means extra
edges, never a missing one — and for a payment flow, a missed path is the
expensive mistake.

**4. Discover what the entry points reach.** The entry points are *read from
`testigo.json`*, never searched for — see above. An empty list is an error, not
an empty result.

```
worklist = [entry points]
while worklist is not empty:
    fn = take one
    for each callee of fn:
        if callee is outside this module:   skip it
        record the edge  fn -> callee, WITH THE LINE NUMBER of the call site
        if callee not seen:  mark seen, add to worklist
```

This step computes a **set**, not a sequence. Every reachable function is
processed exactly once and emits all its edges when it is, so the order the
worklist is drained in cannot be observed in the output — swap the queue for a
stack and `flow.json` comes out byte-identical. There is no traversal decision to
defend here.

The ordering data is the **line number on each edge** — the position of the call
expression *inside the caller's body*, not the position of the callee's
declaration. Two things get called "order" here and only one is used:

| | | used? |
|---|---|---|
| declaration order | where `func A` sits in the file | **no, never** |
| call-site order | where the call sits inside a body | **yes** |

Go allows forward references at package level, so declaration order means
nothing. Statement order inside a body does mean something: those statements run
in that sequence. `Node.Calls` is stored sorted by it, and
`TestDeclarationOrderIsIgnored` writes a package declared in reverse to prove it.

Three things make the discovery correct rather than merely reachable:

**5. Extract facts per function.** From the SSA instructions: does it open a
transaction, commit, roll back, leave the process, read the clock, generate an
ID, start a goroutine, carry a money type. And for each call that leaves the
process, whether a test could substitute it — see *Injectability* below.

**6. Extract by pattern.** State machines, money fields and I/O boundaries are
found by NAME, using the tables in `internal/scanningFlow/patterns/`. These are
heuristics, not proofs, and they are the first thing to check when a result
surprises you.

Then `.testigo/flow.json` is written atomically: temp file, then rename, so a
crash never leaves half a file behind.

### The sidecar file
This file stores everything Testigo discovered about your payment flow scan, such as:
* functions (Nodes)
* what they call
* compiler/SSA facts
* external boundaries (Seams)
* state machines
* detected findings
* agent notes
* entry points
* infrastructure found in migrations, compose files and config

Your GO source stays untouched; flow.json stores Testigo's knowledge about it.

**Size.** About 2.7 KB per function, so a 400-function service produces roughly
1 MB, or 130 KB gzipped. Add `.testigo/flow.json` to `.gitignore` — it rebuilds
in seconds and costs nothing. Do **commit** `.testigo/knowledge.json`: that one
holds the agent answers, and those cost real money to produce.



## What it finds today

| id | severity | what |
|---|---|---|
| `MONEY-FLOAT` | critical | money in a `float32`/`float64` field, parameter or result |
| `MONEY-DIV` | high | money divided with no stated rounding rule |
| `MONEY-NO-CURRENCY` | medium | a bare integer amount with no currency beside it |
| `TX-NET-CALL` | critical | a network call inside a database transaction |
| `TX-NO-ROLLBACK` | high | `BeginTx` with no rollback in the same function |
| `SEAM-CONCRETE` | high | I/O on a concrete type — faults cannot be injected |
| `STATE-NEVER-SET` | medium | a declared status constant nothing ever assigns |
| `IDEM-KEY-NOT-UNIQUE` | critical | an idempotency key field with no UNIQUE index in any migration |
| `LOAD-ERROR` | info | a package did not type-check; its results are partial |

`IDEM-KEY-NOT-UNIQUE` is worth a note. It compares two files that never mention
each other: a Go struct with a key field, and the migrations. If nothing makes
that column unique, then whatever prevents duplicates in Go is a read followed by
a write, and two requests arriving together can both pass the read. That is a
real double-spend window, found with no agent and no running code.

## Injectability

The most useful field in the output. SSA reports an interface method call as an
*invoke*, and an invoke is dispatched through a value the caller was handed —
which a test can substitute. A static call on a concrete `*sql.DB` or
`http.DefaultClient` cannot be intercepted at all.

This is not a heuristic, and it decides whether a failure test can be written.
Most serious payment bugs only appear when something downstream fails: the
provider times out after the debit, the COMMIT fails, the clock crosses an
expiry. If the seam is concrete, none of those are testable, and saying so is
more honest than pretending otherwise.

## Design notes

**CHA over VTA for the call graph.** CHA over-approximates: for an interface
call it assumes every implementing type could be the receiver. False edges, but
never a missing one. For a payment flow, missing a path is the expensive
mistake; an extra path costs only noise.

**Reach propagation stops at the module boundary.** A function you own
contributes everything it can reach; a dependency contributes only what it does
directly. Without this bound, CHA unions the reach of every `error`
implementation in the program and the tool concludes that formatting an error
message touches the database, the network and the random source. Correct as a
bound, useless as information. `TestDoesNotReportNoise` keeps it that way.

**Facts and notes are separate fields.** Facts are recomputed from source every
run and always win. Only notes are merged, because only notes are expensive.

## Tests

```sh
go test ./...
```

`testdata/paysvc` is a deliberately broken payment service. Every defect in it
was planted, and `TestFindsPlantedDefects` asserts each one is still found. That
is the only test that means anything for a bug-finding tool: a unit test on the
parser proves the parser parses, not that the tool would catch a float64
balance.

## PHASE TWO: AskingTheAgent

Phase 1 stops at a wall. A compiler can prove what the code *does*. It cannot
know what the code *should* do, because that is a business rule.

Phase 2 asks about that, in two rounds.

**Round 1 collects context. It writes no code.**

| Question | What it settles |
|---|---|
| binding | which type is money, which call commits it, where the idempotency key lives |
| transitions | which state changes are impossible, and which states are final |
| externalEffect | what each outside call does to the world, and whether a rollback undoes it |
| notes | what each step means in business terms |

**Round 2 asks for tests**, using round 1's answers as given.

Round 2 is *blocked* until round 1 is answered. If the agent decides the wrong
call commits the money, a test written in the same reply will faithfully test the
wrong call and pass forever. Splitting the rounds gives you one place to look and
one thing to fix, before any code exists.

**How they talk.** `testigo ask` writes markdown to `.testigo/asks/`. Your agent
reads it and writes JSON to `.testigo/answers/`. `testigo collect` validates and
applies. No API key, no network, any agent. The prompts are plain files you can
read and edit before spending anything on them.

### How phase 2 keeps the token bill down

Four things, and they compound.

**1. The flow file becomes steps, not JSON.**
`flow.json` is a graph keyed by symbol, which is right for a program and wrong
for a prompt. Before rendering, testigo walks it into an ordered list of *steps*
per entry point, each already annotated with what it touches:

```
step 2  (*Server).process
        api/server.go:35
        proved: opens a transaction, commits, makes a network call
        leaves process: (psp.Gateway).Authorize  [http]  line 58  injectable
        sets status: StatusPending  line 44  inside a transaction
```

The agent never parses JSON, never re-derives the call graph, and never gets the
parts of the file it has no use for.

**2. Each question gets only its own facts.**
A currency-conversion prompt does not carry the database call graph. Seams are
scoped to the entry point that question is about and deduplicated by target.

**3. The shared rules are written once.**
Everything true of every question lives in one `PREAMBLE.md` the agent reads
once, instead of being repeated in each prompt. On a 19-case pack that alone
saves about 19k tokens.

**4. Questions that cannot apply are never asked.**
23 scenarios live in the catalog as Go data, which costs nothing. Only the ones
whose preconditions match your repository get rendered. A repo with no balance
reader never sees a conservation test; a repo with no queue never sees a
duplicate-delivery test. `testigo cases` shows the list and the estimated cost
before you spend it.

Round 1 also feeds round 2: once binding says there is no balance function,
three scenarios drop out before a single prompt is written.

### testigo.rules.json

The one thing testigo cannot work out from code: what *moving money* means here.

It is not the same event everywhere. A wallet moves money when a balance row
changes. A switch moves money when it forwards an authorization. A
service-activation system moves money when it reports a status to a settlement
provider, and the provider settles later on that report. A tool that assumed the
first definition would generate confident, wrong tests for the other two.

`testigo rules` writes a commented starter file. Everything in it is optional,
and the field worth the most thought is `money_movement.external_signal`: set it
only if money moves in your system because a message was *sent*, not because a
row changed. That single fact changes which tests make sense.

The file also carries your retry policy — whether one exists is a fact to test,
not something testigo should assume in either direction — plus invariants only
your team knows, and scenarios to skip with a reason.

### Not built yet

Phase 3: running the generated suite at scale, mutation testing to score whether
those tests could actually fail, and the final report.

## Dependencies

One: `golang.org/x/tools` (go/packages, go/ssa, go/callgraph). Everything else
is the standard library.

**`vendor/` is committed on purpose.** proxy.golang.org rate-limits and
geo-blocks, and a 429 in the middle of a build is not a problem worth having
twice. With the vendor directory present, Go never contacts the network:

```sh
go build -mod=vendor ./cmd/testigo
go test  -mod=vendor ./...
```

Set it once and forget the flag:

```sh
go env -w GOFLAGS=-mod=vendor
```

That is scoped to your machine, not this repo, so unset it (`go env -u GOFLAGS`)
if it gets in the way of another project.

If you ever need to refresh or add a dependency, and the proxy is refusing you,
a mirror usually works:

```sh
go env -w GOPROXY=https://goproxy.io,direct
go mod tidy && go mod vendor
```

And if every proxy is blocked, clone from GitHub and point at it — this is
exactly how this repo was built:

```sh
git clone --depth 1 --branch v0.35.0 https://github.com/golang/tools ~/deps/tools
git clone --depth 1 --branch v0.26.0 https://github.com/golang/mod   ~/deps/mod
git clone --depth 1 --branch v0.16.0 https://github.com/golang/sync  ~/deps/sync
# then add a replace block to go.mod pointing at those paths, and:
GOFLAGS=-mod=mod GOPROXY=off go mod vendor
# finally strip the local paths back out of vendor/modules.txt and go.mod
```

Config is JSON with `//` line comments stripped, not YAML, on purpose.
`gopkg.in/yaml.v3` declares `go 1.11` in its go.mod, which predates module graph
pruning, so depending on it pulls in its whole test dependency graph —
check.v1, kr/pretty, kr/text. Eight lines of comment-stripping was the better
trade for a ten-line config file.


## One hazard worth knowing

`go list` finds a `go.work` by walking **up** from the target directory. If your
payment repo sits anywhere under an unrelated workspace file, packages outside
that workspace vanish from the load and the only symptom is "entry point not
found".

testigo therefore loads every repository with `GOWORK=off`, always. It is not an
option you can pass, because there is no situation in which inheriting a
workspace file from some parent directory is the behaviour you wanted — the repo
under test is analysed on its own terms or the result is not about that repo.
This bit me while building the fixture, which is why the line in `scan.go` has a
comment on it.
