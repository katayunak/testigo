package models

type Phase string

// PhaseScanningTheFlow is step one: everything testigo can prove from source, NO AGENTS INVOLVED.
// Using GO's SSA (Static Single Assignment) and AST (Abstract Syntax Tree)
const (
	PhaseScanningTheFlow Phase = "ScanningTheFlow"
)
