# studies

The research testigo is built on. Each one exists because a decision in the code needed
defending. None of them is a literature review for its own sake.

| Study | Question it answers | Where it shows up in the code |
|---|---|---|
| [How real fintech systems test payment flows](howRealFintechSystemsTestPaymentFlows.md) | What do companies that actually move money at scale test for? | the 31 scenarios in `internal/testPlan/catalog.go` |
| [Context engineering](contextEngineering.md) | How do you get a better answer from a model for fewer tokens? | `internal/agent/planner`, `prompts/preamble.go`, the whole two-phase split |
| [How tables are written, and how that breaks](howTablesAreWrittenAndHowThatBreaks.md) | How does the way a table is written decide how it can break? | the write-pattern classifier and the scenarios that depend on it |

## The rule they all follow

**Cite a primary source, or say you could not find one.**

Every scenario in the catalogue traces back to one of these documents. Where the
research found no reliable evidence, they say so rather than filling the gap with
something that merely sounds right.
