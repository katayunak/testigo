### Does current repo matches the saved one in the sidecar file?

To *Resolve()* this, first we gather our needed data using GO's AST internal package.
All we looking for is one of these statuses:

| Outcome    | What changed?                                                     | What Testigo does                                                                  | Agent cost   |
|------------|-------------------------------------------------------------------|------------------------------------------------------------------------------------|--------------|
| `fresh`    | Same symbol, same function structure                              | Reuses the existing node and its agent notes                                       | **None**     |
| `stale`    | Same symbol, but the function structure changed                   | Re-scans the changed function && refreshes its agent notes                         | **One node** |
| `moved`    | Function was renamed or relocated, but its structure is unchanged | Updates the code reference's pkg + symbol && preserves the existing notes          | **None**     |
| `orphaned` | Function can no longer be found                                   | Moves its notes to `orphans` so they are preserved but no longer treated as active | **None**     |

### Function Body Hash

Testigo hashes the function's AST rather than its source text.

**Deliberately excluded from the hash:**

- Comments and doc comments
- Whitespace and formatting
- The function's own name
- The receiver variable's name

This means a rename or formatting-only change does not invalidate the existing analysis.

**Deliberately included in the hash:**

- Parameter and result types
- Every statement
- Every operator
- Every identifier
- Every literal

If any of these change, the function's behavior may have changed, so its existing notes must be re-checked.

### Why `crypto/sha256`?

Testigo uses GO's standard `crypto/sha256` package to turn the normalized AST representation into a stable, compact
fingerprint.

SHA-256 is a good fit because it is:

- **Deterministic** — the same AST produces the same hash
- **Stable** — independent of formatting and comments
- **Collision-resistant** — extremely unlikely for two different functions to produce the same hash
- **Standard library** — no external dependency

### The Index

The `Index` is the **lookup table for the current version of the repository**.

Without it, `Resolve()` would have to scan every function whenever it asks:

> What happened to this saved `CodeRef`?

The index lets `Resolve()` answer two questions efficiently:

1. **Does this exact function still exist?**
2. **If not, did the same function move or get renamed?**

Conceptually:

```text
Current repository
        │
      scan
        ▼
      Index
   ┌────┼────┐
   ▼    ▼    ▼
 byID byHash nodes
```
Scan creates a new flow and initialize the index in repositories packages and entry points at the start.