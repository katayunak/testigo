package codeRef

type MatchingStatus string

const (
	// Fresh means the function still exists and its structure did not change
	// We can safely reuse its saved notes
	Fresh MatchingStatus = "fresh"

	// Stale means the function still exists, but its structure changed.
	// The old notes may no longer be correct, so we analyze it again
	Stale MatchingStatus = "stale"

	// Moved means we found the same function structure under a different name
	// or package. We keep the notes and update the codeRef
	Moved MatchingStatus = "moved"

	// Orphaned means we could not find the old function.
	// It was deleted, or renamed in a way we could not detect,
	// or moved and edited at the same time.
	// We keep its notes instead of silently deleting them, because only a
	// person can tell which of those three actually happened.
	Orphaned MatchingStatus = "orphaned"
)
