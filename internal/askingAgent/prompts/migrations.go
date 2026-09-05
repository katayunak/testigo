package prompts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// Migrations hands the agent the database schema, so it never opens a migration
// file.
//
// This is the cheapest information testigo owns. A migration is the only place
// where the shape of the data is written down once, in full, by a person who
// meant it — and reading it costs nothing, because phase 1 already did. An agent
// that has NOT been told the schema does one of two things when asked to write
// an integration test: it opens every file under db/migrations, which on a real
// service is tens of thousands of tokens and the single most expensive thing it
// can do, or it invents column names and the test does not compile.
//
// So this goes in PREAMBLE.md, written once, read once, and every question is
// answered in its light.
//
// What is deliberately NOT here: every table in the database. A real service has
// dozens and the flow touches five. The rest are listed by name only, with a
// pointer to flow.json, because a name is enough for an agent to ask for more
// and a full column list is not worth what it costs.
func Migrations(f *flowEntity.Flow) string {
	in := f.Infra
	if in.Empty() {
		if len(in.MigrationDirs) > 0 {
			return fmt.Sprintf("\n## The database\n\nMigrations were found in %s but nothing in them parsed. Treat the\nschema as unknown, and say so rather than guessing column names.\n",
				strings.Join(in.MigrationDirs, ", "))
		}
		// Saying this plainly stops the agent going to look for something that
		// is not there, which is a whole turn.
		return "\n## The database\n\nNo migration files were found in this repository. Do not guess a schema:\nif a test needs one, say what it needs and why.\n"
	}

	var b strings.Builder
	fmt.Fprintf(&b, `
## The database, from the migrations

Read from %s (%d file(s)). This is the complete schema testigo could find, so
do NOT open the migration files: everything below is already here, and reading
them again costs more than every question in this pack put together.

`, strings.Join(in.MigrationDirs, ", "), in.MigrationFiles)

	// Her snippet, unchanged. Uniqueness comes first because it is the one fact
	// that decides whether a duplicate request can be inserted twice, which is
	// the most expensive bug this tool exists to find.
	if len(f.Infra.Constraints) > 0 {
		b.WriteString("UNIQUENESS, from the migrations\n")
		for _, c := range f.Infra.Constraints {
			fmt.Fprintf(&b, "  %-18s %s(%s)   %s:%d\n",
				c.Kind, c.Table, strings.Join(c.Columns, ", "), c.File, c.Line)
		}
		b.WriteString("\n")
	}

	keep, rest := relevantTables(f)

	if len(keep) > 0 {
		b.WriteString("TABLES the flow touches\n\n")
		for _, t := range keep {
			fmt.Fprintf(&b, "  %s   %s:%d\n", t.Name, t.File, t.Line)
			for _, c := range t.Columns {
				fmt.Fprintf(&b, "      %-22s %-26s %s\n", c.Name, c.Type, columnNote(c))
			}
			b.WriteString("\n")
		}
	}
	if len(rest) > 0 {
		names := make([]string, 0, len(rest))
		for _, t := range rest {
			names = append(names, t.Name)
		}
		fmt.Fprintf(&b, "OTHER TABLES, names only (full columns in testigo/flow.json)\n  %s\n\n",
			strings.Join(names, ", "))
	}

	if len(in.Checks) > 0 {
		b.WriteString(`RULES THE DATABASE ENFORCES ITSELF

Each one is an invariant somebody already wrote. A test that violates one gets
an error from the database rather than a wrong answer, so these are the cheapest
assertions available — and a rule here that the Go code does not also enforce is
worth pointing out.

`)
		for _, c := range in.Checks {
			where := c.Table
			if where == "" {
				where = "(type)"
			}
			fmt.Fprintf(&b, "  %-18s %s   %s:%d\n", where, c.Expr, c.File, c.Line)
			if len(c.Values) > 0 {
				fmt.Fprintf(&b, "      accepts only: %s\n", strings.Join(c.Values, ", "))
			}
		}
		b.WriteString("\n")
	}

	if len(in.ForeignKeys) > 0 {
		b.WriteString(`REFERENCES — the order rows must be inserted in

A fixture that inserts a child before its parent fails on the constraint, not on
the thing being tested. ON DELETE also says what a cleanup between test cases
actually removes.

`)
		for _, fk := range in.ForeignKeys {
			od := fk.OnDelete
			if od == "" {
				od = "(no action)"
			}
			fmt.Fprintf(&b, "  %s(%s) -> %s(%s)   on delete %s   %s:%d\n",
				fk.Table, strings.Join(fk.Columns, ", "),
				fk.RefTable, strings.Join(orNoneList(fk.RefColumns), ", "),
				od, fk.File, fk.Line)
		}
		b.WriteString("\n")
	}

	if s := stateMismatch(f); s != "" {
		b.WriteString(s)
	}

	b.WriteString(`WHAT THIS IS FOR

An integration test here means: start the real database, apply these migrations,
insert rows that satisfy the constraints above, run the flow, assert on what the
tables hold afterwards. Use the exact table and column names printed here. If a
test needs a column that is not listed, say so instead of inventing one — a
missing column means testigo failed to read a migration, and that is worth
knowing.
`)
	return b.String()
}

func columnNote(c flowEntity.Column) string {
	var parts []string
	if c.PrimaryKey {
		parts = append(parts, "primary key")
	}
	switch {
	case c.NotNull && c.Default == "":
		// The one fact a fixture author cannot do without.
		parts = append(parts, "NOT NULL, no default - every insert must set it")
	case c.NotNull:
		parts = append(parts, "not null, default "+c.Default)
	case c.Default != "":
		parts = append(parts, "default "+c.Default)
	}
	return strings.Join(parts, ", ")
}

// relevantTables splits the schema into the tables this flow touches and the
// rest.
//
// The rule is deliberately generous — a table is kept if anything in the flow
// mentions it — because a missing table costs the agent a turn to ask for, and a
// spare table costs eight lines. Wrong in the cheap direction.
func relevantTables(f *flowEntity.Flow) (keep, rest []flowEntity.Table) {
	want := map[string]bool{}

	// Tables that carry a uniqueness rule are load-bearing by definition: that
	// is what phase 1 looked at to decide whether duplicates are possible.
	for _, c := range f.Infra.Constraints {
		want[c.Table] = true
	}
	for _, c := range f.Infra.Checks {
		if c.Table != "" {
			want[c.Table] = true
		}
	}

	// Tables named like the entities the flow moves. "Order" -> orders, order.
	for entity := range f.Entities {
		for _, n := range tableNamesFor(entity) {
			want[n] = true
		}
	}
	// And like the types that carry money.
	for _, c := range f.MoneyTypes {
		for _, n := range tableNamesFor(c.Owner) {
			want[n] = true
		}
	}

	// A table holding a column that phase 1 ranked as an idempotency key is part
	// of the flow whatever it is called.
	keyed := map[string]bool{}
	for _, c := range f.IdempotencyKeys {
		keyed[strings.ToLower(snake(c.Name))] = true
	}

	for _, t := range f.Infra.Tables {
		hit := want[t.Name] || want[strings.ToLower(t.Name)]
		if !hit {
			for _, col := range t.Columns {
				if keyed[strings.ToLower(col.Name)] {
					hit = true
					break
				}
			}
		}
		if hit {
			keep = append(keep, t)
		} else {
			rest = append(rest, t)
		}
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].Name < keep[j].Name })
	sort.Slice(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })
	return keep, rest
}

// tableNamesFor guesses what a Go type is called in the database. Go structs are
// CamelCase and singular, tables are usually snake_case and plural, and nobody
// writes that mapping down.
func tableNamesFor(goType string) []string {
	if i := strings.LastIndex(goType, "."); i >= 0 {
		goType = goType[i+1:]
	}
	s := snake(goType)
	if s == "" {
		return nil
	}
	out := []string{s, s + "s"}
	switch {
	case strings.HasSuffix(s, "y"):
		out = append(out, s[:len(s)-1]+"ies")
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "ch"):
		out = append(out, s+"es")
	}
	return out
}

func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + 32)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// stateMismatch compares the states the database accepts with the states the Go
// code declares.
//
// This is free and it finds real bugs. Two lists that were written months apart
// and are meant to be the same thing rarely are: a state added to Go and not to
// the migration makes every write of it fail in production, and a state the
// database still allows that Go dropped is a row nothing can handle.
func stateMismatch(f *flowEntity.Flow) string {
	var out strings.Builder
	// A schema usually states its state list twice — a CREATE TYPE ... AS ENUM
	// and a CHECK ... IN (...) that repeats it. Both produce the same finding,
	// and printing it twice makes a reader wonder whether they are different.
	said := map[string]bool{}
	for _, c := range f.Infra.Checks {
		if len(c.Values) == 0 {
			continue
		}
		db := map[string]bool{}
		for _, v := range c.Values {
			db[strings.ToUpper(v)] = true
		}
		for _, m := range f.States {
			var onlyGo, onlyDB []string
			goSide := map[string]bool{}
			for _, s := range m.States {
				goSide[strings.ToUpper(s)] = true
			}
			// Only compare lists that clearly describe the same thing. Two
			// unrelated sets overlapping in nothing is not a mismatch, it is two
			// different enums.
			shared := 0
			for s := range goSide {
				if db[s] {
					shared++
				}
			}
			if shared == 0 {
				continue
			}
			for _, s := range m.States {
				if !db[strings.ToUpper(s)] {
					onlyGo = append(onlyGo, s)
				}
			}
			for _, v := range c.Values {
				if !goSide[strings.ToUpper(v)] {
					onlyDB = append(onlyDB, v)
				}
			}
			if len(onlyGo) == 0 && len(onlyDB) == 0 {
				continue
			}
			key := m.Type + "|" + strings.Join(onlyGo, ",") + "|" + strings.Join(onlyDB, ",")
			if said[key] {
				continue
			}
			said[key] = true
			fmt.Fprintf(&out, "MISMATCH: %s and the database do not agree on the state list\n", m.Type)
			if len(onlyGo) > 0 {
				fmt.Fprintf(&out, "  only in Go:       %s\n      writing one of these fails the constraint at %s:%d\n",
					strings.Join(onlyGo, ", "), c.File, c.Line)
			}
			if len(onlyDB) > 0 {
				fmt.Fprintf(&out, "  only in the database: %s\n      a row can hold one of these and no Go code handles it\n",
					strings.Join(onlyDB, ", "))
			}
			out.WriteString("\n")
		}
	}
	return out.String()
}

func orNoneList(s []string) []string {
	if len(s) == 0 {
		return []string{"?"}
	}
	return s
}
