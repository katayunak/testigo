package fact

import "sort"

type Severity string

const (
	SevCritical Severity = "critical"
	SevHigh     Severity = "high"
	SevMedium   Severity = "medium"
	SevLow      Severity = "low"
	SevInfo     Severity = "info"
)

func (s Severity) Rank() int {
	switch s {
	case SevCritical:
		return 0
	case SevHigh:
		return 1
	case SevMedium:
		return 2
	case SevLow:
		return 3
	}
	return 4
}

// Basis says how a finding was reached. The distinction matters more than the
// severity: a proved finding is wrong only if the analyser has a bug, a
// heuristic one is wrong whenever the code is named unusually.
type Basis string

const (
	Proved    Basis = "proved"
	Heuristic Basis = "heuristic"
)

// Finding is evidence of a risk, not proof of a bug. Ref is the enclosing
// declaration, Line the site inside it, Related the other places the claim
// depends on (the Begin of a leaked transaction, the constraint that is
// missing a column).
type Finding struct {
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Basis    Basis    `json:"basis,omitempty"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	Ref      CodeRef  `json:"ref"`
	Line     int      `json:"line"`
	Related  []Pos    `json:"related,omitempty"`
}

func (f Finding) At() Pos { return Pos{File: f.Ref.File, Line: f.Line} }

func SortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Severity.Rank() != b.Severity.Rank() {
			return a.Severity.Rank() < b.Severity.Rank()
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Ref.File != b.Ref.File {
			return a.Ref.File < b.Ref.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Ref.ID() != b.Ref.ID() {
			return a.Ref.ID() < b.Ref.ID()
		}
		return a.Title < b.Title
	})
}
