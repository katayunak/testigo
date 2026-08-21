### Function Body Hash

Testigo hashes the function's AST rather than its source text.

**Excluded from the hash:**
- Comments and doc comments
- Whitespace and formatting
- The function's own name
- The receiver variable's name

This means a rename or formatting-only change does not invalidate the existing analysis.

**Included in the hash:**
- Parameter and result types
- Every statement
- Every operator
- Every identifier
- Every literal

If any of these change, the function's behavior may have changed, so its existing notes must be re-checked.

### Why `crypto/sha256`?

Testigo uses Go's standard `crypto/sha256` package to turn the normalized AST representation into a stable, compact fingerprint.

SHA-256 is a good fit because it is:
- **Deterministic** — the same AST produces the same hash
- **Stable** — independent of formatting and comments
- **Collision-resistant** — extremely unlikely for two different functions to produce the same hash
- **Standard library** — no external dependency