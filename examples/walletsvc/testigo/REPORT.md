# example.com/walletsvc — payment flow report

## What this is

A single-balance wallet service with three entry points: transfer between two wallets (api/server.go:12), deposit (api/server.go:16) and withdraw (api/server.go:20). Money is the `Balance` of a `domain.Wallet`, an integer in minor units with no currency (domain/wallet.go:7, domain/wallet.go:26). There is no payment provider in this module: a transfer calls a `Notifier` to settle with a core system between its two legs (service/transfer.go:37), and that call's implementation is not in this repository. testigo classified it as a wallet spine with a payout motion. For a wallet, that means the balance column is the money: every write to it must be atomic, guarded and recorded, because there is no provider-side record to reconcile against.

## What was tested

| Scenario | Test kind | Why it matters here |
|---|---|---|
| IDEM-REPLAY | fault injection | Deposit takes a caller tracking number (service/transfer.go:44) that a retry repeats |
| IDEM-CONCURRENT | concurrency | Deposit reads the balance, then writes it back (service/transfer.go:45, service/transfer.go:58) |
| IDEM-CRASH-AT-STEP | fault injection | Transfer is three separate calls (service/transfer.go:33, :37, :41); deposit is two (:58, :61) |
| TRANSFER-IS-ATOMIC | fault injection | Settle runs between the debit and the credit (service/transfer.go:37) |
| SELF-TRANSFER-REFUSED | table | CoreTransfer never compares the two wallet ids (service/transfer.go:20, :24) |
| DOUBLE-ENTRY-SUMS-TO-ZERO | property | The two legs of a transfer are separate writes (service/transfer.go:33, :41) |
| IDEM-PAYLOAD-MISMATCH | fault injection | The tracking number is stored but never compared (service/transfer.go:62) |
| WALLET-STATUS-GATES-MOVEMENT | table | Only withdraw checks the status (service/transfer.go:71) |
| FINAL-STATE-IS-FINAL | state machine | EnableWallet writes `enable` without reading the current status (service/transfer.go:84) |
| ILLEGAL-TRANSITION-REFUSED | state machine | Same driver, plus the legal moves, to show a guard would be selective |
| MONEY-ROUND-TRIP-EXACT | fuzz | The transfer amount passes through `float64` (service/transfer.go:18) |
| CURRENCY-MIXING-REFUSED | table | No money type has a currency (domain/wallet.go:21-62) |

Not run:

- CONSERVATION-UNDER-CONCURRENCY, LOST-UPDATE, LEDGER-OUTSIDE-CALLER-TRANSACTION, BALANCE-NEVER-NEGATIVE, READ-MODIFY-WRITE-NEEDS-A-LOCK, DEADLOCK-IS-RECOVERED, BALANCE-LIMIT-ENFORCED, APPEND-ONLY-HISTORY-IS-IMMUTABLE — medium tests need a real Postgres, and the module has no database driver in go.mod; round 2 may not add one.
- UNCLASSIFIED-ERROR-NOT-RETRIED — the code classifies no error and retries nothing (service/transfer.go:37-39), so there is no retryable class to test beside the unknown one.
- ORPHANED-AUTHORIZATION, FAILURE-IS-NOT-TERMINAL, WEBHOOK-ORDER-INDEPENDENT, RECONCILER-IS-IDEMPOTENT — do not apply: no authorizations, no failed state, no callbacks, no reconciliation job.
- MINOR-UNIT-CONVERSION, SPLIT-SUMS-TO-TOTAL — nothing to call: no currency conversion, and no function splits an amount.

## Results

All 12 written tests are red, and all 12 were declared expected to fail before they ran. Each red test is a finding, not a broken test.

| Test | Result | What it proves |
|---|---|---|
| TestTransferIsAtomic | red, expected | When Settle or the credit fails, the debit stays: 1000 across two wallets becomes 700 |
| TestSelfTransferRefused | red, expected | A transfer from a wallet to itself is accepted and adds the amount: 500 becomes 600 |
| TestDoubleEntrySumsToZero | red, expected | The first generated self-transfer moved the total of all wallets from 3,000,000 to 3,004,588 |
| TestIdemCrashAtStep | red, expected | 8 of 14 failure points end differently from a clean run: a transfer retried after a failure past the debit debits twice (900 becomes 800); a deposit retried after its history write fails credits twice |
| TestIdemReplay | red, expected | The same deposit delivered twice credits twice (200, want 100); a deposit that reported failure twice still credited 200 |
| TestIdemConcurrent | red, expected | 32 simultaneous deliveries of one deposit: all 32 succeed, 32 history rows say 3200 was credited, and the balance says 400 |
| TestIdemPayloadMismatch | red, expected | The same tracking number with amount 200 instead of 100 is accepted: the balance ends at 300 |
| TestWalletStatusGatesMovement | red, expected | Initial, pending, disabled and deleted wallets accept deposits and transfers in and out; withdraw refuses them but says only "wallet not active" |
| TestFinalStateIsFinal | red, expected | A deleted wallet is enabled again |
| TestIllegalTransitionRefused | red, expected | deleted -> enable is accepted; the 7 legal moves in the same table pass |
| FuzzMoneyRoundTripExact | red, expected | A transfer of 9,007,199,254,740,993 arrives as 9,007,199,254,740,992: one minor unit lost through `float64` |
| TestCurrencyMixingRefused | red, expected | Wallet, Transaction and TransferInput carry money with no currency |

## Strengths

- Money is an integer type in minor units, not a float, in every struct (domain/wallet.go:5, domain/wallet.go:7).
- The service reaches the database and the core system only through interfaces it defines (repo/wallet.go:10, repo/wallet.go:19). That is why 12 tests could run with no database at all.
- Withdraw refuses a wallet that is not active (service/transfer.go:71).
- Transfer checks for sufficient funds before the debit (service/transfer.go:29), even though the check is not atomic (see Problems).
- The maximum balance is configuration, read per wallet type from `settings` (repo/wallet.go:52), not a constant in code.
- The `transactions` table is only ever inserted into (repo/wallet.go:44-49). Nothing updates or deletes history.

## Weaknesses

Ranked by what each costs in production:

1. A transfer is not atomic: money disappears whenever anything fails after the debit (service/transfer.go:33-41).
2. Deposits have no working idempotency, so every retry or redelivery credits again (service/transfer.go:61-63).
3. Every balance write is "read, compute in Go, write the absolute value" with no lock (service/transfer.go:45, repo/wallet.go:35), so concurrent writes erase each other.
4. `Atomic` is not atomic: it opens a transaction and runs the work outside it (repo/wallet.go:59-66).
5. The status gate exists only on withdraw (service/transfer.go:71), and status changes have no guard (service/transfer.go:84, service/transfer.go:93).
6. Withdraw has no funds check (service/transfer.go:75).
7. The amount passes through `float64` and loses its sign (service/transfer.go:18).

## Problems

### A transfer destroys money when anything fails after the debit

**Where** service/transfer.go:33, service/transfer.go:37, service/transfer.go:41

**What happens** The debit is written and committed on its own (repo/wallet.go:35). If Settle fails, or the credit fails, CoreTransfer returns an error with the debit in place and no credit: in TestTransferIsAtomic, 1000 across two wallets became 700. A client that retries after that error debits again (TestIdemCrashAtStep: 900 became 800).

**Fix** Write the debit and the credit in one database transaction, each as a conditional relative UPDATE (`UPDATE wallets SET balance = balance - $1 WHERE id = $2 AND balance >= $1`). Do not call Settle inside that transaction: write an outbox row in the same transaction and send Settle after commit, retrying it with the transfer id as its key. Cost: the transaction holds two row locks; take them in wallet-id order so two opposite transfers cannot deadlock. This needs `Atomic` fixed first (below).

### A transfer from a wallet to itself creates money

**Where** service/transfer.go:20, service/transfer.go:24, service/transfer.go:41

**What happens** CoreTransfer reads the same wallet twice, debits it, then writes the credit from the balance it read before the debit. A transfer of 100 from a wallet holding 500 to itself leaves 600 (TestSelfTransferRefused). Repeating it grows the balance without limit.

**Fix** Refuse `FromWalletID == ToWalletID` before any read. If transfers get their own table, add `CHECK (from_wallet_id <> to_wallet_id)` so no other path can do it. Cost: none.

### Deposits are not idempotent, and a failed deposit keeps its credit

**Where** service/transfer.go:58, service/transfer.go:61-63, migrations/0001_wallets.sql:15

**What happens** The tracking number is written into history but never read back, and no constraint makes it unique. Two deliveries of the same deposit credit twice (TestIdemReplay), 32 simultaneous deliveries all succeed (TestIdemConcurrent), and the same key with a different amount is accepted as a new deposit (TestIdemPayloadMismatch). The balance is written before the history row, with nothing joining the two, so a deposit whose history write fails reports failure but keeps the credit, and the client's retry credits again.

Read from the code, not from a test (no test ran against Postgres): Deposit never sets `Transaction.ID` or `TransferID` (service/transfer.go:61-63), and both columns are `uuid NOT NULL` with no default (migrations/0001_wallets.sql:12, migrations/0001_wallets.sql:14). On Postgres, an empty string is not a valid uuid, so the history insert would fail on every deposit, after the balance was already credited.

**Fix** Add `UNIQUE (wallet_id, tracking_number)` to `transactions`. In one transaction, insert the history row first, then apply the balance change as a relative UPDATE. On a unique violation, compare the stored amount: the same amount returns the stored result, a different amount returns a conflict. Generate the id in Go or give the column `DEFAULT gen_random_uuid()`, and make `transfer_id` nullable for deposits. Cost: one index; existing duplicate rows must be resolved before the constraint can be added.

### Concurrent balance writes erase each other

**Where** service/transfer.go:45, service/transfer.go:58, repo/wallet.go:35

**What happens** Every balance change reads the wallet, computes the new balance in Go, and writes the absolute value back. Two deposits that read the same balance both write "old + amount", and one is lost. In TestIdemConcurrent, 32 credits of 100 left a balance of 400 while history recorded 3200.

**Fix** Replace `UpdateBalance(id, balance)` with a relative, guarded statement: `UPDATE wallets SET balance = balance + $1 WHERE id = $2 AND status = 'enable' AND balance + $1 BETWEEN 0 AND $3 RETURNING balance`, where `$3` is the limit. One statement is atomic in Postgres, needs no explicit lock, and carries the funds, limit and status checks with it. Cost: the repository interface changes, and every caller moves from "set" to "add".

### `Atomic` runs the work outside its own transaction

**Where** repo/wallet.go:59, repo/wallet.go:63, repo/wallet.go:64

**What happens** `Atomic` opens a transaction, then calls `fn(ctx)`, and every repository method inside `fn` uses the plain `*sql.DB` (repo/wallet.go:35). The transaction holds no statements (TX-UNUSED), and when `fn` fails it returns without a rollback (TX-NO-ROLLBACK), leaving the transaction and its connection open. Withdraw relies on it (service/transfer.go:74).

**Fix** Carry the `*sql.Tx` in the context, or pass a querier to `fn`, so every method runs on it; `defer tx.Rollback()` right after `BeginTx`. Cost: the repository methods need a querier parameter or a context lookup.

### Withdraw can make a balance negative

**Where** service/transfer.go:75

**What happens** Withdraw writes `balance - amount` with no funds check. A withdrawal of 600 from a wallet holding 500 leaves -100. Round 1 established that a negative balance is never legitimate here.

**Fix** Use the guarded relative UPDATE above (`AND balance >= $1`) and add `CHECK (balance >= 0)` on `wallets`, so no other path can write a negative balance either. Cost: the CHECK cannot be added while any row is negative.

### Inactive and deleted wallets still move money

**Where** service/transfer.go:44, service/transfer.go:17, service/transfer.go:71

**What happens** Deposit and CoreTransfer never check the status. A deleted wallet accepts a deposit, and money can be transferred into and out of initial, pending, disabled and deleted wallets (TestWalletStatusGatesMovement). Money paid into a deleted wallet is stranded. Withdraw refuses them, but only says "wallet not active".

**Fix** Put `AND status = 'enable'` in the balance UPDATE itself, so the check and the write cannot be separated, and return an error that names the status. Cost: none.

### A deleted wallet can be enabled again

**Where** service/transfer.go:84, service/transfer.go:93

**What happens** EnableWallet and DeleteWallet write the new status without reading the current one, and the column has no CHECK (migrations/0001_wallets.sql:7). TestFinalStateIsFinal deletes a wallet and enables it again.

**Fix** `UPDATE wallets SET status = 'enable' WHERE id = $1 AND status IN ('initial', 'pending', 'disable')`, and treat zero rows changed as a refusal. Cost: none.

### The transfer amount passes through `float64`

**Where** service/transfer.go:18

**What happens** `math.Abs(float64(in.Amount))` loses precision above 2^53, so 9,007,199,254,740,993 arrives as 9,007,199,254,740,992 (FuzzMoneyRoundTripExact). It also turns a negative amount into a positive one, so a caller's sign error moves money instead of being refused.

**Fix** Refuse `in.Amount <= 0` with an error and remove the conversion. Cost: none.

### A transfer can push the receiving wallet past its limit

**Where** service/transfer.go:41, service/transfer.go:54

**What happens** Deposit checks `settings.max_balance`, but the credit leg of a transfer does not, so a wallet can be filled past its limit through transfers.

**Fix** The guarded relative UPDATE above, with the limit, on both deposit and the credit leg. Cost: one more read of `settings`, or the limit cached per wallet type.

### Smaller issues

- No money type has a currency (domain/wallet.go:21-62). Harmless with one currency; add a currency column before a second one exists.
- `Fee` truncates (service/transfer.go:97) and nothing calls it; `TransferInput.Fee` is accepted and never applied (domain/wallet.go:61). Decide whether transfers charge a fee, and how it rounds.

## What this cost

testigo's own estimates of prompt size, not a bill: round 1 was ~14.6k tokens across 13 questions (~58.8k avoided by putting shared rules in one preamble), round 2 was ~28.1k across 27 test cases, and this report prompt was ~5.8k. Still unanswered: the 8 medium scenarios, which need a Postgres driver in go.mod; whether anything outside this module writes `wallets.balance` (round 1 could not tell); and what the core system does with a repeated Settle.
