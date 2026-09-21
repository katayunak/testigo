package flowEntity

import "fmt"

type CodeRef struct {
	Pkg    string `json:"pkg"`
	Symbol string `json:"symbol"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

func (c CodeRef) ID() string { return c.Pkg + "#" + c.Symbol }

func (c CodeRef) String() string { return fmt.Sprintf("%s (%s:%d)", c.ID(), c.File, c.Line) }

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

	TableWrites []TableWrites `json:"table_writes,omitempty"`

	HashChains []HashChain `json:"hash_chains,omitempty"`

	IdempotencyKeys Candidates `json:"idempotency_keys,omitempty"`
	MoneyTypes      Candidates `json:"money_types,omitempty"`

	Entities map[string]string `json:"entities,omitempty"`

	GeneratedFiles    int `json:"generated_files,omitempty"`
	GeneratedFindings int `json:"generated_findings,omitempty"`

	Docs []Doc `json:"docs,omitempty"`
}

func CodeRefOf(pkg, symbol, file string, line int) CodeRef {
	return CodeRef{Pkg: pkg, Symbol: symbol, File: file, Line: line}
}

func NewFlow(module string) *Flow {
	return &Flow{SchemaVersion: SchemaVersion, Module: module, Nodes: map[string]*Node{}}
}

type EntryPoint struct {
	Pkg    string `json:"pkg"`
	Symbol string `json:"symbol"`
	Label  string `json:"label,omitempty"`
}

const SchemaVersion = 4

type Finding struct {
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	Ref      CodeRef  `json:"ref"`
	Line     int      `json:"line"`
}

type HashChain struct {
	Type   string  `json:"type"`
	Method CodeRef `json:"method"`
}

type StateMachine struct {
	Type   string       `json:"type"`
	Field  string       `json:"field,omitempty"`
	States []string     `json:"states"`
	Writes []StateWrite `json:"writes"`

	NeverAssigned []string `json:"never_assigned,omitempty"`
}

type StateWrite struct {
	In   CodeRef `json:"in"`
	To   string  `json:"to"`
	Line int     `json:"line"`
	InTx bool    `json:"in_tx,omitempty"`
}

type Severity string

const (
	SevCritical Severity = "critical"
	SevHigh     Severity = "high"
	SevMedium   Severity = "medium"
	SevInfo     Severity = "info"
)
