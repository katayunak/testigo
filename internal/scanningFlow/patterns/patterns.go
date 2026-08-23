// Package patterns holds every name-matching rule testigo uses to recognise
// things in a repository it has never seen before.
//
// They live together, in one file per concern, for one reason: these are the
// rules most likely to be WRONG for your codebase, and the ones you will want to
// edit first. A rule buried inside three hundred lines of SSA analysis is a rule
// nobody finds. A rule in patterns/state.go is a rule you can read over coffee
// and fix in a minute.
//
// A word on what these are and are not. Everything here is a HEURISTIC: a guess
// based on how people usually name things. The call graph, the type checking and
// the reachability analysis are proofs. These are not. That difference matters
// when a result surprises you — a missing seam is usually a missing row in one
// of these tables, not a bug in the analysis.
//
// A missing row is not a small problem. If a repository uses go-pg and go-pg is
// absent from the I/O table, testigo reports that the payment flow never touches
// a database. That is a confident wrong answer, which is worse than no answer.
// When you point testigo at a new repository, read its direct dependencies
// against these tables first.
package patterns

import "regexp"

// anyOf builds a case-insensitive alternation from a word list, so the tables
// below can stay readable lists instead of hand-written regular expressions.
func anyOf(words ...string) *regexp.Regexp {
	pattern := `(?i)(`
	for i, w := range words {
		if i > 0 {
			pattern += "|"
		}
		pattern += w
	}
	return regexp.MustCompile(pattern + `)`)
}

// endingWith matches a name that ENDS in one of the words. Used for type names,
// where the meaningful word is the suffix: PaymentStatus, OrderState, TxnPhase.
func endingWith(words ...string) *regexp.Regexp {
	pattern := `(?i)(`
	for i, w := range words {
		if i > 0 {
			pattern += "|"
		}
		pattern += w
	}
	return regexp.MustCompile(pattern + `)s?$`)
}
