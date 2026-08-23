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

	// MigrationDirs are where the migrations were found, so a person can check.
	MigrationDirs []string `json:"migration_dirs,omitempty"`
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
