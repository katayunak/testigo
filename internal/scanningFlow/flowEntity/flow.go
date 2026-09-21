package flowEntity

import "github.com/katayunak/testigo/internal/codeRef"

type Flow struct {
	SchemaVersion int              `json:"schema_version"`
	Module        string           `json:"module"`
	GeneratedAt   string           `json:"generated_at"`
	Entries       []EntryPoint     `json:"entries"`
	Nodes         map[string]*Node `json:"nodes"`
	Seams         []Seam           `json:"seams"`
	States        []StateMachine   `json:"states"`
	Findings      []Finding        `json:"findings"`
	Infra         Infra            `json:"infra,omitempty"`

	IdempotencyKeys Candidates `json:"idempotency_keys,omitempty"`
	MoneyTypes      Candidates `json:"money_types,omitempty"`

	Entities map[string]string `json:"entities,omitempty"`

	GeneratedFiles    int `json:"generated_files,omitempty"`
	GeneratedFindings int `json:"generated_findings,omitempty"`

	Docs    []Doc   `json:"docs,omitempty"`
	Orphans []*Node `json:"orphans,omitempty"`
}

func CodeRefOf(pkg, symbol, file string, line int) codeRef.CodeRef {
	return codeRef.CodeRef{Pkg: pkg, Symbol: symbol, File: file, Line: line}
}

func NewFlow(module string) *Flow {
	return &Flow{SchemaVersion: SchemaVersion, Module: module, Nodes: map[string]*Node{}}
}

type EntryPoint struct {
	Pkg    string `json:"pkg"`
	Symbol string `json:"symbol"`
	Label  string `json:"label,omitempty"`
}

const SchemaVersion = 3

type Finding struct {
	ID       string          `json:"id"`
	Severity Severity        `json:"severity"`
	Title    string          `json:"title"`
	Detail   string          `json:"detail"`
	Ref      codeRef.CodeRef `json:"ref"`
	Line     int             `json:"line"`
}

type StateMachine struct {
	Type   string       `json:"type"`
	Field  string       `json:"field,omitempty"`
	States []string     `json:"states"`
	Writes []StateWrite `json:"writes"`

	NeverAssigned []string `json:"never_assigned,omitempty"`
}

type StateWrite struct {
	In   codeRef.CodeRef `json:"in"`
	To   string          `json:"to"`
	Line int             `json:"line"`
	InTx bool            `json:"in_tx,omitempty"`
}

type Severity string

const (
	SevCritical Severity = "critical"
	SevHigh     Severity = "high"
	SevMedium   Severity = "medium"
	SevInfo     Severity = "info"
)
