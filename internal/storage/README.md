## Flow Persistence and Merge

Testigo stores its scan results in `.testigo/flow.json`. Each run loads the previous flow, scans the current repository, merges reusable agent NOTES, and then saves the new flow.

```text
Previous flow
     │
     ▼
   Load
     │
     ▼
Current source → Scan → New flow
                       │
                       ▼
                     Merge
                       │
                       ▼
                     Save
                       │
                       ▼
                .testigo/flow.json
```
## Merging Previous Knowledge

After scanning the repository, Testigo compares the new flow with the previous run and decides which agent notes can
safely be reused.

The key rule is:

> **Facts are always recomputed from the current source, Only *expensive agent notes* are carried forward when the code is
still trustworthy.**

### MergeStats

`MergeStats` summarizes what happened to the previous run's notes:

| Result     | Meaning                                                             | Agent needed? |
|------------|---------------------------------------------------------------------|---------------|
| `Fresh`    | Same function, unchanged body; notes are reused                     | No            |
| `Stale`    | Same function, but its body changed; old notes are discarded        | Yes           |
| `Moved`    | Function moved/renamed without structural changes; notes are reused | No            |
| `Orphaned` | Function disappeared; notes are preserved in `orphans`              | No            |
| `New`      | Function has no previous notes                                      | Yes           |

The number that matters for cost is:

```text
NeedsAgent = Stale + New