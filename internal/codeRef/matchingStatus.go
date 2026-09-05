package codeRef

type MatchingStatus string

const (
	Fresh MatchingStatus = "fresh"

	Stale MatchingStatus = "stale"

	Moved MatchingStatus = "moved"

	Orphaned MatchingStatus = "orphaned"
)
