package patterns

import "regexp"

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
