package flowEntity

// Infra is what testigo found about the environment the code runs in.
//
// A payment flow is not only its Go code. Whether two concurrent requests can
// both insert the same idempotency key depends on a unique index in a migration
// file. Whether events can arrive out of order depends on how many partitions
// the topic has. Neither fact is in the source, and both change what a correct
// test looks like.
//
// So testigo reads the migrations, the compose file and the config. This is
// cheap, deterministic, and it answers questions that would otherwise cost an
// agent round trip — or worse, get guessed.
type Infra struct {
	// Constraints are uniqueness rules found in migrations.
	//
	// The most valuable thing in this struct. Round one asks whether idempotency
	// uniqueness is enforced by a database constraint or by application code,
	// and the two behave completely differently under concurrency. If a
	// migration says UNIQUE on the key column, that is PROOF, and the question
	// does not need to be asked at all.
	Constraints []Constraint `json:"constraints,omitempty"`

	// Services are containers the repository expects to exist.
	Services []Service `json:"services,omitempty"`

	// Topics are brokers subjects found in configuration.
	Topics []Topic `json:"topics,omitempty"`

	// Tables are the shapes the migrations create.
	//
	// A test that inserts a row has to satisfy the schema, and an agent that has
	// not been told the schema does one of two things: it opens every migration
	// file, which is the single most expensive thing it can do, or it invents
	// columns and the test does not compile. Handing it the shape is cheaper
	// than either.
	Tables []Table `json:"tables,omitempty"`

	// ForeignKeys decide the order rows must be inserted in, and what a delete
	// does to the rows below it.
	ForeignKeys []ForeignKey `json:"foreign_keys,omitempty"`

	// Checks are invariants the database enforces itself.
	//
	// These are worth more than their size suggests. CHECK (balance >= 0) is a
	// property test somebody already wrote, in a language the database will
	// enforce for us, and a CHECK ... IN (...) list is the set of states the
	// database will accept — which can be compared against the states the Go
	// code actually writes. Where those two disagree, one of them is a bug.
	Checks []Check `json:"checks,omitempty"`

	// MigrationDirs are where the migrations were found, so a person can check.
	MigrationDirs []string `json:"migration_dirs,omitempty"`

	// MigrationFiles is how many were read. Zero with a non-empty MigrationDirs
	// means the directory exists and nothing in it parsed, which is a different
	// problem from having no migrations.
	MigrationFiles int `json:"migration_files,omitempty"`
}

// Table is one CREATE TABLE, as the migrations declare it.
type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns,omitempty"`
	File    string   `json:"file"`
	Line    int      `json:"line"`
}

// Column is one column of a table.
//
// NotNull and Default are here because they decide what a test fixture has to
// provide. A NOT NULL column with no default must be set by every insert; a
// column with a default is one the code may legitimately never mention.
type Column struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"not_null,omitempty"`
	Default    string `json:"default,omitempty"`
	PrimaryKey bool   `json:"primary_key,omitempty"`
}

// ForeignKey is a reference from one table to another.
type ForeignKey struct {
	Table      string   `json:"table"`
	Columns    []string `json:"columns"`
	RefTable   string   `json:"ref_table"`
	RefColumns []string `json:"ref_columns,omitempty"`
	OnDelete   string   `json:"on_delete,omitempty"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
}

// Check is a CHECK constraint.
//
// Values is filled when the check is an IN list or an enum, because that is the
// database's own opinion about which states exist, and it can be compared with
// the constants the Go code declares.
type Check struct {
	Table  string   `json:"table"`
	Column string   `json:"column,omitempty"`
	Expr   string   `json:"expr"`
	Values []string `json:"values,omitempty"`
	File   string   `json:"file"`
	Line   int      `json:"line"`
}

// ColumnsOf returns the declared shape of a table, or nil.
func (i Infra) ColumnsOf(table string) []Column {
	for _, t := range i.Tables {
		if t.Name == table {
			return t.Columns
		}
	}
	return nil
}

// HasTable reports whether the migrations declare a table with this name.
func (i Infra) HasTable(table string) bool { return i.ColumnsOf(table) != nil }

// Empty reports whether anything at all was read. Used to decide whether to
// render the migration section, because a heading with nothing under it makes a
// reader wonder what went wrong.
func (i Infra) Empty() bool {
	return len(i.Tables) == 0 && len(i.Constraints) == 0 &&
		len(i.Checks) == 0 && len(i.ForeignKeys) == 0
}

// Constraint is a uniqueness rule the database will enforce.
type Constraint struct {
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
	Kind    string   `json:"kind"` // unique_index | unique_constraint | primary_key
	File    string   `json:"file"`
	Line    int      `json:"line"`
}

// CoversColumn reports whether some constraint makes a column unique, alone or
// as the first column of a composite key.
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

// Service is a container the repository depends on.
type Service struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	Kind  string `json:"kind"` // postgres | redis | kafka | nats | other
	File  string `json:"file"`
}

// Topic is a broker subject, with the settings that change test design.
//
// Partitions matter because ordering is only guaranteed WITHIN a partition. A
// topic with one partition delivers a payment's events in order; a topic with
// twelve does not, unless every event for one payment shares a key. That single
// number decides whether the out-of-order test is worth generating.
type Topic struct {
	Name       string `json:"name"`
	Partitions int    `json:"partitions,omitempty"`
	Replicas   int    `json:"replicas,omitempty"`
	File       string `json:"file"`
}
