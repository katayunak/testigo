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
