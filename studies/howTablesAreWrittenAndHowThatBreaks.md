# How Tables Are Written, And How That Breaks

This is the research behind testigo's write-pattern classifier.

The idea is simple:

> How a table is written decides how it can break. Two systems can hold the same
> money and need completely different tests, because one adds rows and the other
> changes them.

testigo reads this from the repository layer with no agent and no tokens. It
looks at which ORM calls and which SQL statements touch each table, then picks
the scenarios that match.

---

## The five patterns

| Pattern | What the code does | The one-line risk |
|---|---|---|
| `insert_only` | only adds rows | history can be broken by one stray update |
| `update_in_place` | reads a row, changes it in Go, writes it back | two writers, one change lost |
| `update_in_database` | lets the database compute the new value (`balance = balance - ?`) | safe on one row, still needs a limit check |
| `upsert` | insert that becomes an update on conflict | the row is safe, the side effect is not |
| `unknown` | no writes found | testigo says so instead of guessing |

---

## 1. `update_in_place` — the lost update

This is the oldest bug in database work and it is still the most common one in
wallets.

Two requests read a balance of 100. Both subtract 30. Both write 70. One of the
two withdrawals is gone. Nobody gets an error. The customer keeps the money.

The window between the read and the write is the whole problem. It does not
matter how careful the Go code is, because the database has no idea the two
requests are related.

PostgreSQL's own documentation is direct about this: `READ COMMITTED`, which is
the default, does not stop it. You need one of:

* `SELECT ... FOR UPDATE` — take the row lock before you read the value
* `SERIALIZABLE` — and be ready to retry when the database refuses
* do the math in SQL so there is no window at all
* a version column, and refuse the write if the version moved

Source: [PostgreSQL — Transaction Isolation](https://www.postgresql.org/docs/current/transaction-iso.html)

### What to test

```text
start balance 100
two concurrent debits of 30
  -> final balance is 40, or exactly one debit was refused
  -> never 70
```

The test must run the two writers at the same time and must not use a sleep to
order them. A sleep tests the sleep.

**The trap:** a test that asserts "no error was returned" passes on the broken
code. Losing an update is silent. Assert the number.

### Related: write skew

Two requests each read a set of rows, each check a rule over that set, and each
write a different row. Both checks pass. The rule is broken afterwards. Row
locks do not help, because the two requests never touch the same row.

This is the one that breaks "a user may have at most one active default wallet"
and "total of these accounts must stay above zero". The fix is a constraint the
database enforces, or `SERIALIZABLE`.

Source: Martin Kleppmann, *Designing Data-Intensive Applications*, ch. 7.

---

## 2. `insert_only` — history that quietly stops being history

An append-only table is worth having because yesterday's answer is still there
today. Ledgers, audit logs, event tables and outboxes are all this shape.

They break in three ways.

**One path updates a row.** All it takes is a single `UPDATE` for a correction,
added by someone who did not know the rule. Every read still returns something
sensible, so nothing fails. The table is no longer a record of what happened.
The correct move is a second row that reverses the first.

**The reader forgets there are several rows.** With append-only data, "the
balance" is a sum, not a column. Code that reads the newest row instead of
summing all of them gives the wrong answer as soon as two rows arrive out of
order.

**Nothing enforces the append.** Databases can enforce this. A `BEFORE UPDATE`
trigger that raises, or simply not granting `UPDATE` on the table, turns the
rule into something the database refuses rather than something a reviewer has
to remember.

### What to test

```text
write a row
correct it the way a caller would
  -> the original row is still readable, unchanged
  -> the correction is a new row
  -> the row count only grew
```

**The trap:** asserting only the final value. That passes whether or not
history was kept, which is the one thing this table exists for.

Related practice: double-entry ledgers, where every movement is two rows that
sum to zero and nothing is ever edited. See the idempotency and ledger sections
of [How Real Fintech Systems Test Payment Flows](howRealFintechSystemsTestPaymentFlows.md).

---

## 3. `update_in_database` — safer, and still not safe

`UPDATE wallets SET balance = balance - 30 WHERE id = ?` is much better than
reading and writing back. The database does the math while holding the row, so
two of these cannot lose each other.

It still has two holes.

**It will go negative.** Subtracting more than the balance is a perfectly valid
statement. Nothing stops it unless you add `WHERE balance >= 30` and then check
how many rows were actually changed, or a `CHECK (balance >= 0)` constraint.

Checking the balance in Go first does not count. That puts the read-then-write
window straight back in.

**An upper limit has the same problem.** A maximum balance checked with a
`SELECT` and enforced by a later `UPDATE` can be crossed by two deposits that
each fit on their own.

### What to test

```text
balance 50, debit 80
  -> refused, balance still 50

balance 50, two concurrent debits of 30
  -> exactly one succeeds, balance 20, never -10
```

And read the number of affected rows. A statement that changed zero rows is not
a success, but `err == nil`, so code that ignores the count treats a refused
debit as a completed one.

---

## 4. `upsert` — the row is safe, the work is not

`INSERT ... ON CONFLICT DO UPDATE` makes the write safe to repeat. Teams then
assume the whole request is safe to repeat. It is not.

The second request still runs the function. It still charges the card, still
sends the message, still calls the provider. Then it reaches the upsert, which
absorbs the row write and returns success. Afterwards the row looks perfect, so
any test that checks the row passes, while the customer was charged twice.

There is a second, quieter problem. `ON CONFLICT DO UPDATE` overwrites with the
*second* request's values. If the two requests differ at all, the row ends up
holding a mix of the first attempt and the second.

The published contract for this is Stripe's: store the result of the first
request against the key and return it, and treat the same key with different
parameters as a conflict rather than a replay.

Sources: [Stripe — Idempotent requests](https://docs.stripe.com/api/idempotent_requests),
[brandur.org — Idempotency keys](https://brandur.org/idempotency-keys)

### What to test

```text
send the request twice
  -> the provider was called exactly ONCE
  -> the row holds the first result, not a blend
  -> the second caller is told it is a repeat
```

**The trap:** asserting the row is correct. That is exactly what the upsert
guarantees on its own, so the assertion cannot fail and proves nothing. Count
the side effect, not the rows.

---

## 5. Outbox: insert-only and update-in-place in one breath

The outbox pattern writes the business row and a message row in the same
transaction, then a separate worker reads the outbox and publishes.

This is worth calling out because it mixes two patterns, and each half fails
differently.

* the insert half must be in the **same transaction** as the business row, or a
  crash between them loses the message
* the publish half is at-least-once, so the same message will be delivered
  twice and every consumer has to cope
* the worker marks rows as sent, which is `update_in_place`, so two workers can
  both pick up the same row and publish it twice

Source: [microservices.io — Transactional outbox](https://microservices.io/patterns/data/transactional-outbox.html)

Both repositories testigo was measured against use an outbox table.

---

## What testigo does with this

The classifier records, per table: how many inserts, updates and deletes; whether
the math happens in the database; whether a row is read and written back; whether
a conflict clause exists; and whether any row lock is taken.

Scenarios then declare which patterns they need, so the same repository gets
different tests depending on how it actually writes:

| Scenario | Only offered when |
|---|---|
| `LOST-UPDATE` | `update_in_place` |
| `READ-MODIFY-WRITE-NEEDS-A-LOCK` | `update_in_place` |
| `APPEND-ONLY-HISTORY-IS-IMMUTABLE` | `insert_only` |
| `UPSERT-HIDES-A-SECOND-EFFECT` | `upsert` |
| `BALANCE-NEVER-NEGATIVE` | a balance function, any mutating pattern |

A repository that only ever inserts is never offered a lost-update test, and is
told why. That decision costs nothing, because it is made from the code rather
than by asking a model.

---

## Measured on two real services

| | wallet | payment |
|---|---|---|
| `insert_only` tables | 5 | 5 |
| `update_in_place` tables | 4 | 1 |
| `upsert` tables | 1 | 3 |
| row lock seen anywhere | on 3 tables | **none** |
| `ON CONFLICT` in the repository layer | none | none |

The balance table in the wallet service is written **both** ways — some paths do
the math in SQL, others read the row and write it back — and that is the shape
this whole study says to test first.
