# planner — deciding what an answer is worth

A query planner does not run every join it could; it prices the options and picks one.
This package does the same for prompts.

`testigo ask --explain` prints the decision. `--budget N` caps it.

## Three inputs

| Input | File | Question it answers |
|---|---|---|
| **demand** | `demand.go` | which facts do the *runnable* scenarios actually require? |
| **consumers** | `consumers.go` | what reads each answer field? |
| **cost** | `cost.go` | what would this ask cost, weighted? |

## Output is weighted five times

```go
func (c Cost) Weighted() int { return c.Input + c.Output*OutputMultiplier }
```

Roughly what it bills. Measuring this way reorders every optimisation: trimming a long
prompt feels productive and is nearly worthless next to removing one open-ended field
from the answer shape.

Cost per field, calibrated from real runs:

| Shape | Output tokens per field |
|---|---|
| true/false | 5 |
| pick one | 15 |
| free text | 21–131, measured per kind |

## The consumer registry

Every answer field is listed with what reads it:

```go
{"moneyModel.money.type", Live, "plan.FactsFrom -> Facts.MoneyType"},
{"stateRoles.roles",      Live, "prompts.TestCase via Transitions, IsFinal, Illegal"},
```

Writing this down found **35 fields** that were requested, parsed, validated, stored —
and read by nothing. Every one was paid for on every run.

A test enforces it: a field graded as read must name its reader.

It no longer changes a planner decision, since every surviving field is `Live`. It is
kept because it is the instrument that makes unnecessary asks *findable*, and because
the rule it encodes — register a reader or do not add the field — is the reason the
answer shapes stay honest.

## Failing open

An ask whose kind is not in the registry is **kept and flagged**, never dropped:

```
not in the consumer registry — kept until someone records what reads it
```

Silently dropping an unregistered ask would make forgetting to register a new one look
like a cost saving. Keeping it noisy makes the omission visible instead.

## What `--explain` shows

```
ASKED       what is being bought, the method, and the price
NOT ASKED   what was skipped, why, and what that saved
STILL PAID FOR, NOTHING READS IT   fields to drop from the prompt
cost / avoided / saving
```

The third section is the one worth reading. It names the fields still being collected
that nothing consumes — the list that shrank this project's round one from 123.9k to
30.7k weighted tokens.
