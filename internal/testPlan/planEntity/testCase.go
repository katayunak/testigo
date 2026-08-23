package planEntity

import (
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// TestCase is one scenario, bound to one technique and to this repository.
//
// It is the whole input to a prompt. Nothing else is consulted at render time,
// which means the prompt can be reviewed by reading this struct, and two runs
// that produce the same TestCase produce the same prompt.
type TestCase struct {
	Scenario  Scenario
	Technique Technique
	Size      Size
	Scope     Scope
	Role      Role

	// FuncName is the Go test function to write.
	FuncName string

	// TargetPkg is where the file goes.
	TargetPkg  string
	TargetFile string

	// Entry is the entry point this case is about. Most scenarios are about one
	// path through the system, and scoping to it is what keeps the prompt small.
	Entry *flowEntity.EntryPoint

	// Seams the test will need to fake: deduplicated by target, and reachable
	// from Entry. Not every seam in the repository.
	Seams []flowEntity.Seam

	// States is the machine this case is about, when it is about one.
	States *flowEntity.StateMachine

	// Blocked is set when the case cannot honestly be written for this repo. A
	// blocked case is still reported: "we could not test this and here is what
	// is missing" is a result, not a gap.
	Blocked string
}

// Runnable reports whether this case can actually be generated.
func (c TestCase) Runnable() bool { return c.Blocked == "" }
