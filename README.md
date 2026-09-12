# testigo

testigo reads a Go payment codebase, proves everything a compiler can prove, and asks
an agent only about what is genuinely left over. Then it plans tests from both.

```sh
go install ./cmd/testigo
```

## The one idea

Pointing an LLM at a codebase and asking it to read everything is unbounded: a wrong
answer looks exactly like a right one, so the model has no reason not to invent.

testigo splits the work in two.

| Phase | Name | What it does | Cost |
|---|---|---|---|
| 1 | ScanningTheFlow | Go, SSA and the type checker prove what is provable: reachable functions, seams, state machines, money fields, findings | free, no network |
| 2 | AskingTheAgent | an agent answers only the residue, as bounded questions whose answers can be checked against the code | tokens |

Everything phase 1 proved is pasted into each prompt as fact, so the agent is never
asked to re-derive it and can never contradict it.

## The pipeline, on the bundled fixture

`testdata/paysvc` is a deliberately flawed payment service — nine functions, every
defect planted. Run these in order. The output below is real.

### 1. Tell it where money enters

testigo follows the call graph from entry points you declare. The fixture already has
three: the API handler, the provider webhook and the reconciliation job. Following
only the API would miss the webhook, which is where double-credit bugs live.

```sh
testigo init .
testigo entry . add 'example.com/paysvc/api#(*Server).CreatePayment' "API create"
```

### 2. Scan — the free part

```
$ testigo scan testdata/paysvc
nodes       9 functions reachable from the entry points
seams       4 injectable, 11 on concrete types
states      example.com/paysvc/domain.PaymentStatus: 6 states, 4 write sites
findings    5 critical, 9 high, 4 medium, 0 info

  [critical] IDEM-KEY-NOT-UNIQUE  Payment.OrderID is an idempotency key with no unique constraint behind it
             domain/payment.go:22
  [critical] MONEY-FLOAT          Payment.Amount stores money in a float
             domain/payment.go:24
```

Writes `testigo/flow.json`. Your source is never touched.

### 3. Ask — see the plan before you spend

```
$ testigo ask --explain --round 1 testdata/paysvc
ask plan   22 scenario(s) runnable, 1 blocked
budget     none; output counted 5x

ASKED
  questions                free text    5.5k in + 1.4k out = 12.5k
  mainEntity               free text    1.8k in +  512 out =  4.4k
  stateRoles x2            free text     471 in +  524 out =  3.1k
  moneyModel               free text    1.1k in +  308 out =  2.6k
  externalEffect x4        true/false   1.1k in +  100 out =  1.6k

cost       10.0k in + 2.8k out = 24.2k
```

Writes markdown into `testigo/asks/`. There is no API key and no network: point your
agent at `testigo/asks/INSTRUCTIONS.md` and it writes JSON into `testigo/answers/`.
`--budget N` caps the weighted tokens the planner commits.

### 4. Collect — validate and apply the answers

```sh
testigo collect testdata/paysvc
```

Nothing here can tell whether an answer is *correct*. It can tell whether the answer
is about this repository at all: a state that does not exist, a declared state left
out, a claim with no `file:line`, the prompt's own example sent back verbatim.

### 5. Cases — the test plan, priced, spending nothing

```sh
testigo cases testdata/paysvc
```

Read the *not applicable* list too. A scenario that cannot run here, with its reason,
is information.

### 6. Report — one file, written by the agent

```sh
testigo report testdata/paysvc         # a quick summary, printed
testigo report --ask testdata/paysvc   # a prompt; the agent writes testigo/REPORT.md
```

Every claim in the report must cite `file:line`, and every fix must be safe for money:
nothing that could charge twice, drop a payment or lose the audit trail. A database
constraint beats a check in Go, and the fix's cost has to be stated.

## The nine findings phase 1 proves on its own

| ID | Severity | What it is, and why it ranks there |
|---|---|---|
| `MONEY-FLOAT` | critical | An amount stored in a float. Binary floating point cannot hold 0.10 exactly, so sums drift and equal amounts compare unequal. Critical because it corrupts the amount itself, everywhere it flows. |
| `IDEM-KEY-NOT-UNIQUE` | critical | A key meant to stop repeats, with no unique index behind it. Two identical requests both pass "have I seen this?" before either writes. Critical because that is the classic double charge. |
| `TX-NET-CALL` | critical | A network call inside an open database transaction. Locks are held for the provider's timeout, so a slow provider becomes a database outage, and a rollback cannot undo what the provider already did. |
| `MONEY-DIV` | high | Money divided with no rounding rule. Integer division drops the remainder, and a few lost minor units per operation become books that will not balance at month end. |
| `TX-NO-ROLLBACK` | high | A transaction opened with no rollback on its early-return paths. It leaks a connection and holds locks — an outage shape rather than a wrong amount, hence high. |
| `SEAM-CONCRETE` | high | I/O called on a concrete type, so no test can make it fail or time out. Most payment bugs only appear when something downstream fails, so those paths go untested. |
| `MONEY-NO-CURRENCY` | medium | A bare integer amount with no currency beside it. Nothing stops adding EUR to USD. Cheap while the system has one currency; expensive the day it gets a second. |
| `STATE-NEVER-SET` | medium | A declared status nothing in the code produces. Either it is dead, or something outside Go sets it — and either way nobody tests the transition. |
| `LOAD-ERROR` | info | A package did not type-check. Everything behind it is invisible to the scan: its findings are missing, not absent. |

## Seams, and how testigo decides one is real

A seam is a call where the flow leaves the process: a database, an HTTP or gRPC call,
a broker, a cache. Injectable seams go through an interface your repository defines,
so a test can make them fail.

A call is first a *candidate* because of the package it lives in. It is kept only if
the call graph shows it can reach a real socket or database round trip. On the way,
testigo does not follow:

- plumbing interfaces — `error`, `io.Reader`/`io.Writer`, `context.Context`, `fmt.Stringer` — which resolve to every implementation in the program, including socket reads;
- callbacks with a generic shape such as `func(rune) rune`, which link to every function of that shape;
- library code calling back into your application.

Sends that hand work to a background goroutine — `Publish`, `Send`, `Request` — are
kept by name, because their socket write is not on any static path. Clock and random
seams exist for determinism, not I/O, and are never filtered.

On the recharge service this took effect seams from 35 targets to 15. Every real seam
survived; `grpc/status.New`, `metadata.MD.Get`, go-pg's query builders and
`(error).Error` did not. The call graph still over-approximates: this is a filter, not
a proof.

## Phase 2: what gets asked, and why

**Only a scenario's requirements decide what is asked.** A payment kind selects
scenarios; each scenario runs as a technique; scenarios and techniques declare the
questions they cannot be written without. Nothing else gets asked.

| Ask | What it settles |
|---|---|
| `moneyModel` | which field is money, which function moves it, which field is the idempotency key |
| `mainEntity` | which struct the flow moves, and which identifier a client repeats on a retry |
| `questions` | the scenario and technique questions, true/false wherever a boolean will do |
| `stateRoles` | the role of each state of one state machine; testigo derives the legal transitions |
| `externalEffect` | five verdicts about one seam: survives a rollback, observable after a timeout, deduplicated, moves money |

The planner prices every ask — output counts five times input, roughly what it bills —
and skips anything no runnable scenario needs, or whose fields nothing reads.

Round-one answers feed round two. Each test prompt receives only its own scenario's
answers: the states to refuse for a state test, the verdicts for the seams it fakes,
the key facts for an idempotency test, its own question answers.

### What happened to notes?

Notes were a per-function ask: "what does this function do in business terms?", with
a step label, a purpose, effects, assumptions and a confidence. They were stored on
each node with a hash of the function body, so an edit made them stale.

They were removed. No scenario required them. Only the step label was ever read, as a
diagram label. `effects` repeated what SSA already proves, and the useful part of
`assumptions` is now asked directly, as scenario questions. On the payment service
notes were the largest single cost: 381k of 618k weighted tokens in round one. The
stale-and-rename machinery that existed only to carry notes between scans went with
them.

### What is the preamble?

`testigo/asks/PREAMBLE.md` is written once per round and holds everything the asks in
that round would otherwise repeat. In round one that is what was proved, the
migrations, the flow rendered step by step, and each kind's rules and answer shape. In
round two it is the rules for writing tests.

Each ask tells the agent to read the preamble once and cites step numbers instead of
reprinting the flow. A test fails if any text is identical across the asks of one
kind, because that text belongs in the preamble. On the fixture, hoisting saves about
34.8k tokens against 14.4k actually sent.

## Vocabulary

| Term | Meaning |
|---|---|
| Seam | a call that leaves the process; injectable if it goes through an interface your repo defines |
| Scenario | one of 23 catalogued ways a payment system breaks, in seven families; declares what it `Requires` |
| Technique | how a scenario is tested: fault injection, concurrency, property, table, fuzz, metamorphic, state machine, narrow integration… |
| Question | one thing testigo cannot prove, with the problem it guards against and where the answer lives |
| Payment kind | spine (double-entry, wallet, stateless), motions (top-up, payout, escrow…) and overlays (refund, reconciliation, FX) |
| Finding | something phase 1 proved on its own, with a severity and a location |
| Planner | decides which asks are worth their tokens, and explains why |

## Code map

| Package | Owns |
|---|---|
| `cmd/testigo` | the CLI: one function per command |
| `internal/scanningFlow` | phase 1 — SSA, the call graph, seams, state machines, money, findings |
| `internal/scanningFlow/patterns` | name and package matching; word-boundary aware, so `Current` is not a currency |
| `internal/scanningFlow/flowEntity` | the flow's types: nodes, seams, state machines, findings |
| `internal/testPlan` | the scenario catalogue, the plan types, and the selector that decides what applies |
| `internal/agent/domain` | the agent's vocabulary: questions, asks, answers, payment kinds, and the kind → scenario → question maps |
| `internal/agent/planner` | demand, the consumer registry, the cost model, `--explain` and `--budget` |
| `internal/agent/prompts` | one file per prompt, plus the preamble |
| `internal/agent` | plans asks, writes the pack, collects answers, writes and runs generated tests |
| `internal/config` | everything under `testigo/`: config, rules, and `flow.json` |
| `internal/report` | the printed summary and the Mermaid diagrams |

## House rules

- **No comments in Go.** Only the author writes them. Meaning goes in names, test names and prompt text.
- **Every claim carries a `file:line`.** Answers, findings and the report alike.
- **Ask bounded questions.** A finite answer space makes completeness mechanical.
- **Defaults point at the safe answer.** Anything not literally `true` is not retry-safe. Wrongly assuming a charge is safe to retry bills a customer twice; the reverse costs one extra test.
- **Only scenario requirements drive what is asked or used.**
- **Don't pay for what you already have, or won't read.**

## When a repository will not load

Phase 1 needs every package to type-check. Generated protobuf packages that come from
a git submodule have to exist first: `git submodule update --init`, then the
repository's own `protoc` step. If `go env GOFLAGS` forces `-mod=vendor` on a module
whose vendor directory is stale, run the scan with `GOFLAGS=-mod=mod`.

## Still half-built

- `TX-NO-ROLLBACK` does not recognise `tx.Close()`, which rolls back in go-pg.
- The seam filter over-approximates; a VTA call graph would be more precise than CHA.
- Round two has not yet been run end to end on a real repository.
- True/false verdicts are collected per question, but nothing rolls them up into per-area conclusions yet.
