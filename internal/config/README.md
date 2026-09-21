# config — everything under `testigo/`

One package owns the sidecar directory, because one directory is one concern.

```
testigo/
├── config.json   entry points          you write this
├── rules.json    what money means here you write this, optional
├── flow.json     phase 1 output        testigo writes this
├── asks/         prompts for the agent testigo writes this
└── answers/      the agent's replies   your agent writes these
```

| File | Type | Read by |
|---|---|---|
| `config.json` | `Config` | `scan`, to know where the flow starts |
| `rules.json` | `Rules` | the planner, to skip scenarios and to name the transfer function |
| `flow.json` | `flowEntity.Flow` | every phase-2 command |

## Three functions worth knowing

- `Load` / `Save` — the config.
- `LoadFlow` / `SaveFlow` — the scan result. Saved through a temp file and `os.Rename`, so an interrupted write cannot leave a half-written flow.
- `LoadRules` — returns `nil, nil` when the file is absent. Rules are optional by design.

## Schema versions are refused, not guessed

```
sidecar schema v3, this binary speaks v4: delete testigo/flow.json and rescan
```

A flow written by an older binary is not silently reinterpreted. Misreading a stale
field is worse than asking for a rescan that costs nothing.

## What `rules.json` still holds, and what it lost

It has exactly two fields now:

```json
{
  "money_movement": { "symbols": ["example.com/pay/ledger#(*Ledger).Post"] },
  "skip": [ { "scenario": "MINOR-UNIT-CONVERSION", "why": "single currency" } ]
}
```

`symbols` names the function that actually commits money, so testigo stops guessing.
`skip` turns off a catalogue scenario with the reason recorded beside it.

It used to hold `invariants`, `final_states`, `extra_state_types`, `reversal`,
`retry_policy`, `ledger` and `notes`. All of them were parsed and then **ignored** —
nothing read them. The old example file told users that invariants "become generated
tests", which was never true. They were removed rather than left as a promise the
code did not keep. Re-adding any of them is a matter of wiring a reader first.

## Legacy paths

`testigo.json`, `testigo.rules.json` and `.testigo/` are the pre-`testigo/` layout.
`Load` detects them and prints the migration rather than failing.
