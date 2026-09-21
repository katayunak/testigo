package testPlan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
)

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

type Scenario struct {
	ID     string
	Name   string
	Family Family

	CaseScenario string

	LookingFor string

	Acceptance []string

	AntiGoals []string

	Techniques []Technique

	Oracle OracleProvenance

	Requires Requires

	Severity flowEntity.Severity
}

type Requires struct {
	SeamKinds       []flowEntity.SeamKind
	InjectableSeam  bool
	StateMachine    bool
	MoneyFlows      bool
	MultipleEntries bool
	OpensTx         bool
	Goroutine       bool

	RealDatabase bool
	MultiTenant  bool
	HashChain    bool

	BalanceFunc    bool
	TransferFunc   bool
	IdempotencyKey bool

	WritePatterns []flowEntity.WritePattern
}

type Facts struct {
	MoneyType      string
	BalanceFunc    string
	TransferFunc   string
	IdempotencyKey string
	Uniqueness     string
	Known          bool

	StateMachines map[string]flowEntity.StateRoles

	Skipped map[string]string
}

func (s Scenario) Applies(f *flowEntity.Flow, b Facts) (bool, string) {
	r := s.Requires

	if why, off := b.Skipped[s.ID]; off {
		return false, "turned off in testigo/rules.json: " + why
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

	if r.StateMachine && len(f.States) == 0 {
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
	if len(r.WritePatterns) > 0 {
		found := false
		for _, p := range r.WritePatterns {
			if f.HasWritePattern(p) {
				found = true
				break
			}
		}
		if !found {
			return false, "no table in this repository is written the way this scenario needs (" + patternList(r.WritePatterns) + ")"
		}
	}
	if r.InjectableSeam && !anySeam(f, func(sm flowEntity.Seam) bool { return sm.Injectable }) {

		return false, "every boundary in this flow is a concrete type, so no fault can be injected — extract an interface first"
	}
	if r.MultiTenant {
		if _, ok := f.Infra.TenantScheme(); !ok {
			return false, "no column leads a composite uniqueness constraint on two or more tables, so there is no shared-tenant discriminator to test for a leak"
		}
	}
	if r.HashChain && len(f.HashChains) == 0 {
		return false, "no method takes the previous instance of its own type and computes a cryptographic hash, so there is no hash chain to test"
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

func patternList(ps []flowEntity.WritePattern) string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, string(p))
	}
	sort.Strings(out)
	return strings.Join(out, " or ")
}
