package testPlan

import (
	"strings"
	"testing"

	"github.com/katayunak/gofacts/fact"
)

func TestSelectPicksTheLifecycleStateMachineNotTheFirstOne(t *testing.T) {
	f := &fact.Flow{
		States: []fact.StateMachine{
			{Type: "example.com/x.EType", States: []string{"CARD", "PSP", "TIMEOUT"}},
			{Type: "example.com/x.Status", States: []string{"INITIAL", "PENDING", "SUCCESS", "FAILED"}},
		},
	}
	facts := Facts{
		Known: true,
		StateMachines: map[string]fact.StateRoles{
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
	var got *fact.StateMachine
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
	f := &fact.Flow{
		Nodes: map[string]*fact.Node{
			"example.com/x#Transfer": {Facts: fact.Facts{OpensTx: true}},
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
	f := &fact.Flow{
		Nodes: map[string]*fact.Node{
			"example.com/x#Transfer": {Facts: fact.Facts{}},
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

func TestTenantRowsDontLeakNeedsASharedDiscriminatorColumn(t *testing.T) {
	f := &fact.Flow{
		Infra: fact.Infra{Constraints: []fact.Constraint{
			{Table: "logs", Columns: []string{"ledger", "idempotency_key"}, Kind: "unique_index"},
			{Table: "transactions", Columns: []string{"ledger", "id"}, Kind: "unique_index"},
		}},
	}

	var got *TestCase
	for _, c := range Select(f, Facts{}) {
		if c.Scenario.ID == "TENANT-ROWS-DONT-LEAK" {
			cc := c
			got = &cc
		}
	}
	if got == nil {
		t.Fatal("TENANT-ROWS-DONT-LEAK was not offered at all")
	}
	if !got.Runnable() {
		t.Fatalf("should be runnable once a column leads uniqueness on two tables, got blocked: %q", got.Blocked)
	}
	if got.Tenancy == nil || got.Tenancy.Column != "ledger" {
		t.Fatalf("expected the case to carry the discriminator column, got %+v", got.Tenancy)
	}
}

func TestTenantRowsDontLeakBlockedWithoutASharedDiscriminatorColumn(t *testing.T) {
	f := &fact.Flow{
		Infra: fact.Infra{Constraints: []fact.Constraint{
			{Table: "users", Columns: []string{"email"}, Kind: "unique_index"},
		}},
	}

	for _, c := range Select(f, Facts{}) {
		if c.Scenario.ID == "TENANT-ROWS-DONT-LEAK" {
			if c.Runnable() {
				t.Fatal("should be blocked when no column leads uniqueness on two or more tables")
			}
			if c.Tenancy != nil {
				t.Fatal("a blocked case should not carry a tenancy scheme")
			}
			return
		}
	}
	t.Fatal("TENANT-ROWS-DONT-LEAK was not offered at all")
}

func TestHashChainCatchesTamperingNeedsASelfTypedHashingMethod(t *testing.T) {
	f := &fact.Flow{
		HashChains: []fact.HashChain{
			{Type: "example.com/x.Log", Method: fact.CodeRef{Symbol: "Log.ChainLog", File: "x.go", Line: 10}},
		},
	}

	var got *TestCase
	for _, c := range Select(f, Facts{}) {
		if c.Scenario.ID == "HASH-CHAIN-CATCHES-TAMPERING" {
			cc := c
			got = &cc
		}
	}
	if got == nil {
		t.Fatal("HASH-CHAIN-CATCHES-TAMPERING was not offered at all")
	}
	if !got.Runnable() {
		t.Fatalf("should be runnable once a hash-chaining method is found, got blocked: %q", got.Blocked)
	}
	if got.HashChain == nil || got.HashChain.Type != "example.com/x.Log" {
		t.Fatalf("expected the case to carry the hash chain found by the scanner, got %+v", got.HashChain)
	}
}

func TestHashChainCatchesTamperingBlockedWithoutOne(t *testing.T) {
	f := &fact.Flow{}

	for _, c := range Select(f, Facts{}) {
		if c.Scenario.ID == "HASH-CHAIN-CATCHES-TAMPERING" {
			if c.Runnable() {
				t.Fatal("should be blocked when the scanner found no hash-chaining method")
			}
			if c.HashChain != nil {
				t.Fatal("a blocked case should not carry a hash chain")
			}
			return
		}
	}
	t.Fatal("HASH-CHAIN-CATCHES-TAMPERING was not offered at all")
}

func TestSelectFallsBackToFirstMachineBeforeRolesAreAnswered(t *testing.T) {
	f := &fact.Flow{
		States: []fact.StateMachine{
			{Type: "example.com/x.EType", States: []string{"CARD", "PSP"}},
			{Type: "example.com/x.Status", States: []string{"INITIAL", "SUCCESS"}},
		},
	}

	cases := Select(f, Facts{Known: true})
	var got *fact.StateMachine
	for _, c := range cases {
		if c.Scenario.ID == "ILLEGAL-TRANSITION-REFUSED" {
			got = c.States
		}
	}
	if got == nil || got.Type != "example.com/x.EType" {
		t.Fatalf("with no roles answered yet, expected the deterministic first-machine fallback, got %+v", got)
	}
}

func twoEntryFlow() *fact.Flow {
	api := fact.CodeRef{Pkg: "example.com/x/api", Symbol: "(*Server).Pay", File: "api/server.go", Line: 10}
	charge := fact.CodeRef{Pkg: "example.com/x/api", Symbol: "(*Server).charge", File: "api/server.go", Line: 30}
	webhook := fact.CodeRef{Pkg: "example.com/x/webhook", Symbol: "(*Handler).Callback", File: "webhook/handler.go", Line: 12}
	credit := fact.CodeRef{Pkg: "example.com/x/webhook", Symbol: "(*Handler).credit", File: "webhook/handler.go", Line: 40}
	return &fact.Flow{
		Entries: []fact.EntryPoint{
			{Pkg: api.Pkg, Symbol: api.Symbol, Label: "API"},
			{Pkg: webhook.Pkg, Symbol: webhook.Symbol, Label: "webhook"},
		},
		Nodes: map[string]*fact.Node{
			api.ID():     {Ref: api, Calls: []string{charge.ID()}},
			charge.ID():  {Ref: charge},
			webhook.ID(): {Ref: webhook, Calls: []string{credit.ID()}},
			credit.ID():  {Ref: credit},
		},
		Seams: []fact.Seam{
			{In: credit, Kind: fact.SeamDB, Target: "(example.com/x/ledger.Store).Credit", Line: 44, Injectable: true, Iface: "example.com/x/ledger.Store"},
			{In: charge, Kind: fact.SeamHTTP, Target: "(example.com/x/psp.Gateway).Charge", Line: 33, Injectable: true, Iface: "example.com/x/psp.Gateway"},
		},
	}
}

func caseNamed(t *testing.T, cases []TestCase, id string) TestCase {
	t.Helper()
	for _, c := range cases {
		if c.Scenario.ID == id {
			return c
		}
	}
	t.Fatalf("%s was not offered at all", id)
	return TestCase{}
}

func seamTargets(seams []fact.Seam) []string {
	out := make([]string, 0, len(seams))
	for _, s := range seams {
		out = append(out, s.Target)
	}
	return out
}

func entryIDs(entries []fact.EntryPoint) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID())
	}
	return out
}

func TestSeamsAreScopedFromEveryEntryNotOnlyTheFirst(t *testing.T) {
	c := caseNamed(t, Select(twoEntryFlow(), Facts{}), "IDEM-REPLAY")
	if !c.Runnable() {
		t.Fatalf("fixture setup: expected a runnable case, got blocked: %q", c.Blocked)
	}

	got := strings.Join(seamTargets(c.Seams), ", ")
	want := "(example.com/x/ledger.Store).Credit, (example.com/x/psp.Gateway).Charge"
	if got != want {
		t.Fatalf("seams = %s, want %s: the ledger call is reached only from the webhook, the second entry point", got, want)
	}

	gotEntries := strings.Join(entryIDs(c.Entries), ", ")
	wantEntries := "example.com/x/api#(*Server).Pay, example.com/x/webhook#(*Handler).Callback"
	if gotEntries != wantEntries {
		t.Fatalf("entries under test = %s, want %s, in config order", gotEntries, wantEntries)
	}
}

func TestACaseWhoseSeamsOnlyTheSecondEntryReachesIsDrivenFromIt(t *testing.T) {
	f := twoEntryFlow()
	f.Seams = f.Seams[:1]

	c := caseNamed(t, Select(f, Facts{}), "IDEM-REPLAY")
	if got := strings.Join(entryIDs(c.Entries), ", "); got != "example.com/x/webhook#(*Handler).Callback" {
		t.Fatalf("entries under test = %s, want only the webhook: the API entry never reaches the ledger call", got)
	}
	if c.TargetPkg != "example.com/x/webhook" || !strings.HasPrefix(c.TargetFile, "webhook/") {
		t.Fatalf("test goes in %s (package %s), want it beside the webhook entry that reaches the seam", c.TargetFile, c.TargetPkg)
	}
}

func TestASeamCalledFromTwoEntriesKeepsTheFirstEntrysCallSite(t *testing.T) {
	f := twoEntryFlow()
	shared := "(example.com/x/ledger.Store).Credit"
	charge := f.Nodes["example.com/x/api#(*Server).charge"].Ref
	f.Seams = append(f.Seams, fact.Seam{In: charge, Kind: fact.SeamDB, Target: shared, Line: 35, Injectable: true, Iface: "example.com/x/ledger.Store"})

	c := caseNamed(t, Select(f, Facts{}), "IDEM-REPLAY")
	for _, s := range c.Seams {
		if s.Target == shared && s.In.ID() != charge.ID() {
			t.Fatalf("%s is shown called in %s; want the call site the first entry point reaches, %s", shared, s.In.Symbol, charge.Symbol)
		}
	}
	if got := strings.Join(entryIDs(c.Entries), ", "); got != "example.com/x/api#(*Server).Pay" {
		t.Fatalf("entries under test = %s, want only the API entry, since every shown call site is reached from it", got)
	}
}

func TestSelectIsTheSameEveryRunWithSeveralEntries(t *testing.T) {
	first := Select(twoEntryFlow(), Facts{})
	for run := 0; run < 20; run++ {
		again := Select(twoEntryFlow(), Facts{})
		for i := range first {
			a, b := first[i], again[i]
			if a.Scenario.ID != b.Scenario.ID ||
				strings.Join(seamTargets(a.Seams), ",") != strings.Join(seamTargets(b.Seams), ",") ||
				strings.Join(entryIDs(a.Entries), ",") != strings.Join(entryIDs(b.Entries), ",") ||
				a.TargetFile != b.TargetFile {
				t.Fatalf("run %d, case %d differs: %s %v %v vs %s %v %v", run, i,
					a.Scenario.ID, seamTargets(a.Seams), entryIDs(a.Entries),
					b.Scenario.ID, seamTargets(b.Seams), entryIDs(b.Entries))
			}
		}
	}
}

func TestTimeoutUnknownOutcomeBlockedWhenOnlyTheDatabaseSeamIsInjectable(t *testing.T) {
	f := twoEntryFlow()
	f.Seams[1].Injectable = false
	f.Seams[1].Iface = ""

	got := caseNamed(t, Select(f, Facts{}), "TIMEOUT-UNKNOWN-OUTCOME")
	if got.Runnable() {
		t.Fatal("a provider timeout cannot be faked through a concrete http client, even when the database seam is an interface")
	}
	if !strings.Contains(got.Blocked, "http or queue boundary in this flow is an interface") {
		t.Fatalf("the reason should name the boundary that needs an interface, got %q", got.Blocked)
	}
}

func TestTimeoutUnknownOutcomeIsGivenOnlyTheInjectableProviderSeams(t *testing.T) {
	f := twoEntryFlow()
	charge := f.Seams[1].In
	f.Seams = append(f.Seams, fact.Seam{In: charge, Kind: fact.SeamHTTP, Target: "(*net/http.Client).Do", Line: 35})

	got := caseNamed(t, Select(f, Facts{}), "TIMEOUT-UNKNOWN-OUTCOME")
	if !got.Runnable() {
		t.Fatalf("an injectable provider seam should make the timeout scenario runnable, got blocked: %q", got.Blocked)
	}
	gotSeams := strings.Join(seamTargets(got.Seams), ", ")
	if want := "(example.com/x/psp.Gateway).Charge"; gotSeams != want {
		t.Fatalf("seams = %s, want %s: a fault can only be injected into the interface, not the concrete http client", gotSeams, want)
	}
}

func TestUnclassifiedErrorNotRetriedNeedsAProviderSeamNotOnlyADatabaseOne(t *testing.T) {
	f := twoEntryFlow()
	f.Seams = f.Seams[:1]

	got := caseNamed(t, Select(f, Facts{}), "UNCLASSIFIED-ERROR-NOT-RETRIED")
	if got.Runnable() {
		t.Fatalf("an unrecognised provider error cannot be faked when the only injectable seam is the ledger store, got seams %s", strings.Join(seamTargets(got.Seams), ", "))
	}
	if !strings.Contains(got.Blocked, "http or queue boundary") {
		t.Fatalf("the reason should name the provider boundary that is missing, got %q", got.Blocked)
	}

	provider := caseNamed(t, Select(twoEntryFlow(), Facts{}), "UNCLASSIFIED-ERROR-NOT-RETRIED")
	if gotSeams := strings.Join(seamTargets(provider.Seams), ", "); gotSeams != "(example.com/x/psp.Gateway).Charge" {
		t.Fatalf("seams = %s, want only the provider gateway: the ledger store is not where a provider error comes from", gotSeams)
	}
}
