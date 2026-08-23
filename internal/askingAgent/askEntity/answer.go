package askEntity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/katayunak/testigo/internal/testPlan/planEntity"
	"strings"

	"github.com/katayunak/testigo/internal/scanningFlow/flowEntity"

	"github.com/katayunak/testigo/internal/testPlan"
)

// KnowledgeSchema is the on-disk version of .testigo/knowledge.json.
const KnowledgeSchema = 1

// Knowledge is everything an agent told us, kept separate from everything the
// compiler proved.
//
// The separation is deliberate and is the most important structural decision in
// this package. flowEntity.Flow holds facts: reproducible, checkable, free to
// recompute. Knowledge holds context: expensive, fallible, and worth exactly as
// much as the reasoning behind it. Merging them into one file would make the
// report unable to tell a reader which of its claims are evidence and which are
// opinion — and for a tool whose only product is trust, that distinction is the
// product.
type Knowledge struct {
	SchemaVersion   int                              `json:"schema_version"`
	GeneratedAt     string                           `json:"generated_at,omitempty"`
	Binding         *BindingAnswer                   `json:"binding,omitempty"`
	Transitions     map[string]*TransitionsAnswer    `json:"transitions,omitempty"`
	ExternalEffects map[string]*ExternalEffectAnswer `json:"external_effects,omitempty"`
}

// FromEvidence fills in whatever phase 1 already proved, and reports what is
// left over.
//
// This is the answer to a fair criticism: round one was asking an agent which
// type is money while scanningFlow was already computing exactly that for its
// own findings. Paying for an answer you have is the worst kind of token spend,
// because it also introduces a chance of getting a WORSE answer than the one
// you threw away.
//
// The rule now: if the evidence decides it, it is a fact and no question is
// asked. If the evidence is close, the agent gets a narrow multiple-choice
// question with the evidence attached. Only a genuinely empty result produces an
// open question.
func FromEvidence(f *flowEntity.Flow) (*Knowledge, []string) {
	k := NewKnowledge()
	var unresolved []string

	money, moneyDecided := f.MoneyTypes.Decided()
	idem, idemDecided := f.IdempotencyKeys.Decided()

	if !moneyDecided && !idemDecided && len(f.MoneyTypes) == 0 && len(f.IdempotencyKeys) == 0 {
		return k, []string{"money type", "idempotency key"}
	}

	b := &BindingAnswer{}
	if moneyDecided {
		b.Money.Type = money.Owner + "." + money.Name
		b.Money.AmountField = money.Name
		b.Money.Representation = representationOf(money.Type)
		b.Money.Evidence = fmt.Sprintf("%s:%d", money.File, money.Line)
	} else {
		unresolved = append(unresolved, "money type")
	}

	if idemDecided {
		b.Idempotency.KeyField = idem.Name
		b.Idempotency.Evidence = fmt.Sprintf("%s:%d", idem.File, idem.Line)
		b.Idempotency.KeySource = "request"
		// Uniqueness is not a call the compiler cannot make. Either a migration constrains the
		// column or it does not, and infra.go already read the migrations.
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
		k.Binding = b
	}
	return k, unresolved
}

// representationOf reads how an amount is stored straight off its Go type. This
// was a question in round one, which was absurd: the type checker knows.
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

func NewKnowledge() *Knowledge {
	return &Knowledge{
		SchemaVersion:   KnowledgeSchema,
		Transitions:     map[string]*TransitionsAnswer{},
		ExternalEffects: map[string]*ExternalEffectAnswer{},
	}
}

// Evidence is a symbol plus the file:line that proves it exists. Every claim an
// agent makes about this repository carries one, so a reviewer can check it in
// seconds instead of taking it on faith.
type Evidence struct {
	Symbol   string `json:"symbol"`
	Evidence string `json:"evidence"`
}

type MoneyBinding struct {
	Type           string `json:"type"`
	AmountField    string `json:"amount_field"`
	Representation string `json:"representation"` // minor_units_int | decimal | float | unknown
	Currency       string `json:"currency"`
	Evidence       string `json:"evidence"`
}

type IdempotencyBinding struct {
	KeySource  string `json:"key_source"`
	KeyField   string `json:"key_field"`
	StoredIn   string `json:"stored_in"`
	Uniqueness string `json:"uniqueness"` // db_constraint | app_check_then_write | none | unknown
	Evidence   string `json:"evidence"`
}

type BindingAnswer struct {
	Money         MoneyBinding       `json:"money"`
	TransferFunc  Evidence           `json:"transfer_func"`
	BalanceFunc   Evidence           `json:"balance_func"`
	Idempotency   IdempotencyBinding `json:"idempotency"`
	EntityIDField string             `json:"entity_id_field"`
	Notes         string             `json:"notes"`
}

// Validate rejects answers that would silently produce a useless next round.
//
// The checks are shape checks, not truth checks — nothing here can tell whether
// the agent named the right function. What it can do is refuse an answer that
// is not even internally consistent, which catches the common failure of a
// model returning the example from the prompt.
func (a *BindingAnswer) Validate() error {
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
	if a.Money.Type != "" && a.Money.Evidence == "" {
		bad = append(bad, "money.type was named but money.evidence is empty: every claim needs a file:line")
	}
	if a.TransferFunc.Symbol != "" && a.TransferFunc.Evidence == "" {
		bad = append(bad, "transfer_func was named but has no evidence")
	}
	// The example values from the prompt coming back verbatim means the model
	// answered the illustration instead of the repository.
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

	// FinalStates are the states a payment cannot leave.
	//
	// Worth asking separately rather than inferring from an empty may_move_to
	// list, because the two mean different things. An empty list can mean "this
	// is the end" or "I could not work out what follows this". Only one of those
	// is safe to generate a test from.
	FinalStates []string `json:"final_states"`

	// FinalStateExceptions are the ways a final state can be left after all.
	//
	// This is where the rule earns its keep. A captured payment is finished,
	// until a chargeback arrives forty days later. A settled transfer is
	// finished, until it is recalled. Those exceptions are business rules, not
	// code facts, and a system that models one but tests it as impossible has a
	// test that will fail the first time reality happens.
	FinalStateExceptions []struct {
		From string `json:"from"`
		To   string `json:"to"`
		Why  string `json:"why"`
	} `json:"final_state_exceptions"`

	Notes string `json:"notes"`
}

// IsFinal reports whether a state is final with no declared exception.
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

// ValidateAgainst checks the answer covers exactly the states the compiler
// found. This is the payoff of asking a bounded question: completeness is
// mechanical, so a forgotten state is an error rather than a silent gap.
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
		// A state listed as final that also has outgoing transitions is a
		// contradiction, and it is the kind that produces a confidently wrong
		// test rather than an obviously broken one.
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

// Illegal returns the transitions the answer says must be impossible: every
// ordered pair not listed as allowed and not marked unsure. These become the
// generated tests — drive the payment into `from`, try to move it to `to`,
// assert it is refused.
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

// ExternalEffectAnswer describes what one call does to the world outside this
// process, and whether it can be taken back.
//
// Every field here is an OBSERVATION, not a recommendation. "Does a rollback
// undo this" has an answer in the code. "Should this be retried" does not — that
// is a decision the team makes, and testigo has no business making it for them.
//
// What a duplicate-effect test needs is the observation. If an effect survives a
// rollback and its outcome cannot be checked afterwards, then a client pressing
// refresh can make it happen twice, whether or not the server has a retry loop.
type ExternalEffectAnswer struct {
	Target string `json:"-"`

	// ChangesExternalState: does this leave a mark outside this process?
	ChangesExternalState json.RawMessage `json:"changes_external_state"`

	// ReversibleByRollback: does a database ROLLBACK undo it? For anything that
	// left the machine the answer is no, and that is the whole point.
	ReversibleByRollback json.RawMessage `json:"reversible_by_rollback"`

	// OutcomeObservable: after a timeout, can this code ASK whether the effect
	// happened? A provider with a status endpoint is a completely different
	// testing problem from one without.
	OutcomeObservable json.RawMessage `json:"outcome_observable"`

	// AcceptsDedupKey: does the call take a key the other side deduplicates on?
	AcceptsDedupKey  json.RawMessage `json:"accepts_dedup_key"`
	DedupKeyArgument string          `json:"dedup_key_argument"`

	// Undo is the compensating action, if one exists.
	Undo struct {
		Exists   bool   `json:"exists"`
		Symbol   string `json:"symbol"`
		Evidence string `json:"evidence"`
	} `json:"undo"`

	// MovesMoney is about THIS repository's meaning of money movement, which is
	// not always a transfer. In a service-activation system the money moves when
	// a status is reported to a settlement provider. The business rules file
	// says what counts here.
	MovesMoney json.RawMessage `json:"moves_money"`

	FailureModes []string `json:"failure_modes"`
	Basis        string   `json:"basis"`
	Notes        string   `json:"notes"`
}

// Undoable reports whether the effect can be taken back, defaulting to NO.
//
// The direction of every default in this type is the same, and it is chosen
// rather than accidental. Treating an irreversible effect as reversible means a
// missing test and money moved twice. Treating a reversible one as irreversible
// means one unnecessary test. Only an explicit `true` counts.
func (a *ExternalEffectAnswer) Undoable() bool { return IsTrue(a.ReversibleByRollback) }

// Observable reports whether the outcome can be checked after a timeout.
func (a *ExternalEffectAnswer) Observable() bool { return IsTrue(a.OutcomeObservable) }

// Deduplicated reports whether the far side dedups on a key this code sends.
func (a *ExternalEffectAnswer) Deduplicated() bool { return IsTrue(a.AcceptsDedupKey) }

// EscapesRollback reports whether this effect outlives a failed transaction,
// defaulting to TRUE when unknown.
func (a *ExternalEffectAnswer) EscapesRollback() bool {
	return !IsFalse(a.ChangesExternalState) && !IsTrue(a.ReversibleByRollback)
}

// IsTrue and IsFalse compare the literal token rather than unmarshalling.
//
// Unmarshalling looked correct and was dangerous. json.Unmarshal of the token
// `null` into a *bool returns a nil error and leaves the bool at false — so an
// answer of null, or a field the agent omitted entirely, read as an explicit
// "false, this call does not change anything outside the process". The unsafe
// direction was the silent default, which is the exact opposite of what the
// methods above promise. Comparing the token means only a real `true` is true,
// only a real `false` is false, and everything else — null, "unknown", missing,
// malformed — is neither.
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

// CaseAnswer is round two's answer for one test case.
type CaseAnswer struct {
	CaseID        string `json:"-"`
	Status        string `json:"status"` // written | blocked
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

// Validate checks the shape, then checks the emitted Go against the size the
// case was planned at.
//
// The size check is the part worth having. "This is a small test" is a claim in
// a comment; "this file opens no socket, never sleeps and does not read the wall
// clock" is a predicate over the syntax tree. Catching a violation here costs
// one retry. Not catching it costs someone chasing a flake months later, by
// which time nobody remembers the test was generated.
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
