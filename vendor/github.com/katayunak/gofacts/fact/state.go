package fact

// StateMachine is a named string or integer type the module treats as a
// lifecycle: persisted in a struct field and moved by more than one function,
// or named like a status and actually assigned.
type StateMachine struct {
	Type   string       `json:"type"`
	Field  string       `json:"field,omitempty"`
	Fields []string     `json:"fields,omitempty"`
	States []string     `json:"states"`
	Writes []StateWrite `json:"writes"`
	Decl   Pos          `json:"decl,omitempty"`

	NeverAssigned []string `json:"never_assigned,omitempty"`
}

// StateWrite is one place a state is produced. To is the constant name, or
// "<dynamic>" when the value is computed. From lists the states the write is
// guarded by (`if p.Status == A { p.Status = B }`), when the guard is visible
// in the same function; empty means the source state is not known here.
type StateWrite struct {
	In   CodeRef  `json:"in"`
	To   string   `json:"to"`
	From []string `json:"from,omitempty"`
	Line int      `json:"line"`
	Via  WriteVia `json:"via,omitempty"`
	InTx bool     `json:"in_tx,omitempty"`
}

const DynamicState = "<dynamic>"

type WriteVia string

const (
	ViaAssign   WriteVia = "assign"
	ViaLiteral  WriteVia = "literal"
	ViaReturn   WriteVia = "return"
	ViaArgument WriteVia = "argument"
)

// Transitions lists every distinct (from, to) pair with a known source state.
func (m StateMachine) Transitions() [][2]string {
	seen := map[[2]string]bool{}
	var out [][2]string
	for _, w := range m.Writes {
		for _, from := range w.From {
			pair := [2]string{from, w.To}
			if !seen[pair] {
				seen[pair] = true
				out = append(out, pair)
			}
		}
	}
	return out
}
