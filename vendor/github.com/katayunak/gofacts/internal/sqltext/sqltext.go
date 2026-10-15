// Package sqltext is the lexing both SQL readers share: comments, statement
// boundaries, quoting and parentheses.
package sqltext

import "strings"

// StripComments blanks out -- and /* */ comments, leaving newlines in place so
// every later line number still points at the original file. Quoted strings,
// quoted identifiers and dollar-quoted bodies are left untouched: a "--"
// inside a string literal is not a comment.
func StripComments(sql string) string {
	b := []byte(sql)
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '\'' || b[i] == '"' || b[i] == '`':
			i = skipQuoted(b, i)
		case b[i] == '$':
			if end, ok := skipDollar(b, i); ok {
				i = end
			}
		case b[i] == '-' && i+1 < len(b) && b[i+1] == '-':
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			depth := 0
			for i < len(b) {
				if b[i] == '/' && i+1 < len(b) && b[i+1] == '*' {
					depth++
					b[i], b[i+1] = ' ', ' '
					i += 2
					continue
				}
				if b[i] == '*' && i+1 < len(b) && b[i+1] == '/' {
					depth--
					b[i], b[i+1] = ' ', ' '
					i += 2
					if depth == 0 {
						break
					}
					continue
				}
				if b[i] != '\n' {
					b[i] = ' '
				}
				i++
			}
			i--
		}
	}
	return string(b)
}

func skipQuoted(b []byte, i int) int {
	q := b[i]
	for j := i + 1; j < len(b); j++ {
		if b[j] == q {
			if j+1 < len(b) && b[j+1] == q {
				j++
				continue
			}
			return j
		}
	}
	return len(b) - 1
}

// skipDollar recognises $$...$$ and $tag$...$tag$ bodies, the way Postgres
// functions and DO blocks quote their source.
func skipDollar(b []byte, i int) (int, bool) {
	j := i + 1
	for j < len(b) && (b[j] == '_' || isAlnum(b[j])) {
		j++
	}
	if j >= len(b) || b[j] != '$' {
		return i, false
	}
	if j > i+1 && b[i+1] >= '0' && b[i+1] <= '9' {
		return i, false
	}
	tag := string(b[i : j+1])
	end := strings.Index(string(b[j+1:]), tag)
	if end < 0 {
		return len(b) - 1, true
	}
	return j + 1 + end + len(tag) - 1, true
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

type Chunk struct {
	Text string
	Line int
}

// Split cuts on semicolons outside quotes and dollar bodies and
// records the line each statement starts on.
func Split(sql string) []Chunk {
	b := []byte(sql)
	var out []Chunk
	start, line := 0, 1
	startLine := 1
	flush := func(end int) {
		text := string(b[start:end])
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			lead := strings.Count(text[:strings.Index(text, trimmed[:1])], "\n")
			out = append(out, Chunk{Text: trimmed, Line: startLine + lead})
		}
	}
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '\n':
			line++
		case '\'', '"', '`':
			end := skipQuoted(b, i)
			line += strings.Count(string(b[i:end+1]), "\n")
			i = end
		case '$':
			if end, ok := skipDollar(b, i); ok {
				line += strings.Count(string(b[i:end+1]), "\n")
				i = end
			}
		case ';':
			flush(i)
			start, startLine = i+1, line
		}
	}
	flush(len(b))
	return out
}

// Balanced returns the text inside the parenthesis that opens at s[open] and
// the index just after its closing parenthesis.
func Balanced(s string, open int) (string, int, bool) {
	if open < 0 || open >= len(s) || s[open] != '(' {
		return "", open, false
	}
	depth := 0
	b := []byte(s)
	for i := open; i < len(b); i++ {
		switch b[i] {
		case '\'', '"', '`':
			i = skipQuoted(b, i)
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[open+1 : i], i + 1, true
			}
		}
	}
	return "", len(s), false
}

// SplitTopLevel cuts on commas that are not inside parentheses or quotes.
func SplitTopLevel(body string) []string {
	var out []string
	depth, start := 0, 0
	b := []byte(body)
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '\'', '"', '`':
			i = skipQuoted(b, i)
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	return append(out, body[start:])
}

func Space(s string) string { return strings.Join(strings.Fields(s), " ") }

// CleanIdent strips quoting and any schema qualifier: "public"."payments" and
// `payments` are both payments.
func CleanIdent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer(`"`, "", "`", "", "[", "", "]", "").Replace(s)
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func SplitColumns(s string) []string {
	var out []string
	for _, part := range SplitTopLevel(s) {
		part = strings.TrimSpace(part)
		if part == "" || strings.ContainsAny(part, "()") {
			continue
		}
		if i := strings.IndexAny(part, " \t\n"); i >= 0 {
			part = part[:i]
		}
		if c := CleanIdent(part); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func IsIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
