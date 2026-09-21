package flowEntity

type Infra struct {
	Constraints []Constraint `json:"constraints,omitempty"`

	Services []Service `json:"services,omitempty"`

	Topics []Topic `json:"topics,omitempty"`

	Tables []Table `json:"tables,omitempty"`

	ForeignKeys []ForeignKey `json:"foreign_keys,omitempty"`

	Checks []Check `json:"checks,omitempty"`

	MigrationDirs []string `json:"migration_dirs,omitempty"`

	MigrationFiles int `json:"migration_files,omitempty"`
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
	Column string   `json:"column,omitempty"`
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

type Service struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	Kind  string `json:"kind"`
	File  string `json:"file"`
}

type Topic struct {
	Name       string `json:"name"`
	Partitions int    `json:"partitions,omitempty"`
	Replicas   int    `json:"replicas,omitempty"`
	File       string `json:"file"`
}
