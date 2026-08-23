package internal

type Phase string

// PhaseScanningTheFlow is step one: everything testigo can prove from source, NO AGENTS INVOLVED.
// Using GO's SSA (Static Single Assignment) and AST (Abstract Syntax Tree)
//
// PhaseAskingTheAgent is step two: the questions a compiler cannot answer.
// It runs in two rounds. Round one collects context — which transitions are
// illegal, which calls move money, what each step means. Round two asks for
// tests, and it is only allowed to start once round one has been answered,
// because a test written without those answers is a test written from guesses.
const (
	PhaseScanningTheFlow Phase = "ScanningTheFlow"
	PhaseAskingTheAgent  Phase = "AskingTheAgent"
)
