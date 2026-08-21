# TESTIGO
Test your *GO Fintech* code with some help from your *AGENT!!*

## Install and run

```sh
go build -o testigo ./cmd/testigo

./testigo entries ~/work/payments   # suggest entry points
./testigo init    ~/work/payments   # write testigo.json
$EDITOR ~/work/payments/testigo.json
./testigo scanningFlow    ~/work/payments   # extract, write .testigo/flow.json
./testigo flow    ~/work/payments   # print the Mermaid diagram
```
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

Your GO source stays untouched; flow.json stores Testigo's knowledge about it.



A payment flow has more than one entry point. The API handler starts it, but the
provider webhook, the reconciliation job and the queue consumer all rejoin the
same state machine. `entries` is a list because listing only the first one hides
exactly the bugs worth finding.


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
| `LOAD-ERROR` | info | a package did not type-check; its results are partial |

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

## Not built yet

Phase 2 (agent annotation, legal-transition audit), phase 3 (test synthesis with
a compile/verify loop), mutation testing to score the generated suite, and the
report. See `design-review.md`.

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
found". `scan.Options.Env` exists for this — pass `GOWORK=off` and the
repository is analysed on its own terms. This bit me while building the fixture,
which is why there is a comment about it in the code.
