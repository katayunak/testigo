package fact

type WritePattern string

const (
	WriteUnknown          WritePattern = "unknown"
	WriteInsertOnly       WritePattern = "insert_only"
	WriteUpdateInPlace    WritePattern = "update_in_place"
	WriteUpdateInDatabase WritePattern = "update_in_database"
	WriteUpsert           WritePattern = "upsert"
)

func (w WritePattern) Human() string {
	switch w {
	case WriteInsertOnly:
		return "rows are only added, never changed"
	case WriteUpdateInPlace:
		return "a row is read, changed in Go, then written back"
	case WriteUpdateInDatabase:
		return "the new value is computed by the database itself"
	case WriteUpsert:
		return "insert that falls back to an update on conflict"
	}
	return "no writes were found"
}

func (w WritePattern) CanLoseUpdates() bool { return w == WriteUpdateInPlace }

// TableWrites summarises how the module writes one table, which decides how it
// can break: an update computed in Go can lose a concurrent update, an insert
// without a conflict clause can duplicate.
type TableWrites struct {
	Table   string       `json:"table"`
	Pattern WritePattern `json:"pattern"`

	Inserts int `json:"inserts,omitempty"`
	Updates int `json:"updates,omitempty"`
	Deletes int `json:"deletes,omitempty"`

	InDatabaseMath  bool `json:"in_database_math,omitempty"`
	ReadModifyWrite bool `json:"read_modify_write,omitempty"`
	Upsert          bool `json:"upsert,omitempty"`
	RowLock         bool `json:"row_lock,omitempty"`

	Where []CodeRef `json:"where,omitempty"`
}

func ClassifyWrites(w TableWrites) WritePattern {
	switch {
	case w.Upsert:
		return WriteUpsert
	case w.Updates == 0 && w.Inserts > 0:
		return WriteInsertOnly
	case w.InDatabaseMath && !w.ReadModifyWrite:
		return WriteUpdateInDatabase
	case w.Updates > 0:
		return WriteUpdateInPlace
	}
	return WriteUnknown
}
