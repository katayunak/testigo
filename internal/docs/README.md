# docs — the repository's own documentation

`Find` walks the repository for markdown (`.md`, `.adoc`) and keeps the files that
talk about how money moves. `testigo scan` stores them in `flow.json` under `docs`,
and the round-one `moneyModel` prompt lists them under "Read these first": a rule
the team wrote down beats one inferred from the code.

## How a file is scored

Each file is counted against seven topics. The words are matched anywhere in the
text, ignoring case, so `idempot` also counts `idempotency` and `idempotent`.

| Topic | Words it looks for |
|---|---|
| money movement | settle, settlement, payout, disburse, transfer, capture, authoriz, authoris |
| reversal | refund, reverse, reversal, chargeback, dispute, recall, cancel |
| lifecycle | status, state machine, lifecycle, transition, pending, final |
| idempotency | idempot, duplicate, dedup, retry, replay |
| ledger | ledger, double entry, double-entry, journal, balance, debit, credit |
| messaging | kafka, topic, partition, consumer, webhook, callback, queue |
| reconciliation | reconcil, coherence, catch-up, catchup, mismatch |

Every hit adds one point. A topic with five or more hits adds five more, so a file
that is *about* something beats one that mentions it in passing. A file with no
hits is dropped. The eight highest scores are kept.

## What it skips

- `.git`, `vendor`, `node_modules`, `testdata`, and testigo's own `testigo/`, `.testigo/` and `_to_delete/`;
- files over 512 KB;
- files under an `internal/` path that mention testigo, so testigo's own package docs are not offered as the scanned repository's rules.

## Why it is here and not in gofacts

It decides what to show the agent. That is testigo's product, not a fact about the
code, so it stayed behind when the scanner moved to gofacts. `fact.Flow` keeps a
`Docs` field only so a flow written by testigo keeps its shape.
