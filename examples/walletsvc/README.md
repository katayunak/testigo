# A full testigo run, on a small wallet service

This folder is one complete run of testigo, kept so you can read every step
without running anything. The code is a copy of
[`testdata/walletsvc`](../../testdata/walletsvc): a wallet service with
transfer, deposit and withdraw, about 250 lines of Go, with its bugs planted
on purpose.

The agent in this run was Claude, reading testigo's prompts and writing its
answers back as files. Nothing here was edited by hand afterwards.

## What came out

- **12 tests written. All 12 fail, and all 12 said in advance that they
  would.** Each red test is a bug, named in its failure message.
- **15 cases blocked**, each with a reason. 8 need a real Postgres, and this
  module has no database driver in `go.mod`. 4 do not apply to a wallet
  (no webhooks, no reconciler, no authorizations, no failed state). 3 have
  nothing to test against: no currency conversion, no split, no retry rule.
- **One report**, [`testigo/REPORT.md`](testigo/REPORT.md), with every claim
  pointing at a file and line, and a money-safe fix for each problem.

A few of the failures, as `go test` prints them:

```
--- FAIL: TestTransferIsAtomic/settle_fails_between_the_debit_and_the_credit
    the two wallets held 1000 together before the transfer and 700 after it failed: 300 minor units were destroyed
--- FAIL: TestSelfTransferRefused/part_of_the_balance
    a transfer of 100 from w-1 to w-1 was accepted and changed the balance from 500 to 600: money was created from nothing
--- FAIL: TestIdemReplay/a_deposit_that_failed_is_answered_with_the_same_failure,_not_run_again
    both attempts reported failure, yet the balance is 200: the wallet was credited while the caller was told the deposit failed
--- FAIL: FuzzMoneyRoundTripExact/seed#5
    sent 9007199254740993, the receiving wallet got 9007199254740992: the amount lost 1 minor unit(s) on its way through float64 (service/transfer.go:18)
```

## What to read, in order

| File | What it is |
|---|---|
| [`testigo/answers/`](testigo/answers) | Round 1: the agent's answers to the questions the compiler could not settle, each with a `file:line` |
| [`testigo/answers/testCase.json`](testigo/answers/testCase.json) | Round 2: one entry per test case, either the test's source or the reason it is blocked |
| [`api/*_testigo_test.go`](api) | The 12 tests, exactly as testigo wrote them into the package |
| [`testigo/agentResponse.json`](testigo/agentResponse.json) | Everything collected, including each test's result and output |
| [`testigo/REPORT.md`](testigo/REPORT.md) | The report the agent wrote last |
| [`testigo/flow.json`](testigo/flow.json) | What phase 1 proved: the flow, the seams, the state machine and the findings |

## Run the tests yourself

```sh
cd examples/walletsvc
go test ./api/
```

They need nothing but Go: the service reaches its database through an
interface, so every test fakes it. `TestIdemConcurrent` races 32 goroutines,
so its exact balance changes from run to run. It is wrong every time.

## Make the run again

From a clone of this repository:

```sh
go install github.com/katayunak/testigo/cmd/testigo@latest
cp -r testdata/walletsvc /tmp/walletsvc && cd /tmp/walletsvc
testigo scan .
testigo ask --round 1 .     # point your agent at testigo/asks/INSTRUCTIONS.md
testigo collect .
testigo ask --round 2 .     # the agent writes the tests
testigo collect .           # testigo builds, vets and runs them with -race
testigo report --ask .      # the agent writes testigo/REPORT.md
```

Or skip the files: `testigo mcp .` serves both rounds as MCP tools.
