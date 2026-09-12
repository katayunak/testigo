package patterns

import (
	"regexp"
	"strings"
	"unicode"
)

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
		case r == '_' || r == '-' || r == '.' || r == ' ':
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

func HasWord(name string, set map[string]bool) bool {
	for _, w := range Words(name) {
		if set[w] {
			return true
		}
	}
	return false
}

func wordSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

var currencyWords = wordSet("currency", "currencies", "curr", "ccy", "iso4217", "currencycode")

var counterWords = wordSet(
	"page", "pages", "pagination", "paging", "count", "counts", "counter",
	"index", "idx", "offset", "limit", "len", "length", "size", "num", "number",
	"rows", "records", "items", "quantity", "qty", "capacity", "attempts", "retries",
	"seconds", "millis", "ms", "duration", "timeout", "ttl", "interval", "job", "lock",
)

var containerWords = wordSet(
	"paging", "pagination", "page", "meta", "metadata", "cursor", "filter",
	"query", "search", "config", "configs", "configuration", "settings", "options",
)

func IsCurrencyName(name string) bool { return HasWord(name, currencyWords) }

func IsCounterName(name string) bool { return HasWord(name, counterWords) }

func IsContainerName(name string) bool { return HasWord(name, containerWords) }
