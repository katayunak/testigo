// Package sqlschema reads DDL the way a database would apply it: statement by
// statement, in migration order, so a constraint that a later migration drops
// is not still believed to exist.
//
// It is a pattern-based reader, not a SQL parser. It understands the shapes
// migrations are written in for Postgres and MySQL and says nothing about the
// rest; a statement it does not recognise is kept as KindOther, never guessed.
package sqlschema

import (
	"regexp"
	"strings"

	"github.com/katayunak/gofacts/fact"
	"github.com/katayunak/gofacts/internal/sqltext"
)

type Kind string

const (
	KindCreateTable Kind = "create_table"
	KindCreateIndex Kind = "create_index"
	KindAlterTable  Kind = "alter_table"
	KindDropIndex   Kind = "drop_index"
	KindDropTable   Kind = "drop_table"
	KindCreateEnum  Kind = "create_enum"
	KindRenameIndex Kind = "rename_index"
	KindOther       Kind = "other"
)

type Statement struct {
	Kind Kind
	File string
	Line int
	Text string

	Table        string
	Name         string
	IfNotExists  bool
	Unique       bool
	Concurrently bool
	Columns      []string
	Where        string
	Values       []string
	Names        []string
	// To is the new name of a renamed index.
	To string

	Def     *TableDef
	Actions []Action
}

// TableDef is the body of a CREATE TABLE.
type TableDef struct {
	Columns     []fact.Column
	Constraints []fact.Constraint
	ForeignKeys []fact.ForeignKey
	Checks      []fact.Check
}

type ActionOp string

const (
	AddColumn      ActionOp = "add_column"
	DropColumn     ActionOp = "drop_column"
	AddConstraint  ActionOp = "add_constraint"
	DropConstraint ActionOp = "drop_constraint"
	RenameColumn   ActionOp = "rename_column"
	RenameTable    ActionOp = "rename_table"
	RenameCons     ActionOp = "rename_constraint"
	AlterColumn    ActionOp = "alter_column"
	OtherAction    ActionOp = "other"
)

// Action is one clause of an ALTER TABLE. Detail keeps the rest of an ALTER
// COLUMN clause ("set not null", "type bigint") for readers that judge whether
// a change is safe to deploy.
type Action struct {
	Op     ActionOp
	Column fact.Column
	Name   string
	To     string
	Detail string
	Def    TableDef
}

const ident = "[\\w.\"`$\\[\\]]+"

var (
	reCreateTable = regexp.MustCompile(`(?is)^create\s+(?:(?:global|local)\s+)?(?:temp(?:orary)?\s+|unlogged\s+)?table\s+(if\s+not\s+exists\s+)?(` + ident + `)\s*\(`)
	reCreateIndex = regexp.MustCompile(`(?is)^create\s+(unique\s+)?index\s+(concurrently\s+)?(?:if\s+not\s+exists\s+)?(?:(` + ident + `)\s+)?on\s+(?:only\s+)?(` + ident + `)\s*(?:using\s+\w+\s*)?\(`)
	reAlterTable  = regexp.MustCompile(`(?is)^alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?(` + ident + `)\s+(.*)$`)
	reDropIndex   = regexp.MustCompile(`(?is)^drop\s+index\s+(?:concurrently\s+)?(?:if\s+exists\s+)?(.*)$`)
	reDropTable   = regexp.MustCompile(`(?is)^drop\s+table\s+(?:if\s+exists\s+)?(.*)$`)
	reCreateEnum  = regexp.MustCompile(`(?is)^create\s+type\s+(` + ident + `)\s+as\s+enum\s*\(`)
	reRenameIndex = regexp.MustCompile(`(?is)^alter\s+index\s+(?:if\s+exists\s+)?(` + ident + `)\s+rename\s+to\s+(` + ident + `)\s*$`)

	reWhere      = regexp.MustCompile(`(?is)\bwhere\b(.*)$`)
	reQuotedItem = regexp.MustCompile(`'([^']*)'`)
	reInList     = regexp.MustCompile(`(?is)\bin\s*\(([^)]*)\)`)
	reTrailing   = regexp.MustCompile(`(?i)\s+(cascade|restrict)\s*$`)
	reOnTable    = regexp.MustCompile(`(?is)\s+on\s+` + ident + `\s*$`)
)

// Parse reads every statement of sql. Line numbers are those of file.
func Parse(sql, file string) []Statement {
	var out []Statement
	for _, c := range sqltext.Split(sqltext.StripComments(sql)) {
		st := parseStatement(c.Text)
		st.File, st.Line, st.Text = file, c.Line, sqltext.Space(c.Text)
		out = append(out, st)
	}
	return out
}

func parseStatement(s string) Statement {
	if m := reCreateTable.FindStringSubmatchIndex(s); m != nil {
		table := sqltext.CleanIdent(s[m[4]:m[5]])
		body, _, ok := sqltext.Balanced(s, m[1]-1)
		if !ok {
			return Statement{Kind: KindOther}
		}
		def := parseTableBody(table, body)
		return Statement{Kind: KindCreateTable, Table: table, IfNotExists: m[2] >= 0, Def: &def}
	}
	if m := reCreateIndex.FindStringSubmatchIndex(s); m != nil {
		st := Statement{
			Kind:         KindCreateIndex,
			Unique:       m[2] >= 0,
			Concurrently: m[4] >= 0,
			Table:        sqltext.CleanIdent(s[m[8]:m[9]]),
		}
		if m[6] >= 0 {
			st.Name = sqltext.CleanIdent(s[m[6]:m[7]])
		}
		cols, after, ok := sqltext.Balanced(s, m[1]-1)
		if !ok {
			return Statement{Kind: KindOther}
		}
		st.Columns = sqltext.SplitColumns(cols)
		if w := reWhere.FindStringSubmatch(s[after:]); w != nil {
			st.Where = sqltext.Space(w[1])
		}
		if st.Name == "" {
			st.Name = st.Table + "_" + strings.Join(st.Columns, "_") + "_idx"
		}
		return st
	}
	if m := reCreateEnum.FindStringSubmatchIndex(s); m != nil {
		body, _, _ := sqltext.Balanced(s, m[1]-1)
		return Statement{Kind: KindCreateEnum, Name: sqltext.CleanIdent(s[m[2]:m[3]]), Values: quotedItems(body)}
	}
	if m := reAlterTable.FindStringSubmatch(s); m != nil {
		table := sqltext.CleanIdent(m[1])
		var actions []Action
		for _, part := range sqltext.SplitTopLevel(m[2]) {
			if part = strings.TrimSpace(part); part != "" {
				actions = append(actions, parseAction(table, part))
			}
		}
		return Statement{Kind: KindAlterTable, Table: table, Actions: actions}
	}
	if m := reRenameIndex.FindStringSubmatch(s); m != nil {
		return Statement{Kind: KindRenameIndex, Name: sqltext.CleanIdent(m[1]), To: sqltext.CleanIdent(m[2])}
	}
	if m := reDropIndex.FindStringSubmatch(s); m != nil {
		rest := reOnTable.ReplaceAllString(reTrailing.ReplaceAllString(m[1], ""), "")
		return Statement{Kind: KindDropIndex, Names: identList(rest)}
	}
	if m := reDropTable.FindStringSubmatch(s); m != nil {
		return Statement{Kind: KindDropTable, Names: identList(reTrailing.ReplaceAllString(m[1], ""))}
	}
	return Statement{Kind: KindOther}
}

func identList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if n := sqltext.CleanIdent(part); n != "" {
			out = append(out, n)
		}
	}
	return out
}

var (
	reTableLevel = regexp.MustCompile(`(?i)^\s*(constraint|primary\s+key|unique|check|foreign\s+key|exclude|like)\b`)
	// A MySQL index line names columns in its parentheses; a column called
	// key with a type like varchar(64) has digits there.
	reMySQLIndex = regexp.MustCompile("(?is)^\\s*(?:fulltext\\s+|spatial\\s+)?(?:key|index)\\b(?:\\s+[\\w`\"]+)?(?:\\s+using\\s+\\w+)?\\s*\\(\\s*[^\\d\\s)]")
)

func parseTableBody(table, body string) TableDef {
	var def TableDef
	for _, item := range sqltext.SplitTopLevel(body) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if reMySQLIndex.MatchString(item) {
			continue
		}
		if reTableLevel.MatchString(item) {
			def.merge(parseConstraintSpec(table, item))
			continue
		}
		col, inline, ok := parseColumnDef(table, item)
		if !ok {
			continue
		}
		def.Columns = append(def.Columns, col)
		def.merge(inline)
	}
	return def
}

func (d *TableDef) merge(o TableDef) {
	d.Constraints = append(d.Constraints, o.Constraints...)
	d.ForeignKeys = append(d.ForeignKeys, o.ForeignKeys...)
	d.Checks = append(d.Checks, o.Checks...)
}

var (
	reConstraintName = regexp.MustCompile(`(?is)^constraint\s+(` + ident + `)\s+(.*)$`)
	rePrimaryKey     = regexp.MustCompile(`(?is)^primary\s+key\s*\(`)
	reUnique         = regexp.MustCompile(`(?is)^unique(?:\s+(?:key|index))?(?:\s+(` + ident + `))?\s*(?:nulls\s+(?:not\s+)?distinct\s*)?\(`)
	reForeignKey     = regexp.MustCompile(`(?is)^foreign\s+key\s*\(`)
	reCheck          = regexp.MustCompile(`(?is)^check\s*\(`)
	reReferences     = regexp.MustCompile(`(?is)\breferences\s+(` + ident + `)\s*`)
	reOnDelete       = regexp.MustCompile(`(?is)on\s+delete\s+(cascade|restrict|set\s+null|set\s+default|no\s+action)`)
)

// parseConstraintSpec reads a table-level constraint, either inside CREATE
// TABLE or after ALTER TABLE ... ADD.
func parseConstraintSpec(table, spec string) TableDef {
	var def TableDef
	name := ""
	spec = strings.TrimSpace(spec)
	if m := reConstraintName.FindStringSubmatch(spec); m != nil {
		name, spec = sqltext.CleanIdent(m[1]), strings.TrimSpace(m[2])
	}

	switch {
	case rePrimaryKey.MatchString(spec):
		cols, _, _ := sqltext.Balanced(spec, strings.Index(spec, "("))
		def.Constraints = append(def.Constraints, constraint(table, fact.KindPrimaryKey, name, sqltext.SplitColumns(cols)))

	case reUnique.MatchString(spec):
		m := reUnique.FindStringSubmatchIndex(spec)
		if name == "" && m[2] >= 0 {
			name = sqltext.CleanIdent(spec[m[2]:m[3]])
		}
		cols, _, _ := sqltext.Balanced(spec, m[1]-1)
		def.Constraints = append(def.Constraints, constraint(table, fact.KindUniqueConstraint, name, sqltext.SplitColumns(cols)))

	case reForeignKey.MatchString(spec):
		cols, after, _ := sqltext.Balanced(spec, strings.Index(spec, "("))
		if fk, ok := references(table, sqltext.SplitColumns(cols), spec[after:]); ok {
			def.ForeignKeys = append(def.ForeignKeys, fk)
		}

	case reCheck.MatchString(spec):
		expr, _, _ := sqltext.Balanced(spec, strings.Index(spec, "("))
		def.Checks = append(def.Checks, fact.Check{Table: table, Expr: sqltext.Space(expr), Values: inListValues(expr)})
	}
	return def
}

func constraint(table, kind, name string, cols []string) fact.Constraint {
	if name == "" {
		name = defaultName(table, kind, cols)
	}
	return fact.Constraint{Table: table, Columns: cols, Kind: kind, Name: name}
}

// defaultName follows Postgres' own naming, so a later DROP CONSTRAINT that
// relies on it finds the constraint.
func defaultName(table, kind string, cols []string) string {
	if kind == fact.KindPrimaryKey {
		return table + "_pkey"
	}
	return table + "_" + strings.Join(cols, "_") + "_key"
}

func references(table string, cols []string, rest string) (fact.ForeignKey, bool) {
	m := reReferences.FindStringSubmatchIndex(rest)
	if m == nil {
		return fact.ForeignKey{}, false
	}
	fk := fact.ForeignKey{Table: table, Columns: cols, RefTable: sqltext.CleanIdent(rest[m[2]:m[3]])}
	if open := m[1]; open < len(rest) && rest[open] == '(' {
		refCols, _, _ := sqltext.Balanced(rest, open)
		fk.RefColumns = sqltext.SplitColumns(refCols)
	}
	if d := reOnDelete.FindStringSubmatch(rest); d != nil {
		fk.OnDelete = strings.ToUpper(sqltext.Space(d[1]))
	}
	return fk, true
}

var columnOption = map[string]bool{
	"not": true, "null": true, "default": true, "primary": true, "unique": true,
	"references": true, "check": true, "constraint": true, "generated": true,
	"collate": true, "on": true, "deferrable": true, "auto_increment": true,
	"autoincrement": true, "identity": true, "comment": true,
}

var (
	reDefault      = regexp.MustCompile(`(?is)\bdefault\s+('[^']*'|[\w.():']+)`)
	reInlineUnique = regexp.MustCompile(`(?is)\bunique\b(?:\s+key\b)?\s*([^(\s]|$)`)
	reInlinePK     = regexp.MustCompile(`(?is)\bprimary\s+key\b`)
	reInlineCheck  = regexp.MustCompile(`(?is)\bcheck\s*\(`)
	reInlineName   = regexp.MustCompile(`(?is)\bconstraint\s+(` + ident + `)\s+(?:unique|primary)`)
)

// parseColumnDef reads "name type [options]" and the constraints written on
// the column itself. A column-level UNIQUE is as real as a table-level one;
// missing it made `idempotency_key text unique` look unprotected.
func parseColumnDef(table, item string) (fact.Column, TableDef, bool) {
	fields := strings.Fields(item)
	if len(fields) < 2 {
		return fact.Column{}, TableDef{}, false
	}
	name := sqltext.CleanIdent(fields[0])
	if !sqltext.IsIdent(name) {
		return fact.Column{}, TableDef{}, false
	}
	lower := strings.ToLower(sqltext.Space(item))
	col := fact.Column{
		Name:       name,
		Type:       columnType(fields[1:]),
		NotNull:    strings.Contains(lower, "not null") || reInlinePK.MatchString(item),
		PrimaryKey: reInlinePK.MatchString(item),
	}
	if d := reDefault.FindStringSubmatch(item); d != nil {
		col.Default = strings.Trim(d[1], "'")
	}

	var def TableDef
	inlineName := ""
	if m := reInlineName.FindStringSubmatch(item); m != nil {
		inlineName = sqltext.CleanIdent(m[1])
	}
	if col.PrimaryKey {
		def.Constraints = append(def.Constraints, constraint(table, fact.KindPrimaryKey, inlineName, []string{name}))
	}
	if reInlineUnique.MatchString(item) {
		def.Constraints = append(def.Constraints, constraint(table, fact.KindUniqueConstraint, inlineName, []string{name}))
	}
	if fk, ok := references(table, []string{name}, item); ok {
		def.ForeignKeys = append(def.ForeignKeys, fk)
	}
	if loc := reInlineCheck.FindStringIndex(item); loc != nil {
		expr, _, _ := sqltext.Balanced(item, loc[1]-1)
		def.Checks = append(def.Checks, fact.Check{Table: table, Expr: sqltext.Space(expr), Values: inListValues(expr)})
	}
	return col, def, true
}

func columnType(rest []string) string {
	var parts []string
	for _, w := range rest {
		if columnOption[strings.ToLower(strings.TrimRight(w, ","))] {
			break
		}
		parts = append(parts, w)
	}
	return strings.TrimSuffix(strings.Join(parts, " "), ",")
}

var (
	reAddConstraint  = regexp.MustCompile(`(?is)^add\s+((?:constraint\s+` + ident + `\s+)?(?:unique|primary\s+key|foreign\s+key|check|exclude)\b.*)$`)
	reAddColumn      = regexp.MustCompile(`(?is)^add\s+(?:column\s+)?(?:if\s+not\s+exists\s+)?(.*)$`)
	reDropConstraint = regexp.MustCompile(`(?is)^drop\s+constraint\s+(?:if\s+exists\s+)?(` + ident + `)`)
	reDropColumn     = regexp.MustCompile(`(?is)^drop\s+(?:column\s+)?(?:if\s+exists\s+)?(` + ident + `)`)
	reRenameTable    = regexp.MustCompile(`(?is)^rename\s+to\s+(` + ident + `)`)
	reRenameCons     = regexp.MustCompile(`(?is)^rename\s+constraint\s+(` + ident + `)\s+to\s+(` + ident + `)`)
	reRenameColumn   = regexp.MustCompile(`(?is)^rename\s+(?:column\s+)?(` + ident + `)\s+to\s+(` + ident + `)`)
	reAlterColumn    = regexp.MustCompile(`(?is)^alter\s+(?:column\s+)?(` + ident + `)\s+(.*)$`)
)

func parseAction(table, s string) Action {
	if m := reAddConstraint.FindStringSubmatch(s); m != nil {
		return Action{Op: AddConstraint, Def: parseConstraintSpec(table, m[1])}
	}
	if m := reAddColumn.FindStringSubmatch(s); m != nil {
		col, inline, ok := parseColumnDef(table, m[1])
		if ok {
			return Action{Op: AddColumn, Column: col, Def: inline}
		}
	}
	if m := reDropConstraint.FindStringSubmatch(s); m != nil {
		return Action{Op: DropConstraint, Name: sqltext.CleanIdent(m[1])}
	}
	if m := reDropColumn.FindStringSubmatch(s); m != nil {
		return Action{Op: DropColumn, Name: sqltext.CleanIdent(m[1])}
	}
	if m := reRenameTable.FindStringSubmatch(s); m != nil {
		return Action{Op: RenameTable, To: sqltext.CleanIdent(m[1])}
	}
	if m := reRenameCons.FindStringSubmatch(s); m != nil {
		return Action{Op: RenameCons, Name: sqltext.CleanIdent(m[1]), To: sqltext.CleanIdent(m[2])}
	}
	if m := reRenameColumn.FindStringSubmatch(s); m != nil {
		return Action{Op: RenameColumn, Name: sqltext.CleanIdent(m[1]), To: sqltext.CleanIdent(m[2])}
	}
	if m := reAlterColumn.FindStringSubmatch(s); m != nil {
		return Action{Op: AlterColumn, Name: sqltext.CleanIdent(m[1]), Detail: strings.ToLower(sqltext.Space(m[2]))}
	}
	return Action{Op: OtherAction, Detail: sqltext.Space(s)}
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
