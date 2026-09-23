package flowEntity

import "sort"

type Infra struct {
	Constraints []Constraint `json:"constraints,omitempty"`

	Tables []Table `json:"tables,omitempty"`

	ForeignKeys []ForeignKey `json:"foreign_keys,omitempty"`

	Checks []Check `json:"checks,omitempty"`

	MigrationDirs []string `json:"migration_dirs,omitempty"`

	MigrationFiles int `json:"migration_files,omitempty"`

	MigrationTool string `json:"migration_tool,omitempty"`

	MigrationsTotal int `json:"migrations_total,omitempty"`

	MigrationsWithDown int `json:"migrations_with_down,omitempty"`

	SchemaSources []string `json:"schema_sources,omitempty"`
}

func (i Infra) SchemaKnown() bool { return len(i.Tables) > 0 || len(i.Constraints) > 0 }

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

func (i Infra) ColumnsOf(table string) []Column {
	for _, t := range i.Tables {
		if t.Name == table {
			return t.Columns
		}
	}
	return nil
}

func (i Infra) HasTable(table string) bool { return i.ColumnsOf(table) != nil }

func (i Infra) Empty() bool {
	return len(i.Tables) == 0 && len(i.Constraints) == 0 &&
		len(i.Checks) == 0 && len(i.ForeignKeys) == 0
}

type Constraint struct {
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
	Kind    string   `json:"kind"`
	File    string   `json:"file"`
	Line    int      `json:"line"`
}

type TenantScheme struct {
	Column string
	Tables []string
}

func (i Infra) TenantScheme() (TenantScheme, bool) {
	tablesByColumn := map[string]map[string]bool{}
	for _, c := range i.Constraints {
		if len(c.Columns) < 2 {
			continue
		}
		if c.Kind != "unique_index" && c.Kind != "unique_constraint" {
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
	sort.Slice(candidates, func(a, b int) bool {
		ca, cb := len(tablesByColumn[candidates[a]]), len(tablesByColumn[candidates[b]])
		if ca != cb {
			return ca > cb
		}
		return candidates[a] < candidates[b]
	})
	if len(candidates) == 0 {
		return TenantScheme{}, false
	}

	col := candidates[0]
	var tables []string
	for t := range tablesByColumn[col] {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	return TenantScheme{Column: col, Tables: tables}, true
}

func (i Infra) CoversColumn(table, column string) (Constraint, bool) {
	for _, c := range i.Constraints {
		if table != "" && c.Table != table {
			continue
		}
		for n, col := range c.Columns {
			if col == column && n == 0 {
				return c, true
			}
		}
	}
	return Constraint{}, false
}
