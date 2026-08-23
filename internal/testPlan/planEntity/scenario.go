package planEntity

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

// Family groups scenarios by the kind of failure they are about.
type Family string

const (
	FamilyIdempotency Family = "idempotency"
	FamilyConsistency Family = "consistency"
	FamilyMoney       Family = "money"
	FamilyState       Family = "state"
	FamilyFailure     Family = "failure"
	FamilyOrdering    Family = "ordering"
	FamilyBoundary    Family = "boundary"
)

// Scenario is one way a payment system breaks, described once, in data.
//
// This is the part of testigo that costs nothing to own. The catalog lives in Go,
// is reviewed like code, and never enters a prompt unless a repository actually
// matches its preconditions. Writing thirty scenarios costs thirty times nothing;
// only the handful that apply to your repo are ever rendered into tokens.
//
// The fields are shaped by what a prompt needs, and the two that carry the most
// weight are the ones usually missing from a test description:
//
//   - Acceptance says what passing MEANS, so an agent has a target instead of a
//     vibe, and a reviewer has something to check the generated code against.
//   - AntiGoals says what a WRONG version of this test looks like. Almost every
//     bad generated test is bad in a predictable way, and naming that way in
//     advance is cheaper and more effective than any amount of "be careful".
type Scenario struct {
	ID     string
	Name   string
	Family Family

	// CaseScenario is the whole thing that must happen in the test, in order,
	// and why it is worth testing. Written for someone who will implement it.
	CaseScenario string

	// LookingFor is the defect this catches, stated as the bug, not the feature.
	LookingFor string

	// Acceptance is what must be true for the test to pass. Each item should be
	// mechanically checkable.
	Acceptance []string

	// AntiGoals are the specific wrong versions of this test.
	AntiGoals []string

	// Techniques are the ways this scenario can be expressed, best first.
	Techniques []Technique

	// Oracle is where "correct" comes from for this scenario.
	Oracle OracleProvenance

	// Requires are the facts that must hold for this scenario to apply at all.
	Requires Requires

	// Severity of the bug this finds, if found.
	Severity flowEntity.Severity

	// Source is where the rule comes from. A scenario with no source is a
	// scenario someone made up, and it should be obvious which is which.
	Source string
}

// Requires is a predicate over what phase 1 proved.
//
// This is the whole token-efficiency story. A repository with no state machine
// never sees a transition question. A repository whose seams are all concrete
// types never sees a fault-injection prompt it could not act on. The catalog can
// grow without the prompt pack growing, because applicability is checked in Go
// before anything is rendered.
type Requires struct {
	SeamKinds       []flowEntity.SeamKind
	InjectableSeam  bool
	StateMachine    bool
	MoneyFlows      bool
	MultipleEntries bool
	OpensTx         bool
	Goroutine       bool

	// RealDatabase means an in-process fake cannot express this scenario
	// honestly, so it must be a medium-size test against a container.
	RealDatabase bool

	// The next three are answered by round one, not by the compiler, and they
	// are the sharpest filters available.
	//
	// A conservation test needs something that reads a balance. A currency test
	// needs a money type. An idempotency test needs a key. The compiler cannot
	// find any of those — it can see that a function returns an int64, not that
	// the int64 is a balance. Asking the binding question first and filtering on
	// the answer is what stops testigo generating a conservation test for a
	// repository that has no concept of a balance.
	BalanceFunc    bool
	TransferFunc   bool
	IdempotencyKey bool
}

// Bindings is the slice of round-one knowledge the catalog filters on. Passed in
// rather than imported so testPlan does not depend on askingAgent, which depends
// on testPlan.
type Bindings struct {
	MoneyType      string
	BalanceFunc    string
	TransferFunc   string
	IdempotencyKey string
	Uniqueness     string
	Known          bool

	// From testigo.rules.json, when the team wrote one.
	Domain           string
	MoneyMovement    string
	ExternalSignal   string
	RetryPolicy      bool
	ReversalPossible bool
	Skipped          map[string]string // scenario ID -> reason
}

// Applies reports whether this scenario is worth asking about, and why not when
// it is not.
//
// Called before anything is rendered, which is where the token saving lives: a
// scenario that cannot apply is never written to a file, never read by an agent,
// and never paid for.
func (s Scenario) Applies(f *flowEntity.Flow, b Bindings) (bool, string) {
	r := s.Requires

	if why, off := b.Skipped[s.ID]; off {
		return false, "turned off in testigo.rules.json: " + why
	}

	if b.Known {
		if r.BalanceFunc && b.BalanceFunc == "" {
			return false, "round one found no function that reads a balance, so there is nothing to assert conservation against"
		}
		if r.TransferFunc && b.TransferFunc == "" {
			return false, "round one found no function that moves money locally"
		}
		if r.IdempotencyKey && b.IdempotencyKey == "" {
			return false, "round one found no idempotency key, so there is no key to retry with"
		}
		if r.MoneyFlows && b.MoneyType == "" && b.TransferFunc == "" {
			return false, "round one identified no money type and no transfer function"
		}
	}

	if r.StateMachine && len(f.Machines) == 0 {
		return false, "no status type with declared constants was found"
	}
	if r.MultipleEntries && len(f.Entries) < 2 {
		return false, "only one entry point is configured, so there is no second path to order against"
	}
	if r.MoneyFlows && !b.Known && !anyNode(f, func(n *flowEntity.Node) bool { return n.Facts.HandlesMoney }) {
		return false, "no money-shaped type flows through the reachable functions"
	}
	if r.OpensTx && !anyNode(f, func(n *flowEntity.Node) bool { return n.Facts.OpensTx }) {
		return false, "nothing in this flow opens a transaction"
	}
	if r.Goroutine && !anyNode(f, func(n *flowEntity.Node) bool { return n.Facts.SpawnsGoroutine }) {
		return false, "this flow never starts a goroutine"
	}
	if len(r.SeamKinds) > 0 && !anySeam(f, func(sm flowEntity.Seam) bool {
		for _, k := range r.SeamKinds {
			if sm.Kind == k {
				return true
			}
		}
		return false
	}) {
		return false, "this flow has no " + kindList(r.SeamKinds) + " boundary"
	}
	if r.InjectableSeam && !anySeam(f, func(sm flowEntity.Seam) bool { return sm.Injectable }) {
		// Not a silent skip. An untestable repository is a finding.
		return false, "every boundary in this flow is a concrete type, so no fault can be injected — extract an interface first"
	}
	return true, ""
}

func anyNode(f *flowEntity.Flow, pred func(*flowEntity.Node) bool) bool {
	for _, n := range f.Nodes {
		if pred(n) {
			return true
		}
	}
	return false
}

func anySeam(f *flowEntity.Flow, pred func(flowEntity.Seam) bool) bool {
	for _, s := range f.Seams {
		if pred(s) {
			return true
		}
	}
	return false
}

func kindList(kinds []flowEntity.SeamKind) string {
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return strings.Join(out, " or ")
}

// BestTechnique returns the technique this scenario should use, given what the
// repository can support.
//
// The ordering in Techniques is by preference, but preference loses to honesty:
// a scenario that needs a real database will not be downgraded to a unit test
// with an in-memory map, because that test would pass on broken code. When the
// repository cannot support the honest technique, the answer is a refusal with a
// reason, not a weaker test.
func (s Scenario) BestTechnique(f *flowEntity.Flow) (Technique, string) {
	if len(s.Techniques) == 0 {
		return "", "no technique is declared for this scenario"
	}
	for _, t := range s.Techniques {
		if t == TechniqueFaultInjection && !anySeam(f, func(sm flowEntity.Seam) bool { return sm.Injectable }) {
			continue
		}
		return t, ""
	}
	return "", "the techniques that could express this scenario all need something this repository does not have"
}

func (s Scenario) String() string {
	return fmt.Sprintf("%s (%s)", s.ID, s.Family)
}
