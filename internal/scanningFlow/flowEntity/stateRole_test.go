package flowEntity

import (
	"reflect"
	"sort"
	"testing"
)

// TestDerivedMatrixMatchesAHandWrittenOne is the proof for the whole idea.
//
// A real recharge service was scanned, and an agent was paid to answer the old
// N×N transitions question about micro/domain/entity.Status — nine states,
// eighty-one cells. The answer below in `want` is that agent's output, copied
// verbatim from testigo/answers/transitions-micro-domain-entity-Status.json.
//
// The roles in `roles` are the same nine states classified once each, which is
// nine answers instead of eighty-one. If Derive reproduces the matrix, then the
// expensive question was the wrong shape and this is the right one.
//
// The two cases that make it interesting:
//
//	FAILED    is final AND retryable — a retry cron picks failed orders back up,
//	          so it must be able to reach PENDING. Treating it as plainly final
//	          produces a test that breaks the first time a customer retries.
//	ENABLE    and DISABLE are foreign: they share the Status type but belong to
//	DISABLE   providers and contacts, not orders. They form their own two-state
//	          machine and never touch the order lifecycle.
func TestDerivedMatrixMatchesAHandWrittenOne(t *testing.T) {
	roles := StateRoles{
		{State: "INITIAL", Initializing: true, Proof: "order_dto.go:29 — set on insert"},
		{State: "PROGRESS", InProgress: true, Proof: "order_dto.go:95"},
		{State: "PENDING", Pending: true, Proof: "waiting on the provider reply"},
		{State: "SUCCESS", Final: true, Proof: "order_dto.go:442 — insert refused after this"},
		{State: "FAILED", Final: true, Retryable: true, RetryEntersAt: "PENDING", Proof: "get_status/retry.go — retry cron re-attempts"},
		{State: "ENABLE", Foreign: true, Proof: "applies to providers and contacts, not orders"},
		{State: "DISABLE", Foreign: true, Proof: "applies to providers and contacts, not orders"},
		{State: "ERROR", Final: true, Proof: "declared, never assigned in this module"},
		{State: "FAILEDSIMTYPE", Sentinel: true, Proof: "order_dto.go:122 — in-memory discriminator, never stored"},
	}

	// What the agent produced, and what a person would have to check by hand.
	want := map[string][]string{
		"INITIAL":       {"FAILED", "PENDING", "PROGRESS"},
		"PROGRESS":      {"FAILED", "PENDING", "SUCCESS"},
		"PENDING":       {"FAILED", "PENDING", "PROGRESS", "SUCCESS"},
		"SUCCESS":       {},
		"FAILED":        {"PENDING"},
		"ENABLE":        {"DISABLE"},
		"DISABLE":       {"ENABLE"},
		"ERROR":         {},
		"FAILEDSIMTYPE": {},
	}

	// ERROR is declared and never assigned anywhere in the module — phase 1
	// proved that, so nothing here can move to it.
	got := roles.Derive([]string{"ERROR"})

	// The bar is SUPERSET, not equality, and the direction matters.
	//
	// These derived edges feed a test that asserts illegal transitions are
	// REFUSED. Calling a transition legal when it is not costs one test that
	// never gets written. Calling it illegal when the business allows it
	// produces a red test asserting something real, which someone deletes
	// instead of fixing. So the derivation over-approximates, exactly as the
	// call graph does, and every edge the agent paid for must survive.
	for state, wantTo := range want {
		gotTo := got[state]
		sort.Strings(gotTo)
		sort.Strings(wantTo)
		for _, w := range wantTo {
			if !contains(gotTo, w) {
				t.Errorf("%s -> %s: the agent said this is legal and the derivation forbids it\n  derived %v",
					state, w, gotTo)
			}
		}
	}
	_ = reflect.DeepEqual

	// PROGRESS must not be able to reach INITIAL: a payment does not go back to
	// being new. This is the class of mistake the old matrix invited, because
	// every cell was filled in by hand.
	for _, to := range got["PROGRESS"] {
		if to == "INITIAL" {
			t.Error("PROGRESS -> INITIAL was derived; a payment cannot become new again")
		}
	}
	// And SUCCESS must be a dead end, because nothing here is compensating.
	if len(got["SUCCESS"]) != 0 {
		t.Errorf("SUCCESS is final with no compensating state, so it must have no exits, got %v", got["SUCCESS"])
	}

	// The edges it must NEVER derive, whatever else it over-approximates.
	for _, bad := range []struct{ from, to, why string }{
		{"PROGRESS", "INITIAL", "a payment cannot become new again"},
		{"PROGRESS", "ERROR", "nothing in this module ever assigns ERROR"},
		{"PENDING", "ERROR", "nothing in this module ever assigns ERROR"},
		{"FAILED", "INITIAL", "the order already exists; a retry re-enters at PENDING"},
		{"FAILED", "SUCCESS", "a retry does not jump straight to success"},
		{"ENABLE", "PROGRESS", "ENABLE belongs to providers, not to the order lifecycle"},
		{"FAILEDSIMTYPE", "PROGRESS", "a sentinel is never stored, so nothing moves from it"},
	} {
		if contains(got[bad.from], bad.to) {
			t.Errorf("derived %s -> %s, but %s", bad.from, bad.to, bad.why)
		}
	}
}

// TestCompensatingReopensAFinalState covers the case recharge does not have and
// most payment systems do.
//
// Captured is finished, until a chargeback arrives weeks later. A refund is
// reached FROM a settled payment, which is the one legitimate way out of a
// final state, and a lifecycle that cannot express it produces a test asserting
// refunds are impossible.
func TestCompensatingReopensAFinalState(t *testing.T) {
	roles := StateRoles{
		{State: "PENDING", Initializing: true, Proof: "x.go:1"},
		{State: "CAPTURED", Final: true, Proof: "x.go:2"},
		{State: "REFUNDED", Final: true, Compensating: true, Proof: "x.go:3"},
	}
	got := roles.Derive(nil)

	if !contains(got["CAPTURED"], "REFUNDED") {
		t.Errorf("a captured payment must be able to reach a compensating state, got %v", got["CAPTURED"])
	}
	if contains(got["REFUNDED"], "CAPTURED") {
		t.Error("a refund does not become a capture again")
	}
}

// TestRoleContradictionsAreRefused. Coherence is checkable; correctness is not.
// So the checkable half is checked hard.
func TestRoleContradictionsAreRefused(t *testing.T) {
	for _, c := range []struct {
		name string
		role StateRole
		want string
	}{
		{"two phases", StateRole{State: "X", InProgress: true, Final: true, Proof: "e"}, "mutually exclusive"},
		{"no phase", StateRole{State: "X", Proof: "e"}, "no role given"},
		{"retryable but not final", StateRole{State: "X", InProgress: true, Retryable: true, Proof: "e"}, "only says something about a FINAL"},
		{"modifier on a foreign state", StateRole{State: "X", Foreign: true, Compensating: true, Proof: "e"}, "cannot apply to a state outside it"},
		{"no proof", StateRole{State: "X", Final: true}, "no proof"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.role.Validate()
			if err == nil {
				t.Fatalf("accepted an incoherent role")
			}
			if !containsSub(err.Error(), c.want) {
				t.Errorf("wrong reason:\n  got  %v\n  want it to mention %q", err, c.want)
			}
		})
	}

	ok := StateRole{State: "FAILED", Final: true, Retryable: true, Proof: "retry.go:12"}
	if err := ok.Validate(); err != nil {
		t.Errorf("a retryable final state is the whole point and must be allowed: %v", err)
	}
}

// TestShapeReportsWhatIsWrong. A number would have to be invented; naming the
// deviation tells a person where to look.
func TestShapeReportsWhatIsWrong(t *testing.T) {
	_, problems := StateRoles{
		{State: "A", Initializing: true, Proof: "e"},
		{State: "B", Initializing: true, Proof: "e"},
		{State: "C", InProgress: true, Proof: "e"},
	}.Shape()

	if len(problems) < 2 {
		t.Fatalf("two starts and no final state are two problems, got %v", problems)
	}
	joined := ""
	for _, p := range problems {
		joined += p + " | "
	}
	for _, want := range []string{"more than one state claims to be the start", "nothing is final"} {
		if !containsSub(joined, want) {
			t.Errorf("Shape did not report %q, said: %s", want, joined)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func containsSub(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
