# agent — phase 2

Phase 1 proved everything a compiler can prove. This package asks an agent about the
rest, turns the answers back into checked data, and generates tests from both.

## The rule every prompt follows

**Ask a bounded question with a verifiable answer.**

"Read this code and tell me what it does" is unbounded: a wrong answer looks exactly
like a right one. "Here are six declared states and the lines that write them — what
part does each play?" is bounded. The answer space is finite, completeness is
mechanical, and every claim names a file and line that either exists or does not.

## Only scenario requirements drive what is asked

A payment kind selects scenarios. Each scenario runs as a technique. Scenarios and
techniques declare the questions they cannot be written without. Nothing else is
asked.

[`planner/`](planner/README.md) prices every candidate — output counts five times
input — and drops any ask that no runnable scenario needs, or whose fields nothing
downstream reads.

## Two rounds

| Round | Asks for | Writes code |
|---|---|---|
| 1 `understand` | `moneyModel`, `mainEntity`, `questions`, `stateRoles`, `externalEffect` | no |
| 2 `generate` | one test per runnable scenario | yes |

Round 2 is gated on round 1. If the agent names the wrong money-moving call, a test
written in the same breath would encode that mistake and pass forever.

Round-one answers flow into round two **scoped per case**, so no test prompt sees
another scenario's answers.

## Transport: files, not an API

`testigo ask` writes `testigo/asks/`: one markdown file per kind, `PREAMBLE.md`,
`INSTRUCTIONS.md`, and a manifest. Your agent writes one JSON file per ask into
`testigo/answers/`. `testigo collect` validates and applies.

No API key, no network, no vendor. The prompts are markdown a person can read before
spending anything, and the answers are JSON a person can correct by hand. When an
answer is wrong, you fix the file — you do not re-run and hope.

The manifest records which question IDs each ask actually asked, so an answer stays
valid even when later answers change which scenarios are runnable.

## What gets validated

Nothing here can tell whether an answer is *correct*. It can tell whether it is about
this repository at all:

- every state named must exist, and every declared state needs a role;
- a claim without a `file:line` is rejected;
- the placeholder from the prompt's own example coming back is rejected;
- an answer to a question that was not asked is rejected; an unanswered one is reported by ID;
- a true/false question with no verdict is rejected, never read as `false`;
- a generated test that sleeps, opens a socket, or cannot prove it reached its situation is rejected;
- a test marked blocked with no reason is rejected.

## Safe defaults

External-effect verdicts accept `true`, `false` or `"unknown"`. Only a literal `true`
counts as reversible, observable or deduplicated; anything else is read the unsafe way.

Assuming a charge is safe to repeat when it is not is how a customer gets billed
twice. The reverse costs one extra test.

## Files

| file | holds |
|---|---|
| `plan.go` | which asks are worth making in each round |
| `batch.go` | grouping the asks of one kind into one file |
| `pack.go` | writing and reading `testigo/asks/` |
| `collect.go` | reading, validating and applying answers |
| `verify.go` | writing a generated test and asking the toolchain whether it is real |
| [`domain/`](domain/README.md) | questions, asks, answers, payment kinds |
| [`planner/`](planner/README.md) | demand, the consumer registry, the cost model |
| [`prompts/`](prompts/README.md) | one file per prompt, plus the preamble |
