package scanningFlow

import (
	"regexp"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

var (
	reCreateTableFull = regexp.MustCompile(`(?is)create\s+table\s+(?:if\s+not\s+exists\s+)?([\w."]+)\s*\((.*?)\n\s*\)[^;]*;`)
	reAlterFK         = regexp.MustCompile(`(?is)alter\s+table\s+(?:only\s+)?([\w."]+)\s+add\s+constraint\s+\S+\s+foreign\s+key\s*\(([^)]*)\)\s*references\s+([\w."]+)\s*(?:\(([^)]*)\))?([^;]*);`)
	reAlterCheck      = regexp.MustCompile(`(?is)alter\s+table\s+(?:only\s+)?([\w."]+)\s+add\s+constraint\s+\S+\s+check\s*\((.*?)\)\s*;`)
	reCreateEnum      = regexp.MustCompile(`(?is)create\s+type\s+([\w."]+)\s+as\s+enum\s*\(([^)]*)\)`)

	reColRefs    = regexp.MustCompile(`(?is)references\s+([\w."]+)\s*(?:\(([^)]*)\))?`)
	reOnDelete   = regexp.MustCompile(`(?is)on\s+delete\s+(cascade|restrict|set\s+null|set\s+default|no\s+action)`)
	reInlineChk  = regexp.MustCompile(`(?is)check\s*\((.*)\)`)
	reDefault    = regexp.MustCompile(`(?is)\bdefault\s+('[^']*'|[\w.()':]+)`)
	reInList     = regexp.MustCompile(`(?is)\bin\s*\(([^)]*)\)`)
	reQuotedItem = regexp.MustCompile(`'([^']*)'`)

	reTableLevel = regexp.MustCompile(`(?i)^\s*(constraint|primary\s+key|unique|check|foreign\s+key|exclude|like|partition)\b`)
)

func parseSchema(sql, file string) (tables []flowEntity.Table, fks []flowEntity.ForeignKey, checks []flowEntity.Check) {

	for _, m := range reCreateEnum.FindAllStringSubmatchIndex(sql, -1) {
		checks = append(checks, flowEntity.Check{
			Expr:   "enum type " + cleanIdent(sql[m[2]:m[3]]),
			Values: quotedItems(sql[m[4]:m[5]]),
			File:   file, Line: lineAt(sql, m[0]),
		})
	}

	for _, m := range reCreateTableFull.FindAllStringSubmatchIndex(sql, -1) {
		table := cleanIdent(sql[m[2]:m[3]])
		body := sql[m[4]:m[5]]
		line := lineAt(sql, m[0])

		t := flowEntity.Table{Name: table, File: file, Line: line}
		for _, item := range splitTopLevel(body) {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if reTableLevel.MatchString(item) {

				if fk, ok := tableLevelFK(table, item, file, line); ok {
					fks = append(fks, fk)
				}
				if c, ok := tableLevelCheck(table, item, file, line); ok {
					checks = append(checks, c)
				}
				continue
			}
			col, ok := parseColumn(item)
			if !ok {
				continue
			}
			t.Columns = append(t.Columns, col)

			if r := reColRefs.FindStringSubmatch(item); r != nil {
				fk := flowEntity.ForeignKey{
					Table: table, Columns: []string{col.Name},
					RefTable: cleanIdent(r[1]), File: file, Line: line,
				}
				if len(r) > 2 && strings.TrimSpace(r[2]) != "" {
					fk.RefColumns = splitColumns(r[2])
				}
				if d := reOnDelete.FindStringSubmatch(item); d != nil {
					fk.OnDelete = strings.ToUpper(space(d[1]))
				}
				fks = append(fks, fk)
			}
			if ch := reInlineChk.FindStringSubmatch(item); ch != nil {
				checks = append(checks, flowEntity.Check{
					Table: table, Expr: space(ch[1]),
					Values: inListValues(ch[1]), File: file, Line: line,
				})
			}
		}
		if len(t.Columns) > 0 {
			tables = append(tables, t)
		}
	}

	for _, m := range reAlterFK.FindAllStringSubmatchIndex(sql, -1) {
		fk := flowEntity.ForeignKey{
			Table:    cleanIdent(sql[m[2]:m[3]]),
			Columns:  splitColumns(sql[m[4]:m[5]]),
			RefTable: cleanIdent(sql[m[6]:m[7]]),
			File:     file, Line: lineAt(sql, m[0]),
		}
		if m[8] >= 0 {
			fk.RefColumns = splitColumns(sql[m[8]:m[9]])
		}
		if m[10] >= 0 {
			if d := reOnDelete.FindStringSubmatch(sql[m[10]:m[11]]); d != nil {
				fk.OnDelete = strings.ToUpper(space(d[1]))
			}
		}
		fks = append(fks, fk)
	}

	for _, m := range reAlterCheck.FindAllStringSubmatchIndex(sql, -1) {
		expr := sql[m[4]:m[5]]
		checks = append(checks, flowEntity.Check{
			Table: cleanIdent(sql[m[2]:m[3]]), Expr: space(expr),
			Values: inListValues(expr), File: file, Line: lineAt(sql, m[0]),
		})
	}
	return tables, fks, checks
}

func parseColumn(item string) (flowEntity.Column, bool) {
	fields := strings.Fields(item)
	if len(fields) < 2 {
		return flowEntity.Column{}, false
	}
	name := cleanIdent(fields[0])
	if name == "" || !isIdent(name) {
		return flowEntity.Column{}, false
	}
	c := flowEntity.Column{
		Name: name,

		Type:       columnType(fields[1:]),
		NotNull:    matchesWord(item, "not null"),
		PrimaryKey: matchesWord(item, "primary key"),
	}
	if d := reDefault.FindStringSubmatch(item); d != nil {
		c.Default = strings.Trim(d[1], "'")
	}
	return c, true
}

var colOptionWord = map[string]bool{
	"not": true, "null": true, "default": true, "primary": true, "unique": true,
	"references": true, "check": true, "constraint": true, "generated": true,
	"collate": true, "on": true, "deferrable": true,
}

func columnType(rest []string) string {
	var parts []string
	for _, w := range rest {
		if colOptionWord[strings.ToLower(strings.TrimRight(w, ","))] {
			break
		}
		parts = append(parts, w)
	}
	return strings.TrimSuffix(strings.Join(parts, " "), ",")
}

func tableLevelFK(table, item, file string, line int) (flowEntity.ForeignKey, bool) {
	i := strings.Index(strings.ToLower(item), "foreign key")
	if i < 0 {
		return flowEntity.ForeignKey{}, false
	}
	cols := firstParens(item[i:])
	r := reColRefs.FindStringSubmatch(item)
	if r == nil || cols == "" {
		return flowEntity.ForeignKey{}, false
	}
	fk := flowEntity.ForeignKey{
		Table: table, Columns: splitColumns(cols),
		RefTable: cleanIdent(r[1]), File: file, Line: line,
	}
	if len(r) > 2 && strings.TrimSpace(r[2]) != "" {
		fk.RefColumns = splitColumns(r[2])
	}
	if d := reOnDelete.FindStringSubmatch(item); d != nil {
		fk.OnDelete = strings.ToUpper(space(d[1]))
	}
	return fk, true
}

func tableLevelCheck(table, item, file string, line int) (flowEntity.Check, bool) {
	i := strings.Index(strings.ToLower(item), "check")
	if i < 0 {
		return flowEntity.Check{}, false
	}
	expr := firstParens(item[i:])
	if expr == "" {
		return flowEntity.Check{}, false
	}
	return flowEntity.Check{
		Table: table, Expr: space(expr), Values: inListValues(expr),
		File: file, Line: line,
	}, true
}

func splitTopLevel(body string) []string {
	var out []string
	depth, start := 0, 0
	inQuote := false
	for i, r := range body {
		switch {
		case r == '\'':
			inQuote = !inQuote
		case inQuote:
		case r == '(':
			depth++
		case r == ')':
			depth--
		case r == ',' && depth == 0:
			out = append(out, body[start:i])
			start = i + 1
		}
	}
	return append(out, body[start:])
}

func firstParens(s string) string {
	i := strings.Index(s, "(")
	if i < 0 {
		return ""
	}
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[i+1 : j]
			}
		}
	}
	return ""
}

func inListValues(expr string) []string {
	m := reInList.FindStringSubmatch(expr)
	if m == nil {
		return nil
	}
	return quotedItems(m[1])
}

func quotedItems(s string) []string {
	var out []string
	for _, m := range reQuotedItem.FindAllStringSubmatch(s, -1) {
		if v := strings.TrimSpace(m[1]); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func matchesWord(s, phrase string) bool {
	return strings.Contains(space(strings.ToLower(s)), phrase)
}

func space(s string) string { return strings.Join(strings.Fields(s), " ") }

func isIdent(s string) bool {
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
