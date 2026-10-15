package sqlschema

import (
	"strings"

	"github.com/katayunak/gofacts/fact"
)

// Schema is the state DDL leaves behind. Apply statements in the order the
// database would run them; Infra fills the schema part of a fact.Infra.
type Schema struct {
	tables      []*tableState
	constraints []fact.Constraint
	foreignKeys []fact.ForeignKey
	checks      []fact.Check
}

type tableState struct {
	fact.Table
}

func (s *Schema) table(name string) *tableState {
	for _, t := range s.tables {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// ApplySQL parses sql and applies it.
func (s *Schema) ApplySQL(sql, file string) {
	for _, st := range Parse(sql, file) {
		s.Apply(st)
	}
}

func (s *Schema) Apply(st Statement) {
	switch st.Kind {
	case KindCreateTable:
		if s.table(st.Table) != nil {
			if st.IfNotExists {
				return
			}
			s.dropTable(st.Table)
		}
		s.tables = append(s.tables, &tableState{fact.Table{
			Name: st.Table, Columns: st.Def.Columns, File: st.File, Line: st.Line,
		}})
		s.addDef(*st.Def, st.File, st.Line)

	case KindCreateIndex:
		if !st.Unique || len(st.Columns) == 0 {
			return
		}
		s.addConstraint(fact.Constraint{
			Table: st.Table, Columns: st.Columns, Kind: fact.KindUniqueIndex,
			Name: st.Name, Where: st.Where, File: st.File, Line: st.Line,
		})

	case KindCreateEnum:
		s.checks = append(s.checks, fact.Check{
			Expr: "enum type " + st.Name, Values: st.Values, File: st.File, Line: st.Line,
		})

	case KindAlterTable:
		for _, a := range st.Actions {
			s.applyAction(st, a)
		}

	case KindRenameIndex:
		// Postgres renames the constraint an index backs along with it.
		for i := range s.constraints {
			if s.constraints[i].Name == st.Name {
				s.constraints[i].Name = st.To
			}
		}

	case KindDropIndex:
		drop := set(st.Names)
		s.constraints = filter(s.constraints, func(c fact.Constraint) bool {
			return !(c.Kind == fact.KindUniqueIndex && drop[c.Name])
		})

	case KindDropTable:
		for _, name := range st.Names {
			s.dropTable(name)
		}
	}
}

func (s *Schema) applyAction(st Statement, a Action) {
	t := s.table(st.Table)
	switch a.Op {
	case AddColumn:
		if t != nil && !hasColumn(t.Columns, a.Column.Name) {
			t.Columns = append(t.Columns, a.Column)
		}
		s.addDef(a.Def, st.File, st.Line)

	case AddConstraint:
		s.addDef(a.Def, st.File, st.Line)

	case DropConstraint:
		s.constraints = filter(s.constraints, func(c fact.Constraint) bool {
			return !(c.Table == st.Table && c.Name == a.Name)
		})

	case DropColumn:
		if t != nil {
			t.Columns = filter(t.Columns, func(c fact.Column) bool { return c.Name != a.Name })
		}
		// Postgres drops every index and constraint that uses the column.
		s.constraints = filter(s.constraints, func(c fact.Constraint) bool {
			return !(c.Table == st.Table && containsStr(c.Columns, a.Name))
		})
		s.foreignKeys = filter(s.foreignKeys, func(fk fact.ForeignKey) bool {
			return !(fk.Table == st.Table && containsStr(fk.Columns, a.Name))
		})

	case RenameColumn:
		if t != nil {
			for i := range t.Columns {
				if t.Columns[i].Name == a.Name {
					t.Columns[i].Name = a.To
				}
			}
		}
		for i := range s.constraints {
			if s.constraints[i].Table == st.Table {
				s.constraints[i].Columns = replaced(s.constraints[i].Columns, a.Name, a.To)
			}
		}
		for i := range s.foreignKeys {
			if s.foreignKeys[i].Table == st.Table {
				s.foreignKeys[i].Columns = replaced(s.foreignKeys[i].Columns, a.Name, a.To)
			}
		}

	case RenameCons:
		for i := range s.constraints {
			if s.constraints[i].Table == st.Table && s.constraints[i].Name == a.Name {
				s.constraints[i].Name = a.To
			}
		}

	case RenameTable:
		if t != nil {
			t.Name = a.To
		}
		for i := range s.constraints {
			if s.constraints[i].Table == st.Table {
				s.constraints[i].Table = a.To
			}
		}
		for i := range s.foreignKeys {
			if s.foreignKeys[i].Table == st.Table {
				s.foreignKeys[i].Table = a.To
			}
			if s.foreignKeys[i].RefTable == st.Table {
				s.foreignKeys[i].RefTable = a.To
			}
		}
		for i := range s.checks {
			if s.checks[i].Table == st.Table {
				s.checks[i].Table = a.To
			}
		}

	case AlterColumn:
		if t == nil {
			return
		}
		for i := range t.Columns {
			if t.Columns[i].Name != a.Name {
				continue
			}
			switch {
			case strings.HasPrefix(a.Detail, "set not null"):
				t.Columns[i].NotNull = true
			case strings.HasPrefix(a.Detail, "drop not null"):
				t.Columns[i].NotNull = false
			case strings.HasPrefix(a.Detail, "type "), strings.HasPrefix(a.Detail, "set data type "):
				t.Columns[i].Type = strings.TrimPrefix(strings.TrimPrefix(a.Detail, "set data type "), "type ")
			}
		}
	}
}

func (s *Schema) addDef(d TableDef, file string, line int) {
	for _, c := range d.Constraints {
		c.File, c.Line = file, line
		s.addConstraint(c)
	}
	for _, fk := range d.ForeignKeys {
		fk.File, fk.Line = file, line
		s.foreignKeys = append(s.foreignKeys, fk)
	}
	for _, ch := range d.Checks {
		ch.File, ch.Line = file, line
		s.checks = append(s.checks, ch)
	}
}

func (s *Schema) addConstraint(c fact.Constraint) {
	if len(c.Columns) == 0 {
		return
	}
	for _, have := range s.constraints {
		if have.Table == c.Table && have.Kind == c.Kind && have.Where == c.Where &&
			strings.Join(have.Columns, ",") == strings.Join(c.Columns, ",") {
			return
		}
	}
	s.constraints = append(s.constraints, c)
}

func (s *Schema) dropTable(name string) {
	var kept []*tableState
	for _, t := range s.tables {
		if t.Name != name {
			kept = append(kept, t)
		}
	}
	s.tables = kept
	s.constraints = filter(s.constraints, func(c fact.Constraint) bool { return c.Table != name })
	s.foreignKeys = filter(s.foreignKeys, func(fk fact.ForeignKey) bool { return fk.Table != name })
	s.checks = filter(s.checks, func(c fact.Check) bool { return c.Table != name })
}

// Tables returns the tables in creation order with at least one column.
func (s *Schema) Tables() []fact.Table {
	var out []fact.Table
	for _, t := range s.tables {
		if len(t.Columns) > 0 {
			out = append(out, t.Table)
		}
	}
	return out
}

func (s *Schema) Constraints() []fact.Constraint {
	return append([]fact.Constraint(nil), s.constraints...)
}
func (s *Schema) ForeignKeys() []fact.ForeignKey {
	return append([]fact.ForeignKey(nil), s.foreignKeys...)
}
func (s *Schema) Checks() []fact.Check { return append([]fact.Check(nil), s.checks...) }

// Fill appends the schema to infra.
func (s *Schema) Fill(infra *fact.Infra) {
	infra.Tables = append(infra.Tables, s.Tables()...)
	infra.Constraints = append(infra.Constraints, s.constraints...)
	infra.ForeignKeys = append(infra.ForeignKeys, s.foreignKeys...)
	infra.Checks = append(infra.Checks, s.checks...)
}

func filter[T any](in []T, keep func(T) bool) []T {
	out := in[:0:0]
	for _, x := range in {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

func set(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hasColumn(cols []fact.Column, name string) bool {
	for _, c := range cols {
		if c.Name == name {
			return true
		}
	}
	return false
}

func replaced(list []string, from, to string) []string {
	out := make([]string, len(list))
	for i, x := range list {
		if x == from {
			x = to
		}
		out[i] = x
	}
	return out
}
