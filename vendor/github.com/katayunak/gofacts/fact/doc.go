// Package fact is the vocabulary shared by every tool that reads Go payment
// code: what a function is, where a call leaves the process, which states a
// lifecycle moves through, what the schema enforces, and what was found.
//
// It imports only the standard library, so a consumer can talk about a flow
// without importing the analyser that built it. testigo plans tests from these
// types; 1819 reviews diffs against them.
//
// Every claim carries a location. A fact without a file and line cannot be
// checked, so the types make it awkward to express one.
package fact
