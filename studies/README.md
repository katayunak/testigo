# studies

The research testigo is built on. Both documents exist because a decision in the code
needed defending, and neither is a literature review for its own sake.

| Study | Question it answers | Where it shows up in the code |
|---|---|---|
| [How real fintech systems test payment flows](howRealFintechSystemsTestPaymentFlows.md) | What do companies that actually move money at scale test for? | the 28 scenarios in `internal/testPlan/catalog.go` |
| [Context engineering](contextEngineering.md) | How do you get a better answer from a model for fewer tokens? | `internal/agent/planner`, `prompts/preamble.go`, the whole two-phase split |

## The rule both follow

**Cite a primary source, or say you could not find one.**

Every scenario in the catalogue carries a source. An entry without one is something
somebody invented, and a reader should be able to tell at a glance which is which.
Where the research found no reliable evidence, these documents say so rather than
filling the gap with something plausible.
