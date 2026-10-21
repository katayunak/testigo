# testigo

[![CI](https://github.com/katayunak/testigo/actions/workflows/ci.yml/badge.svg)](https://github.com/katayunak/testigo/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](go.mod)

**testigo finds the bugs that lose money in Go payment code, then gets an AI agent to write the tests that prove them.**

It reads your code with the Go compiler first, which is free and exact. Then it asks
an AI agent only what the compiler can't know, and every answer has to point at a
file and a line.

On the bundled wallet service, whose bugs were planted on purpose, one run wrote
12 tests. All 12 fail, all 12 said in advance that they would, and each failure
names the bug:

```
--- FAIL: TestTransferIsAtomic/settle_fails_between_the_debit_and_the_credit
    the two wallets held 1000 together before the transfer and 700 after it failed: 300 minor units were destroyed
--- FAIL: TestSelfTransferRefused/part_of_the_balance
    a transfer of 100 from w-1 to w-1 was accepted and changed the balance from 500 to 600: money was created from nothing
--- FAIL: TestIdemReplay/a_repeated_deposit_with_the_same_tracking_number_credits_once
    balance after two deliveries of tracking number trk-1 = 200, want 100: the repeat ran the deposit again instead of replaying it
```

Every answer, every test and the final report from that run are in
[examples/walletsvc](examples/walletsvc).

```sh
go install github.com/katayunak/testigo/cmd/testigo@latest
```

Go 1.25+. No API key, no network, no vendor lock-in. The prompts are plain markdown,
so you can read them before you spend anything. It works with any agent through
files, or as an [MCP server](#phase-2-as-mcp-tools) inside Claude Code or any other
MCP client.

## Why I built this

Payment bugs are quiet. A retry charges a customer twice. A transfer fails halfway
and the money is just gone. The test suite stays green the whole time, because it
only ever tried the happy path.

You can point an AI agent at the repo and ask it for tests. It will read everything,
burn a lot of tokens, and write tests that check what the code does instead of what
it should do. I wanted the opposite. Let the compiler do the reading. Use what the
industry already knows about how payment systems break. Pay the model only for the
few things a compiler can't see, and check its answers against the code.

## What it can do

**It reads code like a compiler, because it uses one.** testigo runs the Go type
checker and SSA. Starting from your entry points, it finds every function the flow
reaches and every call that leaves the process: database, HTTP, gRPC, a broker, a
cache. It knows whether a test can fake each of those calls, which writes run inside
a database transaction and which don't, every state machine and every line that
changes a status, and which fields hold money. It reads your migrations too, so it
knows which columns are really unique.

**It finds a dozen kinds of money and transaction risks for free.** Money in a float.
An idempotency key with no unique constraint behind it. A network call inside an open
transaction. A transaction that never rolls back. No AI and no tokens for any of it.

**It knows how payment systems break.** It works out what kind of system you have: a
wallet, a double-entry ledger, a one-shot purchase, top-ups, payouts, escrow, with
refunds, reconciliation or FX on top. Then it picks from a [catalog](CATALOG.md) of
34 failure scenarios in 7 families. Much of it comes from how Stripe, Adyen, Airbnb,
Uber, Monzo, Nubank and TigerBeetle say they test their own money flows.

**It plans its questions the way Postgres plans a query.** It prices every question
before asking it, skips anything no test needs, and shows you the plan and the bill
first. More on that [below](#the-planner).

**It asks questions that can be checked.** True or false wherever possible. Every
answer cites a file and line, and testigo checks it against what the compiler
proved. An answer that names a state that doesn't exist, leaves out a declared one,
has no proof, or just sends back the prompt's own example is rejected with the
reason.

**It writes the tests, then runs them.** The agent writes Go tests. testigo puts them
in your package, builds them, runs `go vet`, and runs them with `-race`. Small tests
are checked mechanically: no sleeping, no wall clock, no sockets, no real database.
Every test has to prove it reached the situation it was written for: the fake was
called, the goroutines really collided. If the agent expects a test to fail, it has
to say so before the test runs. If a test can't be written honestly, the agent marks
it blocked and says why, instead of writing a weaker one.

**It writes a report you can act on.** Every claim has a file and line, and every fix
has to be safe for money: nothing that could charge twice, drop a payment or lose the
audit trail.

**It is cheap.** On a real payment service, one round of questions went from 123.9k
to 30.7k weighted tokens without losing a single test. The
[token story](#the-token-story) explains how.

## The planner

A database never runs a query the first way that comes to mind. Postgres looks at
the query and its table statistics, prices each way of running it, and runs the
cheapest. `EXPLAIN` shows you that plan before anything runs.

testigo does the same thing with the questions it sends to the agent.

| In Postgres | In testigo |
|---|---|
| the query | the tests worth writing here. The selector reads the facts from the scan and decides which of the 34 scenarios can run in this repository, and says why the others can't |
| table statistics | what each answer is worth. A registry lists every answer field and the code that reads it. A field nothing reads is never paid for. Writing that registry down found 35 fields that were asked for on every run and read by nothing |
| the cost model | the price of each question: the size of its prompt plus the answer it expects, with output counted five times, roughly how it's billed |
| access paths | the ways to ask: skip it, verify it with true/false (about 5 output tokens), narrow it to a pick-one (about 15), or ask open free text (21 to 131, measured per kind) |
| `EXPLAIN` | `testigo ask --explain` |
| a cost limit | `--budget N`. The planner keeps the questions that serve the most scenarios per token and drops the rest, and tells you what it dropped |

Here it is on the payment fixture with a budget that is too small for everything:

```
$ testigo ask --explain --budget 12000 --round 1 .
ask plan   30 scenario(s) runnable, 4 blocked
budget     12.0k weighted (output counted 5x)

ASKED
  mainEntity               free text    1.7k in + 512 out = 4.3k
  stateRoles x2            free text    473 in + 524 out = 3.1k
  moneyModel               free text    1.1k in + 308 out = 2.6k
  externalEffect x5        true/false   1.3k in + 125 out = 1.9k

NOT ASKED
  questions                over the 12.0k budget; 11.9k already committed saves 6.3k
  externalEffect x4        over the 12.0k budget; 11.9k already committed saves 3.1k

cost       4.5k in + 1.5k out = 11.9k
avoided    7.0k in + 500 out = 9.5k
saving     44% of the unplanned cost
```

The planner decides what is worth asking. It never decides what is true. That is
still the agent's job, and `testigo collect` checks the agent's work.

## How it works

testigo starts with the entry points of your payment flow and figures out what kind of payment system you have: a credit system, a wallet, a PSP payment, or something else.

Then it reads your code and figures out what is actually happening inside the flow: how money moves, where state changes, what external systems are involved, what can go wrong, and what needs to be tested.

But testigo doesn't just throw all of that code and information at an agent and hope it figures things out.

It prepares the context for the agent.

Different payment flows have different scenarios, and every scenario has its own requirements. testigo figures out what matters for each scenario and gives the agent the relevant information it needs, without wasting tokens on things that don't matter.

So instead of asking the agent to explore your codebase from scratch, testigo does the groundwork first. The agent can focus on reasoning about the actual payment scenarios and generating the right tests.

The result is faster and more focused testing, fewer wasted tokens, and a detailed report showing what was tested and what happened.

### Two phases, and why they are separate

| Phase | Name | What it does | Cost |
|---|---|---|---|
| 1 | `ScanningTheFlow` | the Go type checker and SSA prove what can be proved: reachable functions, seams, state machines, money fields, findings | **free**, no network |
| 2 | `AskingTheAgent` | an agent answers only what is left, as bounded questions whose answers can be checked against the code, through files or MCP tools | tokens |

The split is the whole design. A compiler is free, exact and has no opinion. A model
is expensive, can be wrong, and will answer anything you ask. So the compiler goes
first and settles everything it can, and the model is asked only what is really left,
in a form where a wrong answer can be caught.

---

## Step by step

Steps 1 and 2 are free: no agent, no tokens. **Tokens start at step 3**, the moment
an agent reads the questions.

| Step | Command | Who does the work | Tokens |
|---|---|---|---|
| 1. Tell it where money enters | `testigo init`, `testigo entry` | you | none |
| 2. Scan | `testigo scan` | the Go compiler | none |
| 3. Round 1: answer the questions | `testigo ask --round 1`, then `testigo collect` | your agent answers, testigo checks | **yes** |
| 4. Round 2: write the tests | `testigo ask --round 2`, then `testigo collect` | your agent writes tests, testigo builds and runs them | **yes** |
| 5. The report | `testigo report --ask` | your agent writes `REPORT.md` | **yes** |

Free at any time: `testigo ask --explain` (the plan and its price), `testigo cases`
(the test plan), `testigo report` (a printed summary) and `testigo flow` (the flow as
a Mermaid diagram).

On the wallet example, the three paid steps were about 14.6k, 28.1k and 5.8k tokens
of prompt. That is testigo's own estimate from the files it wrote, not a bill.

`testdata/paysvc` is the fixture for the steps below: a payment service with nine
functions and every defect planted on purpose. The output is real.

### Free: no agent, no tokens

#### 1. Tell it where money enters

testigo never guesses entry points. Which function starts a flow is something you
know and the code doesn't say. A guessed list invites someone to accept it without
reading, and that quietly drops a whole path from the analysis.

```sh
testigo init .
testigo entry . add 'example.com/paysvc/api#(*Server).CreatePayment' "API create"
```

The fixture declares three: the API handler, the provider webhook and the
reconciliation job. Following only the API would miss the webhook, which is exactly
where double-credit bugs live.

#### 2. Scan

```
$ testigo scan .
phase       1 · ScanningTheFlow
module      example.com/paysvc
entries     3
nodes       9 functions reachable from the entry points
seams       4 injectable, 11 on concrete types
states      example.com/paysvc/domain.PaymentStatus: 6 states, 4 write sites
states      example.com/paysvc/domain.SettlementMode: 3 states, 2 write sites

findings    4 critical, 9 high, 5 medium, 0 info

  [critical] IDEM-KEY-NOT-UNIQUE  Payment.IdempotencyKey is an idempotency key with no unique constraint behind it
             domain/payment.go:23
  [critical] MONEY-FLOAT          Payment.Amount stores money in a float
             domain/payment.go:24
  [critical] TX-NET-CALL          network call inside a database transaction
             api/server.go:64

still unknown (phase 2 asks an agent):
  - which state transitions are legal
  - how the uninjectable seams behave under failure
```

It writes `testigo/flow.json` and never touches your source files. You can stop
here: the findings alone are worth reading.

### Paid: your agent works from here

#### 3. Round 1: the agent answers questions

```
$ testigo ask --round 1 .
questions   14
est. cost   ~15.9k tokens across 14 prompt(s)
            ~56.6k saved by hoisting the shared rules into PREAMBLE.md
```

Writing the questions costs nothing. They go into `testigo/asks/` as markdown. The
tokens are spent when your agent reads `testigo/asks/INSTRUCTIONS.md` and writes its
answers as JSON into `testigo/answers/`. Run it with `--explain` first to see the
[plan](#the-planner).

```sh
testigo collect .
```

`collect` can't tell whether an answer is *correct*. It can tell whether the answer
is about this repository at all: a state that doesn't exist, a declared state left
out, a claim with no `file:line`, or the prompt's own example sent back word for
word. A bad answer comes back with the reason, so the agent fixes that one file
instead of redoing the round.

#### 4. Round 2: the agent writes the tests

```sh
testigo ask --round 2 .
testigo collect .
```

Round 2 opens only after round 1 passes `collect`. The agent gets one brief per test
case, with only the answers that case needs. `collect` then writes each test into
your package as `*_testigo_test.go`, builds it, runs `go vet`, and runs it with
`-race`. `testigo report` shows what happened:

```
TESTS — round 2, written by the agent, run with -race
  12 written: 0 passed, 12 failed
  15 blocked
  failed    DOUBLE-ENTRY-SUMS-TO-ZERO          api/double_entry_sums_to_zero_testigo_test.go  (expected)
  failed    IDEM-CONCURRENT                    api/idem_concurrent_testigo_test.go  (expected)
  failed    IDEM-CRASH-AT-STEP                 api/idem_crash_at_step_testigo_test.go  (expected)
  failed    SELF-TRANSFER-REFUSED              api/self_transfer_refused_testigo_test.go  (expected)
  failed    TRANSFER-IS-ATOMIC                 api/transfer_is_atomic_testigo_test.go  (expected)
  ...
```

That is the wallet run. `(expected)` means the agent said before the run that the
test would fail, and why. A red test like that is a finding, not a broken test.

#### 5. The report

```sh
testigo report --ask .   # a prompt; the agent writes testigo/REPORT.md
```

Every claim must cite `file:line`, and every proposed fix must be safe for money:
nothing that could charge twice, drop a payment or lose the audit trail. A database
constraint beats a check in Go, and the cost of the fix has to be stated.

### A complete run

For every step with every output kept, on the second fixture, a wallet service, see
[examples/walletsvc](examples/walletsvc): the round 1 answers, the 12 tests round 2
wrote (all red, each one a planted bug), the 15 cases it blocked and why, and the
`REPORT.md` the agent wrote.

---

## Phase 2 as MCP tools

Steps 3 and 4 pass files back and forth: testigo writes prompts, the agent writes
JSON, and someone runs `collect`. `testigo mcp` removes the files. It serves the same
two rounds as five [MCP](https://modelcontextprotocol.io) tools over stdio, so the
agent asks for work and gets its answers checked in the same conversation.

```sh
claude mcp add testigo -- testigo mcp /path/to/your/repo
```

Any other MCP client works the same way:

```json
{ "mcpServers": { "testigo": { "command": "testigo", "args": ["mcp", "/path/to/your/repo"] } } }
```

Run `testigo scan` first. The server reads `testigo/flow.json`; it does not scan.

| Tool | What it does |
|---|---|
| `testigo_status` | which round is next, why round 2 might be blocked, and what the next round will cost |
| `testigo_round1_get_pack` | the shared preamble once, then every unanswered batch of round-1 questions |
| `testigo_round1_submit_answers` | checks one batch right away: good answers are applied, bad ones come back with what is wrong, missing ones are named |
| `testigo_round2_next_case` | the brief for the next test to write, or all of them with `all: true` |
| `testigo_round2_submit_test` | writes the test, builds it, vets it, runs it with `-race`, and returns the result with failure output trimmed |

Every tool is a thin layer over the functions the CLI already uses, so the files and
MCP give the same answers, the same checks and the same tests. Only the transport is
new.

One lesson from building it: the Go MCP SDK builds each tool's JSON schema from the
Go type. A field typed `json.RawMessage` turns into "an array of bytes", so every real
call failed validation before reaching the handler. `go build` and `go vet` passed.
Only a real call over stdio caught it.

---

## What phase 1 proves on its own

| ID | Severity | What it is, and why it ranks there |
|---|---|---|
| `MONEY-FLOAT` | critical | An amount in a float. Binary floating point cannot hold `0.10` exactly, so sums drift and equal amounts compare unequal. Critical because it corrupts the amount itself, everywhere it flows. |
| `IDEM-KEY-NOT-UNIQUE` | critical | A key meant to stop repeats, with no unique index behind it. Two identical requests both pass "have I seen this?" before either writes. That is the classic double charge. |
| `TX-NET-CALL` | critical | A network call inside an open transaction. Locks are held for the provider's timeout, so a slow provider becomes a database outage. And a rollback cannot undo what the provider already did. |
| `SCHEMA-NOT-FOUND` | critical | The code talks to a database, but no schema was found. Every uniqueness claim is then held back instead of guessed. Critical because without the schema, nothing above can say whether the database refuses a duplicate. |
| `MONEY-DIV` | high | Money divided with no rounding rule. Integer division drops the remainder, and a few lost minor units per operation become books that will not balance. |
| `TX-NO-ROLLBACK` | high | A transaction with no rollback on its early-return paths. It leaks a connection and holds locks: an outage, not a wrong amount. |
| `TX-UNUSED` | high | A transaction is opened and committed, but no statement runs on it. Everything between begin and commit runs outside it, so nothing it was meant to protect is atomic. |
| `TX-BYPASS` | high or medium | Database work runs beside the open transaction instead of in it. That work commits on its own: a rollback leaves it in place. High when the call is proved to be database work, medium when that is guessed from names. |
| `SEAM-CONCRETE` | high | I/O on a concrete type, so no test can make it fail or time out. Most payment bugs only appear when something downstream fails, so those paths go untested. |
| `MONEY-NO-CURRENCY` | medium | A bare integer amount with no currency beside it. Nothing stops adding EUR to USD. Cheap with one currency; expensive the day there are two. |
| `STATE-NEVER-SET` | medium | A declared status nothing produces. Either it is dead, or something outside Go sets it. Either way, nobody tests that transition. |
| `MIGRATION-NO-DOWN` | medium | Migrations with no down step, under a tool that expects one. A test that builds the database from migrations cannot tear it down between cases, and a bad migration has no rehearsed way back. |
| `LOAD-ERROR` | info | A package did not type-check. Everything behind it is invisible: its findings are **missing, not absent**. |

A finding is evidence of a risk, not proof of a bug. `STATE-NEVER-SET` cannot know
whether another service writes that state. It reports the fact and leaves the
judgement to you.

Each finding also says how it was reached. `proved` means the type checker, SSA or
the schema showed it. `heuristic` means it depends on names: that a field called
`Amount` is money, or that `PSP.Authorize` is an HTTP call. A heuristic finding is
wrong when the code is named unusually.

---

## How testigo decides a boundary is real

A seam is a call where the flow leaves the process: a database, an HTTP or gRPC call,
a broker, a cache. It is **injectable** if it goes through an interface your
repository defines, because then a test can replace it and make it fail.

A call is first a *candidate* because of the package it lives in. It is kept only if
the call graph shows it can actually reach a socket or a database round trip. On the
way there, testigo refuses to follow:

- **plumbing interfaces** (`error`, `io.Reader`/`io.Writer`, `context.Context`, `fmt.Stringer`), which resolve to every implementation in the program, including socket reads;
- **generic callbacks** such as `func(rune) rune`, which link to every function of that shape;
- **library code calling back into your application**, since a library's own I/O cannot depend on the callbacks you register.

Sends that hand work to a background goroutine (`Publish`, `Send`, `Request`) are
kept by name, because their socket write is not on any static path at all.

On a real service this took seams from **165 to 69**, and effect-seam targets from 35
to 15. Every real seam survived: NATS publish, go-pg insert and select, the order
repository. The false ones did not: `grpc/status.New`, `metadata.MD.Get`, go-pg's
query builders, `(error).Error`. These numbers were measured with testigo's own
scanner, before it moved to gofacts, and have not been re-measured.

It is a filter, not a proof. CHA still over-approximates.

---

## What phase 2 asks, and why

**Only a scenario's requirements decide what is asked.** A payment kind selects
scenarios; each scenario runs as a technique; scenarios and techniques declare the
questions they cannot be written without. Nothing else is asked, ever.

| Ask | What it settles |
|---|---|
| `moneyModel` | which field is money, which function moves it, which field is the idempotency key |
| `mainEntity` | which struct the flow moves, and which identifier a client repeats on a retry |
| `questions` | the scenario and technique questions, true/false wherever a boolean will do |
| `stateRoles` | the role of each state of one machine; testigo derives the legal transitions itself |
| `externalEffect` | five verdicts about one seam: survives a rollback, observable after a timeout, deduplicated, moves money |

Round-one answers feed round two, **scoped per case**: a state test receives the
states it must refuse, a fault-injection test receives the verdicts for the seams it
fakes, an idempotency test receives the key facts. No case sees another scenario's
raw answers. It can see what they add up to: the true/false verdicts roll up into
claims about money, seams, flow and the main entity, and a case gets a claim only
when the claim names its scenario.

Round 2 is gated on round 1 for a reason. If the agent names the wrong money-moving
call, a test written in the same breath would encode that mistake and pass forever.

---

## The token story

This is the part that made the project worth building.

**Output is what costs.** The planner weights output five times input, which is
roughly how it bills. Measuring that way changes every decision downstream.

Four things brought round one on a real payment service from **123.9k to 30.7k
weighted tokens**, without dropping a single test:

1. **Ask only what a runnable scenario requires.** The planner prices every candidate
   ask and skips any that no runnable scenario needs.
2. **Never buy a field nothing reads.** A registry records, for every answer field,
   what consumes it. A field with no reader should not exist, and 35 of them did.
3. **Hoist everything repeated into one preamble.** Shared rules and the rendered
   flow are written once into `PREAMBLE.md`; each ask cites step numbers instead of
   reprinting them. On the fixture that is ~56.6k saved against ~15.9k actually sent.
   A test fails the build if any text is identical across the asks of one kind.
4. **Prefer a boolean to a paragraph.** A true/false question costs about 5 output
   tokens; an open one costs 40 to 130. Most things worth knowing are decidable.

The largest single win was deleting a feature. Per-function "notes" (what does this
function do in business terms?) cost **381k of 618k** weighted tokens in round one,
and no scenario required them. They are gone.

See [studies/contextEngineering.md](studies/contextEngineering.md) for the full
argument and the measurements.

---

## Vocabulary

| Term | Meaning |
|---|---|
| **Seam** | a call that leaves the process; injectable if it goes through an interface your repo defines |
| **Scenario** | one of 34 catalogued ways a payment system breaks, in 7 families; declares what it `Requires`; see [CATALOG.md](CATALOG.md) |
| **Technique** | how a scenario is expressed: fault injection, concurrency, property, table, fuzz, metamorphic, state machine, narrow integration… |
| **Question** | one thing testigo cannot prove, with the problem it guards against and where the answer lives |
| **Payment kind** | spine (double-entry, wallet, stateless), motions (top-up, payout, escrow…) and overlays (refund, reconciliation, FX) |
| **Finding** | something phase 1 proved on its own, with a severity and a location |
| **Oracle** | whatever tells a test what "correct" means, and where that came from |

---

## Code map

| Package | Owns | Docs |
|---|---|---|
| `cmd/testigo` | the CLI: one function per command, plus the MCP server | [README](cmd/testigo/README.md) |
| `internal/docs` | finds the repository's own markdown docs and ranks them for the round-one prompt | [README](internal/docs/README.md) |
| `internal/testPlan` | the scenario catalogue, the plan types, and the selector | [README](internal/testPlan/README.md) |
| `internal/agent` | phase 2: plans asks, writes the pack, collects and validates answers | [README](internal/agent/README.md) |
| `internal/agent/domain` | the agent's vocabulary: questions, asks, answers, payment kinds | [README](internal/agent/domain/README.md) |
| `internal/agent/planner` | demand, the consumer registry, the cost model, `--explain` | [README](internal/agent/planner/README.md) |
| `internal/agent/prompts` | one file per prompt, plus the preamble | [README](internal/agent/prompts/README.md) |
| `internal/config` | everything under `testigo/`: config, rules and `flow.json` | [README](internal/config/README.md) |
| `internal/report` | the printed summary and the Mermaid diagrams | [README](internal/report/README.md) |

9 packages, ~10.0k lines of Go and ~3.5k lines of tests.

Phase 1 itself (SSA, the call graph, seams, transactions, state machines, the
schema, money and the findings) is not in this repository. It is
[gofacts](https://github.com/katayunak/gofacts), a library testigo shares with
[1819](https://github.com/katayunak/1819), a code reviewer that checks diffs against
the same facts. `cmd/testigo` calls it, and `flow.json` is its `fact.Flow` type.

---

## House rules

- **No comments in Go.** Meaning goes in names, test names and prompt text. Only the author writes comments.
- **Every claim carries a `file:line`.** Answers, findings and the report alike.
- **Ask bounded questions.** A finite answer space makes completeness mechanical.
- **Defaults point at the safe answer.** Anything not literally `true` is not retry-safe. Wrongly assuming a charge is repeatable bills a customer twice; the reverse costs one extra test.
- **Only scenario requirements drive what is asked or used.**
- **Don't pay for what you already have, or won't read.**

---

## When a repository will not load

Phase 1 needs every package to type-check. Generated protobuf packages from a git
submodule have to exist first: `git submodule update --init`, then the repository's
own `protoc` step.

testigo does not use your `GOFLAGS`. gofacts sets the go command's environment
itself: it uses the repository's `vendor/` directory when there is one, and
otherwise refuses to change `go.mod` (`-mod=readonly`). So `GOFLAGS=-mod=mod` no
longer helps. If the vendor directory is stale, the scan fails with `inconsistent
vendoring`, and the fix is to bring it up to date with `go mod vendor` in that
repository. There is no switch yet to make testigo skip a stale vendor directory.

The same rules apply when testigo builds and runs a generated test after round two.
Those runs also turn cgo on, because `go test -race` needs it, and each one is
stopped after five minutes.

---

## Limits, stated plainly

- The seam filter over-approximates: it walks the CHA call graph. gofacts also builds a VTA graph, but uses it only to decide which functions call which, not which calls reach I/O.
- The roll-up into per-area claims counts only true/false verdicts. The per-seam verdicts and the money model reach cases directly, but they are not part of it.
- Seven true/false questions are not rolled up, because a `true` or `false` alone does not say which way the system leans. `NotRolledUp` in `internal/agent/domain/featureCatalog.go` gives the reason for each.
- Recheck questions, where the agent re-confirms a fact phase 1 proved with the proof attached, are built and tested, but nothing produces one yet.
- A round 1 answer that says a scenario does not apply here does not remove it from round 2. The agent marks it blocked instead, which costs a little output.
- A `medium` case needs a database driver in the scanned module's `go.mod`. testigo does not check for one before planning the case, so without a driver the agent can only block it.

---

## Running medium and integration tests

A case marked `medium` is allowed to open a real database. A small test is not.
`docker-compose.yml` at the repo root brings up disposable Postgres, Redis, and
Kafka for exactly that:

```bash
docker compose up -d
docker compose ps
```

Every container has a healthcheck; wait for `healthy` before running anything
against them. Nothing is persisted. No named volumes are declared, so
`docker compose down` leaves no data behind and the next `up` starts clean.

Connection details a generated `medium` test should use:

| Service | Address | Credentials |
|---|---|---|
| Postgres | `localhost:5432` | `testigo` / `testigo`, database `testigo` |
| Redis | `localhost:6379` | none |
| Kafka | `localhost:9092` | none |

These are also the values named in the round-two prompt (`internal/agent/prompts/testCase.go`)
so a generated test and this compose file agree without anyone having to remember
to keep them in sync by hand.

---

## Studies

Every scenario testigo knows is listed in **[CATALOG.md](CATALOG.md)**: 34 of them,
with what each needs, what passing means, and the specific wrong versions of each test.

The research this is built on lives in [`studies/`](studies/).

- [How real fintech systems test payment flows](studies/howRealFintechSystemsTestPaymentFlows.md): 19 practices from Stripe, Adyen, Airbnb, Uber, Monzo, Starling, Nubank, Jepsen, TigerBeetle and others, each turned into a catalogue scenario.
- [How tables are written, and how that breaks](studies/howTablesAreWrittenAndHowThatBreaks.md): the write-pattern classifier behind scenarios like `LOST-UPDATE` and `UPSERT-HIDES-A-SECOND-EFFECT`.
- [What Formance Ledger taught us](studies/whatFormanceLedgerTaughtUs.md): reading a real open-source double-entry ledger end to end: one new scenario, three gaps we named instead of guessing at, and one thing even a mature ledger team leaves untested.
- [Context engineering](studies/contextEngineering.md): how to get a better answer from a model for fewer tokens, measured on this project.
