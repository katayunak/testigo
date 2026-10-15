package testPlan

import (
	"github.com/katayunak/gofacts/fact"
)

type TestCase struct {
	Scenario  Scenario
	Technique Technique
	Size      Size

	FuncName string

	TargetPkg  string
	TargetFile string

	Entries []fact.EntryPoint

	Seams []fact.Seam

	States *fact.StateMachine

	Tenancy *fact.TenantScheme

	HashChain *fact.HashChain

	Blocked string
}

func (c TestCase) Runnable() bool { return c.Blocked == "" }
