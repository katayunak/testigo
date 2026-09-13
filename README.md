# testigo

Testigo starts with the entry points of your payment flow and figures out what kind of payment system you have — whether it's a credit system, wallet, PSP payment, or something else.

Then it reads your code and figures out what is actually happening inside the flow: how money moves, where state changes, what external systems are involved, what can go wrong, and what needs to be tested.

But Testigo doesn't just throw all of that code and information at an agent and hope it figures things out.

It prepares the context for the agent.

Different payment flows have different scenarios, and every scenario has its own requirements. Testigo figures out what matters for each scenario and gives the agent the relevant information it needs, without wasting tokens on things that don't matter.

So instead of asking the agent to explore your codebase from scratch, Testigo does the groundwork first. The agent can focus on reasoning about the actual payment scenarios and generating the right tests.

The result is faster and more focused testing, fewer wasted tokens, and a detailed report showing what was tested and what happened.


```sh
go install ./cmd/testigo
```

Go 1.22+. No API key, no network, no vendor lock — the prompts are markdown you can
read before you spend anything.

---

## Two phases, and why they are separate

| Phase | Name | What it does | Cost |
|---|---|---|---|
| 1 | `ScanningTheFlow` | the Go type checker and SSA prove what is provable: reachable functions, seams, state machines, money fields, findings | **free**, no network |
| 2 | `AskingTheAgent` | an agent answers only the residue, as bounded questions whose answers can be checked back against the code | tokens |

The split is the whole design. A compiler is free, exact and has no opinion; a model
is expensive, fallible and will answer anything you ask. So the compiler goes first
and settles everything it can, and the model is asked only what is genuinely left —
in a form where a wrong answer can be caught.

---

## The pipeline, on the bundled fixture

`testdata/paysvc` is a deliberately flawed payment service: nine functions, every
defect planted on purpose. The output below is real.

### 1. Tell it where money enters

testigo never guesses entry points. Which function begins a flow is something you
know and the code does not say — and a guessed list invites someone to accept it
without reading, which silently drops a whole path from the analysis.

```sh
testigo init .
testigo entry . add 'example.com/paysvc/api#(*Server).CreatePayment' "API create"
```

The fixture declares three: the API handler, the provider webhook and the
reconciliation job. Following only the API would miss the webhook, which is exactly
where double-credit bugs live.

### 2. Scan — the free part

```
$ testigo scan .
phase       1 · ScanningTheFlow
module      example.com/paysvc
entries     3
nodes       9 functions reachable from the entry points
seams       4 injectable, 11 on concrete types
states      example.com/paysvc/domain.PaymentStatus: 6 states, 4 write sites
states      example.com/paysvc/domain.SettlementMode: 3 states, 2 write sites

findings    5 critical, 9 high, 4 medium, 0 info

  [critical] IDEM-KEY-NOT-UNIQUE  Payment.OrderID is an idempotency key with no unique constraint behind it
             domain/payment.go:22
  [critical] MONEY-FLOAT          Payment.Amount stores money in a float
             domain/payment.go:24
  [critical] TX-NET-CALL          network call inside a database transaction
             api/server.go:40

still unknown (phase 2 asks an agent):
  - which state transitions are legal
  - how the uninjectable seams behave under failure
```

Writes `testigo/flow.json`. Your source files are never touched.

### 3. Ask — see the plan and the price before you spend

```
$ testigo ask --explain --round 1 .
ask plan   27 scenario(s) runnable, 1 blocked
budget     none; output counted 5x

ASKED
  questions                free text    5.9k in + 1.5k out = 13.6k
  mainEntity               free text    1.8k in +  512 out =  4.4k
  stateRoles x2            free text     471 in +  524 out =  3.1k
  moneyModel               free text    1.1k in +  308 out =  2.6k
  externalEffect x4        true/false   1.1k in +  100 out =  1.6k

cost       10.3k in + 3.0k out = 25.3k

est. cost   ~14.7k tokens across 9 prompt(s)
            ~34.8k saved by hoisting the shared rules into PREAMBLE.md
```

Writes markdown into `testigo/asks/`. Point your agent at
`testigo/asks/INSTRUCTIONS.md`; it writes JSON into `testigo/answers/`.
`--budget N` caps the weighted tokens the planner is allowed to commit.

### 4. Collect — validate, then apply

```sh
testigo collect .
```

Nothing here can tell whether an answer is *correct*. It can tell whether the answer
is about this repository at all: a state that does not exist, a declared state left
out, a claim with no `file:line`, or the prompt's own example sent back verbatim.

### 5. Cases — the test plan, priced, spending nothing

```sh
testigo cases .
```

Read the *not applicable* list too. A scenario that cannot run here, with the reason
it cannot, is information.

### 6. Report — one file, written by the agent

```sh
testigo report .         # a summary, printed, free
testigo report --ask .   # a prompt; the agent writes testigo/REPORT.md
```

Every claim must cite `file:line`, and every proposed fix must be safe for money:
nothing that could charge twice, drop a payment or lose the audit trail. A database
constraint beats a check in Go, and the cost of the fix has to be stated.

---

## What phase 1 proves on its own

| ID | Severity | What it is, and why it ranks there |
|---|---|---|
| `MONEY-FLOAT` | critical | An amount in a float. Binary floating point cannot hold `0.10` exactly, so sums drift and equal amounts compare unequal. Critical because it corrupts the amount itself, everywhere it flows. |
| `IDEM-KEY-NOT-UNIQUE` | critical | A key meant to stop repeats, with no unique index behind it. Two identical requests both pass "have I seen this?" before either writes. That is the classic double charge. |
| `TX-NET-CALL` | critical | A network call inside an open transaction. Locks are held for the provider's timeout, so a slow provider becomes a database outage — and a rollback cannot undo what the provider already did. |
| `MONEY-DIV` | high | Money divided with no rounding rule. Integer division drops the remainder, and a few lost minor units per operation become books that will not balance. |
| `TX-NO-ROLLBACK` | high | A transaction with no rollback on its early-return paths. Leaks a connection and holds locks — an outage shape rather than a wrong amount. |
| `SEAM-CONCRETE` | high | I/O on a concrete type, so no test can make it fail or time out. Most payment bugs only appear when something downstream fails, so those paths go untested. |
| `MONEY-NO-CURRENCY` | medium | A bare integer amount with no currency beside it. Nothing stops adding EUR to USD. Cheap with one currency; expensive the day there are two. |
| `STATE-NEVER-SET` | medium | A declared status nothing produces. Either it is dead, or something outside Go sets it — and either way nobody tests that transition. |
| `LOAD-ERROR` | info | A package did not type-check. Everything behind it is invisible: its findings are **missing, not absent**. |

A finding is evidence of a risk, not proof of a bug. `STATE-NEVER-SET` cannot know
whether another service writes that state. It reports the fact and leaves the
judgement to you.

---

## How testigo decides a boundary is real

A seam is a call where the flow leaves the process: a database, an HTTP or gRPC call,
a broker, a cache. It is **injectable** if it goes through an interface your
repository defines, because then a test can replace it and make it fail.

A call is first a *candidate* because of the package it lives in. It is kept only if
the call graph shows it can actually reach a socket or a database round trip. On the
way there, testigo refuses to follow:

- **plumbing interfaces** — `error`, `io.Reader`/`io.Writer`, `context.Context`, `fmt.Stringer` — which resolve to every implementation in the program, including socket reads;
- **generic callbacks** such as `func(rune) rune`, which link to every function of that shape;
- **library code calling back into your application**, since a library's own I/O cannot depend on the callbacks you register.

Sends that hand work to a background goroutine — `Publish`, `Send`, `Request` — are
kept by name, because their socket write is not on any static path at all.

On a real service this took seams from **165 to 69**, and effect-seam targets from 35
to 15. Every real seam survived — NATS publish, go-pg insert and select, the order
repository. The false ones did not: `grpc/status.New`, `metadata.MD.Get`, go-pg's
query builders, `(error).Error`.

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
| `questions` | the scenario and technique questions — true/false wherever a boolean will do |
| `stateRoles` | the role of each state of one machine; testigo derives the legal transitions itself |
| `externalEffect` | five verdicts about one seam: survives a rollback, observable after a timeout, deduplicated, moves money |

Round-one answers feed round two, **scoped per case**: a state test receives the
states it must refuse, a fault-injection test receives the verdicts for the seams it
fakes, an idempotency test receives the key facts. No case sees another scenario's
answers.

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
   what consumes it. A field with no reader should not exist — and 35 of them did.
3. **Hoist everything repeated into one preamble.** Shared rules and the rendered
   flow are written once into `PREAMBLE.md`; each ask cites step numbers instead of
   reprinting them. On the fixture that is ~34.8k saved against 14.7k actually sent.
   A test fails the build if any text is identical across the asks of one kind.
4. **Prefer a boolean to a paragraph.** A true/false question costs about 5 output
   tokens; an open one costs 40 to 130. Most things worth knowing are decidable.

The largest single win was deleting a feature. Per-function "notes" — what does this
function do in business terms — cost **381k of 618k** weighted tokens in round one,
and no scenario required them. They are gone.

See [studies/contextEngineering.md](studies/contextEngineering.md) for the full
argument and the measurements.

---

## Vocabulary

| Term | Meaning |
|---|---|
| **Seam** | a call that leaves the process; injectable if it goes through an interface your repo defines |
| **Scenario** | one of 28 catalogued ways a payment system breaks, in 7 families; declares what it `Requires` — see [CATALOG.md](CATALOG.md) |
| **Technique** | how a scenario is expressed: fault injection, concurrency, property, table, fuzz, metamorphic, state machine, narrow integration… |
| **Question** | one thing testigo cannot prove, with the problem it guards against and where the answer lives |
| **Payment kind** | spine (double-entry, wallet, stateless), motions (top-up, payout, escrow…) and overlays (refund, reconciliation, FX) |
| **Finding** | something phase 1 proved on its own, with a severity and a location |
| **Oracle** | whatever tells a test what "correct" means, and where that came from |

---

## Code map

| Package | Owns | Docs |
|---|---|---|
| `cmd/testigo` | the CLI: one function per command | [README](cmd/testigo/README.md) |
| `internal/scanningFlow` | phase 1 — SSA, the call graph, seams, state machines, money, findings | [README](internal/scanningFlow/README.md) |
| `internal/scanningFlow/patterns` | name matching, word-boundary aware, so `Current` is not a currency | [README](internal/scanningFlow/patterns/README.md) |
| `internal/scanningFlow/flowEntity` | the flow's types, and what gets persisted | [README](internal/scanningFlow/flowEntity/README.md) |
| `internal/testPlan` | the scenario catalogue, the plan types, and the selector | [README](internal/testPlan/README.md) |
| `internal/agent` | phase 2 — plans asks, writes the pack, collects and validates answers | [README](internal/agent/README.md) |
| `internal/agent/domain` | the agent's vocabulary: questions, asks, answers, payment kinds | [README](internal/agent/domain/README.md) |
| `internal/agent/planner` | demand, the consumer registry, the cost model, `--explain` | [README](internal/agent/planner/README.md) |
| `internal/agent/prompts` | one file per prompt, plus the preamble | [README](internal/agent/prompts/README.md) |
| `internal/config` | everything under `testigo/`: config, rules and `flow.json` | [README](internal/config/README.md) |
| `internal/report` | the printed summary and the Mermaid diagrams | [README](internal/report/README.md) |

11 packages, ~14.5k lines of Go.

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
own `protoc` step. If `go env GOFLAGS` forces `-mod=vendor` on a module whose vendor
directory is stale, run the scan with `GOFLAGS=-mod=mod`.

---

## Limits, stated plainly

- `TX-NO-ROLLBACK` does not yet recognise `tx.Close()`, which rolls back in go-pg, so it is a false positive there.
- The seam filter over-approximates. A VTA call graph would be more precise than CHA.
- Round two has not yet been run end to end on a production repository.
- True/false verdicts are collected per question, but nothing yet rolls them up into per-area conclusions.
- Recheck questions — where the agent re-confirms a fact phase 1 proved, with the proof attached — are built and tested, but nothing currently produces one.

---

## Studies

Every scenario testigo knows is listed in **[CATALOG.md](CATALOG.md)** — 28 of them,
with what each needs, what passing means, and the specific wrong versions of each test.

The research this is built on lives in [`studies/`](studies/).

- [How real fintech systems test payment flows](studies/howRealFintechSystemsTestPaymentFlows.md) — 19 practices from Stripe, Adyen, Airbnb, Uber, Monzo, Starling, Nubank, Jepsen, TigerBeetle and others, each turned into a catalogue scenario.
- [Context engineering](studies/contextEngineering.md) — how to get a better answer from a model for fewer tokens, measured on this project.
