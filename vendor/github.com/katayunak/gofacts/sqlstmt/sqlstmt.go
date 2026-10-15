// Package sqlstmt reads the data statements a program sends: which table they
// touch, which columns, and which placeholder feeds which column. The last part
// is what lets a Go field be tied to the column it is stored in — an INSERT
// that binds p.IdempotencyKey to $2 for column idem_key says more about the
// schema than any struct tag.
//
// It is pattern-based. Statements built at runtime from fragments, or written
// in an ORM's DSL, are invisible to it.
package sqlstmt

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/katayunak/gofacts/internal/sqltext"
)

type Kind string

const (
	Insert Kind = "insert"
	Update Kind = "update"
	Delete Kind = "delete"
	Select Kind = "select"
	Other  Kind = "other"
)

type Statement struct {
	Kind  Kind
	Table string

	// Columns are the INSERT target columns or the UPDATE SET columns.
	Columns []string
	// Binds maps a column written by the statement to the 1-based placeholder
	// that supplies its value.
	Binds map[string]int

	// Where lists columns compared for equality in the WHERE clause, and
	// WhereBinds the placeholder each is compared to.
	Where      []string
	WhereBinds map[string]int

	Upsert          bool
	ConflictColumns []string

	// SelfArith lists SET columns computed from themselves (balance = balance
	// - $1): the database does the arithmetic, so no update is lost.
	SelfArith []string

	Locks        bool
	Placeholders int
}

func (s Statement) Writes() bool { return s.Kind == Insert || s.Kind == Update || s.Kind == Delete }

var (
	reInsert   = regexp.MustCompile(`(?is)^(?:with\b.*?\)\s*)?insert\s+(?:ignore\s+)?into\s+(` + ident + `)(?:\s+as\s+\w+)?\s*(\()?`)
	reValues   = regexp.MustCompile(`(?is)^\s*values\s*\(`)
	reUpdate   = regexp.MustCompile(`(?is)^(?:with\b.*?\)\s*)?update\s+(?:only\s+)?(` + ident + `)(?:\s+(?:as\s+)?\w+)?\s+set\s+`)
	reDelete   = regexp.MustCompile(`(?is)^(?:with\b.*?\)\s*)?delete\s+from\s+(?:only\s+)?(` + ident + `)`)
	reSelect   = regexp.MustCompile(`(?is)^(?:with\b.*?\)\s*)?select\b.*?\bfrom\s+(?:only\s+)?(` + ident + `)`)
	reSetEnd   = regexp.MustCompile(`(?is)\b(from|where|returning)\b`)
	reWhere    = regexp.MustCompile(`(?is)\bwhere\b(.*?)(?:\bgroup\s+by\b|\border\s+by\b|\blimit\b|\bfor\s+(?:no\s+key\s+)?(?:update|share)\b|\breturning\b|\bon\s+conflict\b|$)`)
	reEquals   = regexp.MustCompile(`(?is)(` + ident + `)\s*=\s*(\$\d+|\?|'[^']*'|[\w.]+)`)
	reConflict = regexp.MustCompile(`(?is)\bon\s+conflict\s*(\([^)]*\))?`)
	reDupKey   = regexp.MustCompile(`(?is)\bon\s+duplicate\s+key\s+update\b`)
	reLock     = regexp.MustCompile(`(?is)\bfor\s+(?:no\s+key\s+)?(?:update|share)\b`)
	reDollar   = regexp.MustCompile(`^\$(\d+)$`)
)

const ident = "[\\w.\"`]+"

// Looks reports whether s plausibly is a SQL data statement, so that every
// string constant in a program is not parsed.
func Looks(s string) bool {
	t := strings.TrimSpace(sqltext.StripComments(s))
	return reInsert.MatchString(t) || reUpdate.MatchString(t) || reDelete.MatchString(t) ||
		reSelectAny.MatchString(t)
}

var reSelectAny = regexp.MustCompile(`(?is)^(?:with\b.*?\)\s*)?select\s`)

func Parse(sql string) []Statement {
	var out []Statement
	for _, c := range sqltext.Split(sqltext.StripComments(sql)) {
		out = append(out, parse(c.Text))
	}
	return out
}

func parse(s string) Statement {
	ph := placeholders(s)
	st := Statement{Kind: Other, Placeholders: ph.count}

	switch m := reInsert.FindStringSubmatchIndex(s); {
	case m != nil:
		st.Kind, st.Table = Insert, sqltext.CleanIdent(s[m[2]:m[3]])
		if m[4] >= 0 {
			cols, after, ok := sqltext.Balanced(s, m[4])
			if ok {
				st.Columns = sqltext.SplitColumns(cols)
				if v := reValues.FindStringIndex(s[after:]); v != nil {
					open := after + v[1] - 1
					vals, _, _ := sqltext.Balanced(s, open)
					st.Binds = bindList(st.Columns, sqltext.SplitTopLevel(vals), ph, open+1)
				}
			}
		}
		if c := reConflict.FindStringSubmatch(s); c != nil {
			st.Upsert = true
			if c[1] != "" {
				st.ConflictColumns = sqltext.SplitColumns(strings.Trim(c[1], "()"))
			}
		}
		if reDupKey.MatchString(s) {
			st.Upsert = true
		}

	case reUpdate.MatchString(s):
		m := reUpdate.FindStringSubmatchIndex(s)
		st.Kind, st.Table = Update, sqltext.CleanIdent(s[m[2]:m[3]])
		rest := s[m[1]:]
		end := len(rest)
		if e := reSetEnd.FindStringIndex(rest); e != nil {
			end = e[0]
		}
		st.Binds = map[string]int{}
		offset := m[1]
		for _, assign := range sqltext.SplitTopLevel(rest[:end]) {
			col, expr, ok := strings.Cut(assign, "=")
			if !ok {
				offset += len(assign) + 1
				continue
			}
			name := sqltext.CleanIdent(col)
			st.Columns = append(st.Columns, name)
			if n := ph.at(offset+len(col)+1+leading(expr), strings.TrimSpace(expr)); n > 0 {
				st.Binds[name] = n
			}
			if selfArith(name, expr) {
				st.SelfArith = append(st.SelfArith, name)
			}
			offset += len(assign) + 1
		}

	case reDelete.MatchString(s):
		st.Kind, st.Table = Delete, sqltext.CleanIdent(reDelete.FindStringSubmatch(s)[1])

	case reSelect.MatchString(s):
		st.Kind, st.Table = Select, sqltext.CleanIdent(reSelect.FindStringSubmatch(s)[1])
	}

	if st.Kind != Other {
		st.Where, st.WhereBinds = whereColumns(s, ph)
		st.Locks = reLock.MatchString(s)
	}
	return st
}

func leading(s string) int { return len(s) - len(strings.TrimLeft(s, " \t\n\r")) }

var reArith = regexp.MustCompile(`(?is)^\s*([\w."` + "`" + `]+)\s*[-+*/]`)

func selfArith(col, expr string) bool {
	m := reArith.FindStringSubmatch(expr)
	return m != nil && strings.EqualFold(sqltext.CleanIdent(m[1]), col)
}

// placeholderSet records where each placeholder sits, because "?" placeholders
// are numbered by position in the statement rather than written out.
type placeholderSet struct {
	count    int
	ordinals map[int]int
}

func placeholders(s string) placeholderSet {
	ps := placeholderSet{ordinals: map[int]int{}}
	question := 0
	maxDollar := 0
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '\'':
			for i++; i < len(b) && b[i] != '\''; i++ {
			}
		case '?':
			question++
			ps.ordinals[i] = question
		case '$':
			j := i + 1
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			if j > i+1 {
				n, _ := strconv.Atoi(string(b[i+1 : j]))
				ps.ordinals[i] = n
				if n > maxDollar {
					maxDollar = n
				}
				i = j - 1
			}
		}
	}
	ps.count = question
	if maxDollar > ps.count {
		ps.count = maxDollar
	}
	return ps
}

// at returns the placeholder ordinal of expr if expr is exactly a placeholder
// starting at offset.
func (ps placeholderSet) at(offset int, expr string) int {
	if expr == "?" {
		return ps.ordinals[offset]
	}
	if m := reDollar.FindStringSubmatch(expr); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func bindList(cols, vals []string, ps placeholderSet, offset int) map[string]int {
	binds := map[string]int{}
	for i, v := range vals {
		if i < len(cols) {
			if n := ps.at(offset+leading(v), strings.TrimSpace(v)); n > 0 {
				binds[cols[i]] = n
			}
		}
		offset += len(v) + 1
	}
	return binds
}

func whereColumns(s string, ps placeholderSet) ([]string, map[string]int) {
	loc := reWhere.FindStringSubmatchIndex(s)
	if loc == nil {
		return nil, nil
	}
	clause, base := s[loc[2]:loc[3]], loc[2]
	var cols []string
	binds := map[string]int{}
	for _, m := range reEquals.FindAllStringSubmatchIndex(clause, -1) {
		col := sqltext.CleanIdent(clause[m[2]:m[3]])
		if col == "" || isKeyword(col) {
			continue
		}
		cols = append(cols, col)
		if n := ps.at(base+m[4], clause[m[4]:m[5]]); n > 0 {
			binds[col] = n
		}
	}
	return cols, binds
}

func isKeyword(s string) bool {
	switch strings.ToLower(s) {
	case "and", "or", "not", "null", "true", "false":
		return true
	}
	return false
}
