package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"
	"github.com/katayunak/testigo/internal/testPlan"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
)

const AgentResponseSchema = 2

type AgentResponse struct {
	SchemaVersion int    `json:"schema_version"`
	GeneratedAt   string `json:"generated_at,omitempty"`

	MoneyModel  *MoneyModelAnswer             `json:"money_model,omitempty"`
	MainEntity  *MainEntityAnswer             `json:"main_entity,omitempty"`
	Transitions map[string]*TransitionsAnswer `json:"transitions,omitempty"`

	StateRoles      map[string]*StateRolesAnswer     `json:"state_roles,omitempty"`
	ExternalEffects map[string]*ExternalEffectAnswer `json:"external_effects,omitempty"`

	Classification *Classification            `json:"classification,omitempty"`
	PaymentKind    map[string]*QuestionAnswer `json:"payment_kind,omitempty"`
}

type PaymentKindAnswer struct {
	Answers map[string]*QuestionAnswer `json:"answers"`
	Notes   string                     `json:"notes,omitempty"`
}

func FromScan(f *flowEntity.Flow) (*AgentResponse, []string) {
	k := NewAgentResponse()
	var unresolved []string

	money, moneyDecided := f.MoneyTypes.Decided()
	idem, idemDecided := f.IdempotencyKeys.Decided()

	if !moneyDecided && !idemDecided && len(f.MoneyTypes) == 0 && len(f.IdempotencyKeys) == 0 {
		return k, []string{"money type", "idempotency key"}
	}

	b := &MoneyModelAnswer{}
	if moneyDecided {
		b.Money.Type = money.Owner + "." + money.Name
		b.Money.AmountField = money.Name
		b.Money.Representation = representationOf(money.Type)
		b.Money.Proof = proofOf(money)
	} else {
		unresolved = append(unresolved, "money type")
	}

	if idemDecided {
		b.Idempotency.KeyField = idem.Name
		b.Idempotency.Proof = proofOf(idem)
		b.Idempotency.KeySource = "request"

		if _, unique := f.Infra.CoversColumn("", idem.Name); unique {
			b.Idempotency.Uniqueness = "db_constraint"
		} else if len(f.Infra.MigrationDirs) > 0 {
			b.Idempotency.Uniqueness = "app_check_then_write"
		} else {
			b.Idempotency.Uniqueness = "unknown"
		}
	} else {
		unresolved = append(unresolved, "idempotency key")
	}

	if b.Money.Type != "" || b.Idempotency.KeyField != "" {
		k.MoneyModel = b
	}
	return k, unresolved
}

func representationOf(goType string) string {
	t := strings.ToLower(goType)
	switch {
	case strings.Contains(t, "float"):
		return "float"
	case strings.Contains(t, "decimal"), strings.Contains(t, "big.rat"):
		return "decimal"
	case strings.Contains(t, "int"):
		return "minor_units_int"
	}
	return "unknown"
}

func NewAgentResponse() *AgentResponse {
	return &AgentResponse{
		SchemaVersion:   AgentResponseSchema,
		Transitions:     map[string]*TransitionsAnswer{},
		StateRoles:      map[string]*StateRolesAnswer{},
		ExternalEffects: map[string]*ExternalEffectAnswer{},
		PaymentKind:     map[string]*QuestionAnswer{},
	}
}

type Proof struct {
	Symbol string `json:"symbol"`

	At string `json:"at"`
}

func (p Proof) Empty() bool { return p == Proof{} }

func (p Proof) String() string {
	switch {
	case p.Symbol == "":
		return p.At
	case p.At == "":
		return p.Symbol
	}
	return p.Symbol + " (" + p.At + ")"
}

func proofOf(c flowEntity.Candidate) Proof {
	return Proof{
		Symbol: c.Owner + "." + c.Name,
		At:     fmt.Sprintf("%s:%d", c.File, c.Line),
	}
}

type Money struct {
	Type           string `json:"type"`
	AmountField    string `json:"amount_field"`
	Representation string `json:"representation"`
	Currency       string `json:"currency"`
	Proof          Proof  `json:"proof"`
}

type Idempotency struct {
	KeySource  string `json:"key_source"`
	KeyField   string `json:"key_field"`
	StoredIn   string `json:"stored_in"`
	Uniqueness string `json:"uniqueness"`
	Proof      Proof  `json:"proof"`
}

type MoneyModelAnswer struct {
	Money         Money       `json:"money"`
	TransferFunc  Proof       `json:"transfer_func"`
	BalanceFunc   Proof       `json:"balance_func"`
	Idempotency   Idempotency `json:"idempotency"`
	EntityIDField string      `json:"entity_id_field"`
	Notes         string      `json:"notes"`
}

func (a *MoneyModelAnswer) Validate() error {
	var bad []string
	switch a.Money.Representation {
	case "minor_units_int", "decimal", "float", "unknown", "":
	default:
		bad = append(bad, fmt.Sprintf("money.representation %q is not one of minor_units_int, decimal, float, unknown", a.Money.Representation))
	}
	switch a.Idempotency.Uniqueness {
	case "db_constraint", "app_check_then_write", "none", "unknown", "":
	default:
		bad = append(bad, fmt.Sprintf("idempotency.uniqueness %q is not one of db_constraint, app_check_then_write, none, unknown", a.Idempotency.Uniqueness))
	}
	if a.Money.Type != "" && a.Money.Proof.Empty() {
		bad = append(bad, "money.type was named but money.proof is empty: every claim needs a file:line")
	}
	if a.TransferFunc.Symbol != "" && a.TransferFunc.At == "" {
		bad = append(bad, "transfer_func was named but has no proof")
	}

	if strings.HasPrefix(a.Money.Type, "example.com/pay/") {
		bad = append(bad, "money.type is the placeholder from the prompt, not a symbol from this repository")
	}
	return join(bad)
}

type TransitionsAnswer struct {
	InitialState string              `json:"initial_state"`
	MayMoveTo    map[string][]string `json:"may_move_to"`
	Unsure       []struct {
		From string `json:"from"`
		To   string `json:"to"`
		Why  string `json:"why"`
	} `json:"unsure"`
	NeverAssignedVerdict map[string]string `json:"never_assigned_verdict"`

	FinalStates []string `json:"final_states"`

	FinalStateExceptions []struct {
		From string `json:"from"`
		To   string `json:"to"`
		Why  string `json:"why"`
	} `json:"final_state_exceptions"`

	Notes string `json:"notes"`
}

func (a *TransitionsAnswer) IsFinal(state string) bool {
	for _, e := range a.FinalStateExceptions {
		if e.From == state {
			return false
		}
	}
	for _, f := range a.FinalStates {
		if f == state {
			return true
		}
	}
	return false
}

func (a *TransitionsAnswer) ValidateAgainst(states []string) error {
	declared := map[string]bool{}
	for _, s := range states {
		declared[s] = true
	}
	var bad []string
	for _, s := range states {
		if _, ok := a.MayMoveTo[s]; !ok {
			bad = append(bad, fmt.Sprintf("state %q is declared in the code but missing from may_move_to", s))
		}
	}
	for from, tos := range a.MayMoveTo {
		if !declared[from] {
			bad = append(bad, fmt.Sprintf("may_move_to contains %q, which is not a declared state", from))
		}
		for _, to := range tos {
			if !declared[to] {
				bad = append(bad, fmt.Sprintf("%q may_move_to %q, which is not a declared state", from, to))
			}
		}
	}
	if a.InitialState != "" && !declared[a.InitialState] {
		bad = append(bad, fmt.Sprintf("initial_state %q is not a declared state", a.InitialState))
	}
	for _, f := range a.FinalStates {
		if !declared[f] {
			bad = append(bad, fmt.Sprintf("final_states contains %q, which is not a declared state", f))
		}

		if outs := a.MayMoveTo[f]; len(outs) > 0 && !a.hasException(f) {
			bad = append(bad, fmt.Sprintf("%q is listed as final but may_move_to says it can become %v; if that is a real exception, put it in final_state_exceptions with a reason", f, outs))
		}
	}
	for _, e := range a.FinalStateExceptions {
		if !declared[e.From] || !declared[e.To] {
			bad = append(bad, fmt.Sprintf("final_state_exceptions %s -> %s names a state that does not exist", e.From, e.To))
		}
		if strings.TrimSpace(e.Why) == "" {
			bad = append(bad, fmt.Sprintf("exception %s -> %s has no reason: an unexplained exception cannot be reviewed", e.From, e.To))
		}
	}
	return join(bad)
}

func (a *TransitionsAnswer) hasException(from string) bool {
	for _, e := range a.FinalStateExceptions {
		if e.From == from {
			return true
		}
	}
	return false
}

func (a *TransitionsAnswer) Illegal(states []string) [][2]string {
	unsure := map[[2]string]bool{}
	for _, u := range a.Unsure {
		unsure[[2]string{u.From, u.To}] = true
	}
	allowed := map[[2]string]bool{}
	for from, tos := range a.MayMoveTo {
		for _, to := range tos {
			allowed[[2]string{from, to}] = true
		}
	}
	var out [][2]string
	for _, from := range states {
		for _, to := range states {
			pair := [2]string{from, to}
			if !allowed[pair] && !unsure[pair] {
				out = append(out, pair)
			}
		}
	}
	return out
}

type ExternalEffectAnswer struct {
	Target string `json:"-"`

	ChangesExternalState json.RawMessage `json:"changes_external_state"`

	ReversibleByRollback json.RawMessage `json:"reversible_by_rollback"`

	OutcomeObservable json.RawMessage `json:"outcome_observable"`

	AcceptsDedupKey  json.RawMessage `json:"accepts_dedup_key"`
	DedupKeyArgument string          `json:"dedup_key_argument"`

	Undo struct {
		Exists bool  `json:"exists"`
		Proof  Proof `json:"proof"`
	} `json:"undo"`

	MovesMoney json.RawMessage `json:"moves_money"`

	FailureModes []string `json:"failure_modes"`
	Basis        string   `json:"basis"`
	Notes        string   `json:"notes"`
}

func (a *ExternalEffectAnswer) Undoable() bool { return IsTrue(a.ReversibleByRollback) }

func (a *ExternalEffectAnswer) Observable() bool { return IsTrue(a.OutcomeObservable) }

func (a *ExternalEffectAnswer) Deduplicated() bool { return IsTrue(a.AcceptsDedupKey) }

func (a *ExternalEffectAnswer) EscapesRollback() bool {
	return !IsFalse(a.ChangesExternalState) && !IsTrue(a.ReversibleByRollback)
}

func IsTrue(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "true"
}

func IsFalse(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "false"
}

type NotesAnswer struct {
	Step        string   `json:"step"`
	Purpose     string   `json:"purpose"`
	Effects     []string `json:"effects"`
	Assumptions []string `json:"assumptions"`
	Confidence  string   `json:"confidence"`
}

func (a *NotesAnswer) Validate() error {
	var bad []string
	if strings.TrimSpace(a.Step) == "" {
		bad = append(bad, "step is empty")
	}
	switch a.Confidence {
	case "high", "medium", "low", "":
	default:
		bad = append(bad, fmt.Sprintf("confidence %q is not one of high, medium, low", a.Confidence))
	}
	return join(bad)
}

type CaseAnswer struct {
	CaseID        string `json:"-"`
	Status        string `json:"status"`
	BlockedReason string `json:"blocked_reason"`
	Needed        string `json:"needed"`
	File          struct {
		Path    string `json:"path"`
		Package string `json:"package"`
		Content string `json:"content"`
	} `json:"file"`
	FuncName          string   `json:"func_name"`
	FakesAdded        []string `json:"fakes_added"`
	ReachedAssertions []string `json:"reached_assertions"`
	ExpectedToFail    string   `json:"expected_to_fail"`
	OracleUsed        string   `json:"oracle_used"`
}

func (a *CaseAnswer) Written() bool { return a.Status == "written" }

func (a *CaseAnswer) Validate(want planEntity.Size) error {
	var bad []string
	switch a.Status {
	case "blocked":
		if strings.TrimSpace(a.BlockedReason) == "" {
			bad = append(bad, "blocked with no reason: indistinguishable from a case that was skipped")
		}
		return join(bad)
	case "written":
	default:
		bad = append(bad, fmt.Sprintf("status %q, expected written or blocked", a.Status))
		return join(bad)
	}

	if strings.TrimSpace(a.File.Content) == "" {
		bad = append(bad, "status is written but file.content is empty")
		return join(bad)
	}
	if !strings.HasSuffix(a.File.Path, "_test.go") {
		bad = append(bad, fmt.Sprintf("file.path %q must end in _test.go", a.File.Path))
	}
	if len(a.ReachedAssertions) == 0 {
		bad = append(bad, "no reached_assertions: a test that cannot prove the interesting situation occurred reports green having checked nothing")
	}
	if want != planEntity.SizeSmall && !strings.Contains(a.File.Content, "//go:build "+want.BuildTag()) {
		bad = append(bad, fmt.Sprintf("a %s test must start with //go:build %s so a bare `go test ./...` stays fast", want, want.BuildTag()))
	}
	for _, p := range testPlan.CheckSize(a.File.Path, a.File.Content, want) {
		bad = append(bad, "size violation, "+p)
	}
	return join(bad)
}

func join(bad []string) error {
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("%d problem(s):\n  - %s", len(bad), strings.Join(bad, "\n  - "))
}

type EntityRef struct {
	Struct  string `json:"struct"`
	Package string `json:"package"`
	Proof   Proof  `json:"proof"`
	Why     string `json:"why"`
}

type KeyRef struct {
	Field            string          `json:"field"`
	Proof            Proof           `json:"proof"`
	SuppliedBy       string          `json:"supplied_by"`
	ReadBeforeActing json.RawMessage `json:"read_before_acting"`
	Confidence       string          `json:"confidence"`
}

type Identifier struct {
	Field      string `json:"field"`
	Purpose    string `json:"purpose"`
	Proof      Proof  `json:"proof"`
	CouldBeKey bool   `json:"could_be_key"`
}

type MainEntityAnswer struct {
	MainEntity       EntityRef    `json:"main_entity"`
	IdempotencyKey   KeyRef       `json:"idempotency_key"`
	OtherIdentifiers []Identifier `json:"other_identifiers"`

	NoKeyReason string `json:"no_key_reason"`
	Notes       string `json:"notes"`
}

func (a *MainEntityAnswer) Validate() error {
	var bad []string

	if a.MainEntity.Struct == "" {
		bad = append(bad, "main_entity.struct is empty: the flow moves something, name it")
	} else if a.MainEntity.Proof.Empty() {
		bad = append(bad, "main_entity was named but has no proof: every claim needs a file:line")
	}

	switch {
	case a.IdempotencyKey.Field == "" && a.NoKeyReason == "":

		bad = append(bad, "no idempotency key was named and no_key_reason is empty: "+
			"if there is no deduplication key, say what stops duplicates instead, or say that nothing does")
	case a.IdempotencyKey.Field != "" && a.IdempotencyKey.Proof.Empty():
		bad = append(bad, "idempotency_key.field was named but has no proof: "+
			"name the line it arrives on and the line it is read back on")
	case a.IdempotencyKey.Field != "" && a.IdempotencyKey.SuppliedBy == "generated":
		bad = append(bad, "a value this process generates cannot deduplicate anything, "+
			"because a retry produces a different one")
	}

	if key := a.IdempotencyKey.Field; key != "" {
		for _, o := range a.OtherIdentifiers {
			if !sameField(o.Field, key) {
				continue
			}
			if o.CouldBeKey {
				continue
			}
			bad = append(bad, fmt.Sprintf(
				"%q is named as the idempotency key and also listed under other_identifiers "+
					"with could_be_key false — decide which, or explain in `notes` why the two "+
					"spellings are different values", o.Field))
		}
	}

	for i, o := range a.OtherIdentifiers {
		switch {
		case o.Field == "":
			bad = append(bad, fmt.Sprintf("other_identifiers[%d] has no field name", i))
		case o.Purpose == "":
			bad = append(bad, fmt.Sprintf("%s: no purpose given — saying what it is FOR is how a "+
				"key gets chosen rather than picked", o.Field))
		case !hasCitation(o.Proof.At):
			bad = append(bad, fmt.Sprintf("%s: proof must name a file and line, got %q",
				o.Field, o.Proof.At))
		}
	}

	if len(bad) > 0 {
		return fmt.Errorf("%d problem(s):\n  - %s", len(bad), strings.Join(bad, "\n  - "))
	}
	return nil
}

var citation = regexp.MustCompile(`\.go:\d+`)

func hasCitation(s string) bool { return citation.MatchString(s) }

func sameField(a, b string) bool { return strings.EqualFold(a, b) }

type StateRolesAnswer struct {
	Roles      flowEntity.StateRoles `json:"roles"`
	Exceptions []struct {
		From string `json:"from"`
		To   string `json:"to"`
		Why  string `json:"why"`
	} `json:"exceptions"`
	Notes string `json:"notes"`
}

func (a *StateRolesAnswer) ValidateAgainst(states []string) error {
	declared := map[string]bool{}
	for _, s := range states {
		declared[s] = true
	}

	var bad []string
	seen := map[string]bool{}
	for _, r := range a.Roles {
		if !declared[r.State] {
			bad = append(bad, fmt.Sprintf("roles contains %q, which is not a declared state", r.State))
			continue
		}
		if seen[r.State] {
			bad = append(bad, fmt.Sprintf("%s appears more than once", r.State))
		}
		seen[r.State] = true
		if err := r.Validate(); err != nil {
			bad = append(bad, err.Error())
		}
		if r.RetryEntersAt != "" && !declared[r.RetryEntersAt] {
			bad = append(bad, fmt.Sprintf("%s: retry_enters_at names %q, which is not a declared state",
				r.State, r.RetryEntersAt))
		}
	}
	for _, s := range states {
		if !seen[s] {
			bad = append(bad, fmt.Sprintf("state %q is declared in the code but has no role", s))
		}
	}
	for _, e := range a.Exceptions {
		switch {
		case !declared[e.From] || !declared[e.To]:
			bad = append(bad, fmt.Sprintf("exception %s -> %s names a state that is not declared", e.From, e.To))
		case e.Why == "":
			bad = append(bad, fmt.Sprintf("exception %s -> %s has no reason; an exception without one "+
				"is indistinguishable from a mistake", e.From, e.To))
		}
	}

	if len(bad) > 0 {
		return fmt.Errorf("%d problem(s):\n  - %s", len(bad), strings.Join(bad, "\n  - "))
	}
	return nil
}

func (a *StateRolesAnswer) Transitions(neverAssigned []string) *TransitionsAnswer {
	out := &TransitionsAnswer{
		MayMoveTo: a.Roles.Derive(neverAssigned),
		Notes:     a.Notes,
	}
	if init, ok := a.Roles.Initial(); ok {
		out.InitialState = init
	}
	out.FinalStates = a.Roles.Finals()
	for _, e := range a.Exceptions {
		out.MayMoveTo[e.From] = appendOnce(out.MayMoveTo[e.From], e.To)
		out.FinalStateExceptions = append(out.FinalStateExceptions, struct {
			From string `json:"from"`
			To   string `json:"to"`
			Why  string `json:"why"`
		}{From: e.From, To: e.To, Why: e.Why})
	}
	return out
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}
