# cmd/testigo — the CLI

One file, one function per command, no framework. Every command takes an optional
directory (defaulting to `.`) and flags go **before** it:

```sh
testigo ask --explain --round 1 ./myrepo
```

## The commands

| Command | Phase | Costs | Does |
|---|---|---|---|
| `init` | 1 | free | writes a starter `testigo/config.json` |
| `entry` | 1 | free | appends an entry point: `entry . add <pkg>#<Symbol> [label]` |
| `scan` | 1 | free | runs `ScanningTheFlow`, writes `testigo/flow.json` |
| `flow` | 1 | free | prints the flow as a Mermaid diagram |
| `rules` | 1 | free | writes a starter `testigo/rules.json` |
| `ask` | 2 | free to *plan* | writes the prompt pack into `testigo/asks/` |
| `collect` | 2 | free | reads `testigo/answers/`, validates, applies |
| `cases` | 2 | free | prints the test plan and what generating it would cost |
| `report` | 2 | free | prints everything found; `--ask` writes a prompt for `REPORT.md` |

Only the agent costs anything, and testigo never calls it. `ask` writes markdown;
you run your own agent against it.

## The flags that matter

- `--round 1|2` — round 1 understands the repository, round 2 writes tests. Round 2 is gated on round 1.
- `--explain` — prints the planner's decision: what was asked, what was skipped, and why.
- `--budget N` — caps the weighted tokens the planner may commit. Over budget, the lowest value-per-token asks are dropped first.

## Why `entry` is a command and not a guess

Which function begins a payment flow is something you know and the code does not say.
A handler called `ProcessRequest` may be the whole flow; one called `CreatePayment`
may be a wrapper nobody calls any more.

A guessed list invites someone to accept it without reading, and an entry point
accepted without reading is a whole path through the system that silently never gets
analysed. So an empty entry list is an **error**, not an empty flow.

Most payment systems have several, and they rejoin the same state machine: the API
handler, the provider webhook, the reconciliation job, the settlement consumer.

## Exit behaviour

Errors go to stderr prefixed `testigo:` and exit non-zero. Everything testigo writes
goes under `testigo/`. Your source files are never modified — except by
`collect` after round two, which writes generated `*_testigo_test.go` files and tells
you exactly which.
