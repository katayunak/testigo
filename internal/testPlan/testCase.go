package testPlan

import (
	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

type TestCase struct {
	Scenario  Scenario
	Technique Technique
	Size      Size

	FuncName string

	TargetPkg  string
	TargetFile string

	Entry *flowEntity.EntryPoint

	Seams []flowEntity.Seam

	States *flowEntity.StateMachine

	Tenancy *flowEntity.TenantScheme

	HashChain *flowEntity.HashChain

	Blocked string
}

func (c TestCase) Runnable() bool { return c.Blocked == "" }
