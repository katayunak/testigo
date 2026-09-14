package flowEntity

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

type TableWrites struct {
	Table   string       `json:"table"`
	Pattern WritePattern `json:"pattern"`

	Inserts int `json:"inserts,omitempty"`
	Updates int `json:"updates,omitempty"`
	Deletes int `json:"deletes,omitempty"`

	InDatabaseMath bool `json:"in_database_math,omitempty"`

	ReadModifyWrite bool `json:"read_modify_write,omitempty"`

	Upsert bool `json:"upsert,omitempty"`

	RowLock bool `json:"row_lock,omitempty"`

	Where []CodeRef `json:"where,omitempty"`
}

func (f *Flow) WritesTo(table string) (TableWrites, bool) {
	for _, w := range f.TableWrites {
		if w.Table == table {
			return w, true
		}
	}
	return TableWrites{}, false
}

func (f *Flow) HasWritePattern(p WritePattern) bool {
	for _, w := range f.TableWrites {
		if w.Pattern == p {
			return true
		}
	}
	return false
}
