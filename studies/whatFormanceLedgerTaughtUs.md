# What Formance Ledger Taught Us

[formancehq/ledger](https://github.com/formancehq/ledger) is a real, open-source,
production double-entry ledger — Postgres-backed, Go, with a built-in DSL
(Numscript) for scripting money movement. We read it end to end: the domain
model, the storage layer, the concurrency and idempotency machinery, and how the
team tests all of it. This is what came out of that reading, split into what we
added to the catalogue and what we're naming as a real gap instead of guessing
at a fix.

---

## What we added

### `DEADLOCK-IS-RECOVERED`

Ledger transfers lock two account rows per operation. Before locking, the code
sorts the account identifiers into a fixed order first
(`internal/storage/ledger/balances.go`, "prevent deadlocks by sorting the
accountsVolumes slice"). Even so, the team has a real test
(`internal/storage/ledger/transactions_test.go`) that deliberately drives two
goroutines into a deadlock — one transfer A→B, one B→A, opposite lock order —
and asserts Postgres's own deadlock detector fires and the code recovers
cleanly rather than corrupting a balance or leaking a raw driver error.

That's a scenario testigo didn't have. `LOST-UPDATE` and `TRANSFER-IS-ATOMIC`
both assume a single account (or a single leg) under contention; neither one
asks what happens when a transfer needs *two* rows and two concurrent
transfers want them in opposite order. Every wallet/payment-shaped codebase
that opens its own transaction across more than one account row has this
exposure, whether or not it locks in a fixed order — the scenario is honest
about accepting either real fix (sorted lock order, or catch-and-retry on the
database's deadlock error) rather than prescribing one.

See `DEADLOCK-IS-RECOVERED` in [CATALOG.md](../CATALOG.md).

### `TENANT-ROWS-DONT-LEAK`

A "bucket" in ledger is one Postgres schema, and one bucket can hold several
ledgers at once — every table carries a plain `ledger` column, and every
uniqueness rule is scoped by it: `create unique index ... on logs (ledger,
idempotency_key)`, `create unique index transactions_ledger on transactions
(ledger, id)`, and the same shape on `accounts`, `accounts_metadata`, and
`transactions_metadata`. Two different ledgers can each have their own row
keyed the same business-key value, because the schema was built to allow it.

That structural shape — a column that leads a *composite* uniqueness
constraint on two or more different tables — is now something the scanner
looks for directly (`flowEntity.Infra.TenantScheme()`), reading it straight
from the same schema facts the write-pattern classifier already collects. It
deliberately does not guess from the column's name: an ordinary owner
reference (a `user_id` on that user's own rows) is not the same shape and
should not be offered this scenario, which is exactly what kept it from
firing on either wallet or payment — neither one is actually multi-tenant.

See `TENANT-ROWS-DONT-LEAK` in [CATALOG.md](../CATALOG.md).

### `HASH-CHAIN-CATCHES-TAMPERING`

Every log row in ledger hashes the previous row's hash plus its own content
(`internal/log.go`, `ChainLog`/`ComputeHash`) — an audit trail that's supposed
to make "nobody edited history" something a script can check, not just a
policy someone follows.

Detecting "this is a hash chain" turned out not to need guessing at field or
method names at all. The one part of the shape that can't be a coincidence is
structural: a method that takes *the previous instance of its own type* and,
somewhere in its body, calls a real cryptographic hash function
(`sha256`/`sha512`/`sha1`/`md5`/`blake2`, either the one-shot `Sum*` form or
the streaming `New()` form). `ledger.Log.ChainLog(previous *Log) Log` is
exactly that shape; an ordinary method that happens to take another instance
of its own type for some unrelated reason (a `Merge`, a diff, a comparison)
does not also hash anything, and a method that hashes something unrelated (a
password, say) does not also take a previous instance of its own type. Both
have to be true together, which is what `extractHashChains` checks
(`internal/scanningFlow/hashChain.go`).

What the implemented scenario checks is narrower than the original idea: it
proves tampering with any one record breaks verification of everything after
it, using the metamorphic relation ledger's own report already implied
(verify before / verify after one field changes). It does **not** check the
other half of the original gap — that two concurrent inserts can't both claim
the same "previous hash" (the reason ledger's synchronous hashing mode takes
a per-ledger advisory lock before computing one). That's a genuine
concurrency property, not a tamper-evidence one, and would need its own
scenario rather than being folded into this one.

See `HASH-CHAIN-CATCHES-TAMPERING` in [CATALOG.md](../CATALOG.md).

---

## What we're naming as a gap, not implementing yet

This is a real, concrete idea the ledger's code justified — it would need new
fact-detection in the scanner before it could be a trustworthy, code-derived
`Requires` gate rather than a guess. Naming it here, honestly, beats forcing a
low-precision heuristic into the scanner just to check a box.

**An insert that reads the previous row first can still lose an update.**
Ledger's `moves` table is append-only, but each new row's balance snapshot is
computed by reading the immediately-preceding row for that account and asset
and adding to it (`insert_move` in the migrations). That's a lost-update
window on an *insert-only* table — testigo's current write-pattern classifier
(`internal/scanningFlow/writePattern.go`) only distinguishes insert-only from
update-in-place by counting `INSERT` vs `UPDATE` calls, so it would call this
table safely insert-only and miss the race entirely. Detecting it needs
recognizing a "select latest, then insert the next" shape inside one
function — doable, but a real, separate piece of AST work from what exists
today.

---

## What we confirmed we already had right

- **Idempotency needs a payload hash, not just a key.** Ledger stores
  `IdempotencyHash` — a hash of the original request — right next to
  `IdempotencyKey`, and rejects a replay whose hash doesn't match. That's
  exactly the shape `IDEM-PAYLOAD-MISMATCH` already tests for; seeing a real,
  mature ledger implement it error-for-error the way Stripe's own published
  contract describes it is a strong confirmation, not a new idea.
- **A race on the idempotency check still needs a database backstop.** Ledger
  reads for an existing key first, but the real guarantee is a `UNIQUE`
  index on `(ledger, idempotency_key)` — a losing concurrent insert gets a
  constraint violation, caught and turned into a re-read rather than a raw
  error. This is the same shape `IDEM-CONCURRENT` already assumes.
- **Reversal-as-a-new-transaction, never mutate history.** Ledger's
  `RevertTransaction` inserts a new, opposite-direction transaction rather
  than editing the original — the same principle behind
  `APPEND-ONLY-HISTORY-IS-IMMUTABLE`.

---

## A finding worth sitting with, not a testigo change

Double-entry bookkeeping has one obvious, checkable, global invariant: the
sum of every account's balance in a given asset is always zero. Ledger's own
test suite checks this — but only at fixed points after a scripted stress
run (`HaveCoherentState`, in the Ginkgo test matchers), never generatively,
across randomized posting sequences. Go's standard library ships
`testing/quick` for exactly this kind of property test, and it isn't used
here. `TechniqueProperty` already exists in testigo's catalogue
(`DOUBLE-ENTRY-SUMS-TO-ZERO` already asks for it) — this isn't a gap in
testigo. It's a reminder that even a well-funded, mature ledger team leaves an
obvious property test on the table, which is exactly the kind of thing a tool
like this exists to keep asking for, rather than assuming someone already
wrote it.
