// Package naming holds every rule that decides something from how code is
// named. All of it is heuristic: the type checker proves what a value is, these
// tables only guess what it is for.
//
// Matching is by whole words of an identifier, never by substring. Substring
// matching is how GlobalWeight became money ("bal"), Syntax became a tax and
// StandardFee an idempotency key ("stan").
package naming

import (
	"strings"
	"unicode"
)

// Words splits an identifier into lower-case words at case changes, digits
// boundaries and separators: HTTPStatus -> [http status], total_pages ->
// [total pages].
func Words(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ' || r == '/':
			flush()
		case unicode.IsUpper(r):
			if i > 0 && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1])) {
				flush()
			} else if i > 0 && i+1 < len(rs) && unicode.IsUpper(rs[i-1]) && unicode.IsLower(rs[i+1]) {
				flush()
			}
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return out
}

// Vocab is a set of phrases, each one or more words. A name matches when one
// of its phrases appears as consecutive words of the name.
type Vocab struct {
	phrases [][]string
}

func NewVocab(phrases ...string) Vocab {
	v := Vocab{}
	for _, p := range phrases {
		v.phrases = append(v.phrases, strings.Fields(p))
	}
	return v
}

func (v Vocab) Matches(name string) bool {
	words := Words(name)
	for _, p := range v.phrases {
		if containsRun(words, p) {
			return true
		}
	}
	return false
}

// MatchesLast reports whether the name ends with one of the phrases, which is
// how a type name says what kind of thing it is: PaymentStatus is a status,
// StatusReport is a report.
func (v Vocab) MatchesLast(name string) bool {
	words := Words(name)
	for _, p := range v.phrases {
		if len(p) <= len(words) && sameRun(words[len(words)-len(p):], p) {
			return true
		}
	}
	return false
}

func containsRun(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		if sameRun(words[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

func sameRun(words, run []string) bool {
	for i := range run {
		if words[i] != run[i] && singular(words[i]) != run[i] {
			return false
		}
	}
	return true
}

func singular(w string) string {
	switch {
	case len(w) > 3 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 3 && strings.HasSuffix(w, "ses"):
		return w[:len(w)-2]
	case len(w) > 2 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		return w[:len(w)-1]
	}
	return w
}
