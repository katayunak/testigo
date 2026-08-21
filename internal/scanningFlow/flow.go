package scanningFlow

import "github.com/katayunak/testigo/internal/codeRef"

// Flow contains all the information found during ScanningTheFlow
// It is also the data saved in the sidecar file
type Flow struct {
	SchemaVersion int              `json:"schemaversion"`
	Module        string           `json:"module"`
	GeneratedAt   string           `json:"generated_at"`
	Entries       []EntryPoint     `json:"entries"`
	Nodes         map[string]*Node `json:"nodes"`
	Seams         []Seam           `json:"seams"`
	Machines      []StateMachine   `json:"machines"`
	Findings      []Finding        `json:"findings"`
	Orphans       []*Node          `json:"orphans,omitempty"`
}

func NewFlow(module string) *Flow {
	return &Flow{SchemaVersion: SchemaVersion, Module: module, Nodes: map[string]*Node{}}
}

// EntryPoint is a function where a payment flow starts
// A payment can start in several ways: an API request, a provider webhook,
// a reconciliation job, or a queue message. They may all enter the same
// state machine, so we need to keep ALL of them.
type EntryPoint struct {
	Pkg    string `json:"pkg"`
	Symbol string `json:"symbol"`
	Label  string `json:"label,omitempty"`
}

// SchemaVersion is a handshake between the program that WROTE .testigo/flow.json
// and the program that READS it
const SchemaVersion = 2

// Finding is a problem or risk found directly from the source code.
// Every finding must be reproducible from the source. A developer should be
// able to look at the code and verify why the finding was reported.
type Finding struct {
	ID       string          `json:"id"` // stable code, e.g. "MONEY-FLOAT"
	Severity Severity        `json:"severity"`
	Title    string          `json:"title"`
	Detail   string          `json:"detail"`
	Ref      codeRef.CodeRef `json:"ref"`
	Line     int             `json:"line"`
}

type StateMachine struct {
	Type     string       `json:"type"`            // fully qualified named type
	Field    string       `json:"field,omitempty"` // struct field holding it
	States   []string     `json:"states"`          // declared constants, sorted
	Writes   []StateWrite `json:"writes"`          // every assignment site
	Terminal []string     `json:"terminal,omitempty"`
}

// StateWrite is one place the status is set.
type StateWrite struct {
	In   codeRef.CodeRef `json:"in"`
	To   string          `json:"to"` // constant name, or "<dynamic>" when unknown
	Line int             `json:"line"`
	InTx bool            `json:"in_tx,omitempty"`
}

// Severity tells us how serious a finding is
type Severity string

const (
	SevCritical Severity = "critical"
	SevHigh     Severity = "high"
	SevMedium   Severity = "medium"
	SevInfo     Severity = "info"
)
