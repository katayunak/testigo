package testPlan

import (
	"testing"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

func TestSelectPicksTheLifecycleStateMachineNotTheFirstOne(t *testing.T) {
	f := &flowEntity.Flow{
		States: []flowEntity.StateMachine{
			{Type: "example.com/x.EType", States: []string{"CARD", "PSP", "TIMEOUT"}},
			{Type: "example.com/x.Status", States: []string{"INITIAL", "PENDING", "SUCCESS", "FAILED"}},
		},
	}
	facts := Facts{
		Known: true,
		StateMachines: map[string]flowEntity.StateRoles{
			"example.com/x.EType": {
				{State: "CARD", Foreign: true, Proof: "x.go:1"},
				{State: "PSP", Foreign: true, Proof: "x.go:2"},
				{State: "TIMEOUT", Foreign: true, Proof: "x.go:3"},
			},
			"example.com/x.Status": {
				{State: "INITIAL", Initializing: true, Proof: "x.go:10"},
				{State: "PENDING", InProgress: true, Proof: "x.go:11"},
				{State: "SUCCESS", Final: true, Proof: "x.go:12"},
				{State: "FAILED", Final: true, Proof: "x.go:13"},
			},
		},
	}

	cases := Select(f, facts)
	var got *flowEntity.StateMachine
	for _, c := range cases {
		if c.Scenario.ID == "ILLEGAL-TRANSITION-REFUSED" {
			got = c.States
		}
	}
	if got == nil {
		t.Fatal("ILLEGAL-TRANSITION-REFUSED did not get a state machine at all")
	}
	if got.Type != "example.com/x.Status" {
		t.Fatalf("picked %q, an error-code classifier with no real lifecycle — want the Status machine, "+
			"since it is the one with an initial and final state", got.Type)
	}
}

func TestDeadlockIsRecoveredNeedsAnOpenTransactionAndATransferFunc(t *testing.T) {
	f := &flowEntity.Flow{
		Nodes: map[string]*flowEntity.Node{
			"example.com/x#Transfer": {Facts: flowEntity.Facts{OpensTx: true}},
		},
	}
	facts := Facts{Known: true, TransferFunc: "example.com/x#Transfer"}

	var got *TestCase
	for _, c := range Select(f, facts) {
		if c.Scenario.ID == "DEADLOCK-IS-RECOVERED" {
			cc := c
			got = &cc
		}
	}
	if got == nil {
		t.Fatal("DEADLOCK-IS-RECOVERED was not offered at all")
	}
	if !got.Runnable() {
		t.Fatalf("DEADLOCK-IS-RECOVERED should be runnable once a transfer function opens its own transaction, got blocked: %q", got.Blocked)
	}
	if got.Size != SizeMedium {
		t.Fatalf("a scenario needing a real database must be sized medium, got %q", got.Size)
	}
}

func TestDeadlockIsRecoveredBlockedWithoutAnOpenTransaction(t *testing.T) {
	f := &flowEntity.Flow{
		Nodes: map[string]*flowEntity.Node{
			"example.com/x#Transfer": {Facts: flowEntity.Facts{}},
		},
	}
	facts := Facts{Known: true, TransferFunc: "example.com/x#Transfer"}

	for _, c := range Select(f, facts) {
		if c.Scenario.ID == "DEADLOCK-IS-RECOVERED" {
			if c.Runnable() {
				t.Fatal("DEADLOCK-IS-RECOVERED should be blocked when nothing in the flow opens a transaction")
			}
			return
		}
	}
	t.Fatal("DEADLOCK-IS-RECOVERED was not offered at all")
}

func TestSelectFallsBackToFirstMachineBeforeRolesAreAnswered(t *testing.T) {
	f := &flowEntity.Flow{
		States: []flowEntity.StateMachine{
			{Type: "example.com/x.EType", States: []string{"CARD", "PSP"}},
			{Type: "example.com/x.Status", States: []string{"INITIAL", "SUCCESS"}},
		},
	}

	cases := Select(f, Facts{Known: true})
	var got *flowEntity.StateMachine
	for _, c := range cases {
		if c.Scenario.ID == "ILLEGAL-TRANSITION-REFUSED" {
			got = c.States
		}
	}
	if got == nil || got.Type != "example.com/x.EType" {
		t.Fatalf("with no roles answered yet, expected the deterministic first-machine fallback, got %+v", got)
	}
}
