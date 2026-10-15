package fact

import "sort"

// Infra is the schema as the migrations leave it: drops and later migrations
// are applied in order, and down migrations are not mistaken for schema.
type Infra struct {
	Constraints []Constraint `json:"constraints,omitempty"`
	Tables      []Table      `json:"tables,omitempty"`
	ForeignKeys []ForeignKey `json:"foreign_keys,omitempty"`
	Checks      []Check      `json:"checks,omitempty"`

	MigrationDirs      []string `json:"migration_dirs,omitempty"`
	MigrationFiles     int      `json:"migration_files,omitempty"`
	MigrationTool      string   `json:"migration_tool,omitempty"`
	MigrationsTotal    int      `json:"migrations_total,omitempty"`
	MigrationsWithDown int      `json:"migrations_with_down,omitempty"`
	SchemaSources      []string `json:"schema_sources,omitempty"`
}

func (i Infra) SchemaKnown() bool { return len(i.Tables) > 0 || len(i.Constraints) > 0 }

func (i Infra) Empty() bool {
	return len(i.Tables) == 0 && len(i.Constraints) == 0 &&
		len(i.Checks) == 0 && len(i.ForeignKeys) == 0
}

type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns,omitempty"`
	File    string   `json:"file"`
	Line    int      `json:"line"`
}

type Column struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"not_null,omitempty"`
	Default    string `json:"default,omitempty"`
	PrimaryKey bool   `json:"primary_key,omitempty"`
}

type ForeignKey struct {
	Table      string   `json:"table"`
	Columns    []string `json:"columns"`
	RefTable   string   `json:"ref_table"`
	RefColumns []string `json:"ref_columns,omitempty"`
	OnDelete   string   `json:"on_delete,omitempty"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
}

type Check struct {
	Table  string   `json:"table"`
	Expr   string   `json:"expr"`
	Values []string `json:"values,omitempty"`
	File   string   `json:"file"`
	Line   int      `json:"line"`
}

const (
	KindPrimaryKey       = "primary_key"
	KindUniqueIndex      = "unique_index"
	KindUniqueConstraint = "unique_constraint"
)

// Constraint is a uniqueness guarantee. Where is the predicate of a partial
// unique index: uniqueness then holds only for rows that match it.
type Constraint struct {
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
	Kind    string   `json:"kind"`
	Name    string   `json:"name,omitempty"`
	Where   string   `json:"where,omitempty"`
	File    string   `json:"file"`
	Line    int      `json:"line"`
}

func (c Constraint) Unique() bool {
	switch c.Kind {
	case KindPrimaryKey, KindUniqueIndex, KindUniqueConstraint:
		return true
	}
	return false
}

func (i Infra) ColumnsOf(table string) []Column {
	for _, t := range i.Tables {
		if t.Name == table {
			return t.Columns
		}
	}
	return nil
}

func (i Infra) HasTable(table string) bool {
	for _, t := range i.Tables {
		if t.Name == table {
			return true
		}
	}
	return false
}

func (i Infra) HasColumn(table, column string) bool {
	for _, c := range i.ColumnsOf(table) {
		if c.Name == column {
			return true
		}
	}
	return false
}

// CoversColumn keeps testigo's original meaning: some constraint on table
// (any table when table is "") leads with column. Prefer UniqueOn, which also
// understands composite and partial uniqueness.
func (i Infra) CoversColumn(table, column string) (Constraint, bool) {
	for _, c := range i.Constraints {
		if table != "" && c.Table != table {
			continue
		}
		if len(c.Columns) > 0 && c.Columns[0] == column {
			return c, true
		}
	}
	return Constraint{}, false
}

// Coverage is how a column is protected against duplicates.
type Coverage struct {
	Constraint Constraint `json:"constraint"`
	// Scope lists the other columns of a composite constraint: the value is
	// unique per combination of them, which is how a multi-tenant table scopes
	// an idempotency key.
	Scope   []string `json:"scope,omitempty"`
	Partial bool     `json:"partial,omitempty"`
}

// UniqueOn reports the strongest uniqueness guarantee on table.column: a
// single-column constraint beats a composite one, and a full one beats a
// partial index.
func (i Infra) UniqueOn(table, column string) (Coverage, bool) {
	var best Coverage
	found := false
	for _, c := range i.Constraints {
		if !c.Unique() || c.Table != table || !contains(c.Columns, column) {
			continue
		}
		cov := Coverage{Constraint: c, Partial: c.Where != ""}
		for _, other := range c.Columns {
			if other != column {
				cov.Scope = append(cov.Scope, other)
			}
		}
		if !found || stronger(cov, best) {
			best, found = cov, true
		}
	}
	return best, found
}

func stronger(a, b Coverage) bool {
	if a.Partial != b.Partial {
		return !a.Partial
	}
	return len(a.Scope) < len(b.Scope)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type TenantScheme struct {
	Column string
	Tables []string
}

// TenantScheme finds a column that leads composite uniqueness on two or more
// tables: the shape of a shared-table multi-tenant schema.
func (i Infra) TenantScheme() (TenantScheme, bool) {
	tablesByColumn := map[string]map[string]bool{}
	for _, c := range i.Constraints {
		if len(c.Columns) < 2 {
			continue
		}
		if c.Kind != KindUniqueIndex && c.Kind != KindUniqueConstraint {
			continue
		}
		leading := c.Columns[0]
		if tablesByColumn[leading] == nil {
			tablesByColumn[leading] = map[string]bool{}
		}
		tablesByColumn[leading][c.Table] = true
	}

	var candidates []string
	for col, tables := range tablesByColumn {
		if len(tables) >= 2 {
			candidates = append(candidates, col)
		}
	}
	if len(candidates) == 0 {
		return TenantScheme{}, false
	}
	sort.Slice(candidates, func(a, b int) bool {
		ca, cb := len(tablesByColumn[candidates[a]]), len(tablesByColumn[candidates[b]])
		if ca != cb {
			return ca > cb
		}
		return candidates[a] < candidates[b]
	})

	col := candidates[0]
	var tables []string
	for t := range tablesByColumn[col] {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	return TenantScheme{Column: col, Tables: tables}, true
}
