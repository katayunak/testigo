// Package testPlan is the entity layer between what phase 1 proved and what an
// agent is asked to write.
//
// The order matters and it is the opposite of the obvious one. A prompt is not
// written by hand and then made to fit a test; a TestCase is assembled from
// typed data and the prompt is RENDERED from it, last. Everything that decides
// what the test should be — the scenario, the technique, what counts as passing,
// what a wrong test would look like — lives here as Go values, where it costs
// nothing, can be tested, and can be reviewed without reading prose.
//
// Only the final render costs tokens.
package planEntity

// Technique is what kind of test this is: the method, not its scope.
//
// Choosing the technique as the enum is a deliberate reading of a genuine
// disagreement in the literature. Fowler's pyramid is organised by SCOPE, which
// is weakly actionable — scope is a consequence of what you generate, not an
// input to it. Google organises by SIZE, defined as what the process is allowed
// to touch, which is strongly actionable but orthogonal: a property-based test
// can be small or large.
//
// So technique is the enum, and size and scope are fields. Google is explicit
// that the two are independent axes, and for the same reason we need them to be:
// "the most important qualities we want from our test suite are speed and
// determinism, regardless of the scope of the test."
//
//	https://abseil.io/resources/swe-book/html/ch11.html
type Technique string

const (
	// TechniqueUnit tests one unit with its collaborators faked.
	TechniqueUnit Technique = "unit"

	// TechniqueTable is a unit test over an enumerated input table. Separated
	// from plain unit because it is the right shape for anything with a finite,
	// knowable domain — currency exponents, error classifications, state pairs —
	// and because a generator can fill the table from data rather than invent it.
	TechniqueTable Technique = "table"

	// TechniqueNarrowIntegration exercises the code against a real instance of
	// ONE external thing, usually a container. Fowler's "narrow" integration
	// test: real database, everything else faked.
	//
	// This is the only technique that can observe database isolation behaviour.
	// An in-memory fake cannot exhibit READ COMMITTED, so a lost-update test
	// written as a unit test passes on broken code — which is worse than not
	// writing it.
	TechniqueNarrowIntegration Technique = "narrowIntegration"

	// TechniqueEndToEnd drives the whole system through its real entry point.
	TechniqueEndToEnd Technique = "endToEnd"

	// TechniqueProperty asserts a relation that must hold for ALL inputs, with
	// the inputs generated. The technique of choice for ledger invariants:
	// Nubank describes generating "thousands of examples" of random initial
	// state plus a random event and checking the invariants every time.
	TechniqueProperty Technique = "property"

	// TechniqueMetamorphic asserts a relation BETWEEN runs rather than an
	// absolute output. Used where the correct answer is unknown but the
	// relationship between two answers is not: replaying the same events in a
	// different order must reach the same state.
	TechniqueMetamorphic Technique = "metamorphic"

	// TechniqueFuzz feeds generated input to find crashes and precision loss.
	// `go test -fuzz` makes this nearly free in Go.
	TechniqueFuzz Technique = "fuzz"

	// TechniqueStateMachine drives the system through sequences of operations
	// and checks the state machine's rules after every step.
	TechniqueStateMachine Technique = "stateMachine"

	// TechniqueFaultInjection makes a specific dependency fail at a specific
	// point, deterministically and by script. Distinct from chaos, which is
	// random and belongs in production, not in a generated file.
	TechniqueFaultInjection Technique = "faultInjection"

	// TechniqueConcurrency runs operations in parallel from a barrier and
	// asserts a correctness property, not a latency number.
	TechniqueConcurrency Technique = "concurrency"

	// TechniqueDifferential runs two implementations over the same inputs and
	// asserts they agree. Needs a reference to compare against.
	TechniqueDifferential Technique = "differential"

	// TechniqueGolden compares output against a committed expected file.
	TechniqueGolden Technique = "golden"
)

// Size is Google's axis: what the test process is allowed to touch.
//
// This is kept because it is the only classification in the literature that is a
// CHECKABLE PREDICATE on emitted code rather than a description of intent. A
// generated file that claims to be small can be verified small by looking for
// net, os, time.Sleep and unseeded randomness — see sizeCheck.go. A file that
// claims to be a "unit test" can only be taken at its word.
//
//	https://testing.googleblog.com/2010/12/test-sizes.html
type Size string

const (
	// SizeSmall: one process, no network, no disk, no sleeping, no real clock.
	// Deterministic by construction, and testigo enforces it statically.
	SizeSmall Size = "small"

	// SizeMedium: one machine. localhost network and a real database are
	// allowed. Emitted behind a build tag so a bare `go test ./...` stays fast.
	SizeMedium Size = "medium"

	// SizeLarge: multiple machines, real external systems. testigo does not
	// generate these — it cannot know your environment, and a generated test
	// that talks to a real payment provider is a bad idea in every direction.
	SizeLarge Size = "large"
)

// BuildTag returns the constraint a test of this size is emitted behind, so that
// size becomes a static property of the file rather than a claim in a comment.
func (s Size) BuildTag() string {
	if s == SizeSmall {
		return ""
	}
	return "integration"
}

// Scope is Fowler's axis: how much code is under test. Descriptive only — it is
// reported, never enforced.
type Scope string

const (
	ScopeFunction Scope = "function"
	ScopeUnit     Scope = "unit"
	ScopeService  Scope = "service"
	ScopeSystem   Scope = "system"
)

// Role is what the test is FOR, which is independent of how it is written.
type Role string

const (
	// RoleSpecification asserts what must be true. Can find a bug.
	RoleSpecification Role = "specification"

	// RoleCharacterization locks in what the code currently does, so a refactor
	// is detectably behaviour-preserving. Cannot find a bug, by construction.
	RoleCharacterization Role = "characterization"

	// RoleRegression pins a specific bug so it cannot come back.
	RoleRegression Role = "regression"

	// RoleSmoke checks the thing runs at all.
	RoleSmoke Role = "smoke"
)

// OracleProvenance is where the expected value came from, and it is the most
// important field in this package.
//
// An oracle is whatever tells the test what "correct" means. Its source decides
// whether the test can find a bug at all:
//
//   - An invariant from the domain ("money is conserved") holds whatever the
//     code does, so a buggy implementation fails it.
//   - A value read out of the implementation holds by definition, so a buggy
//     implementation passes it. The bug becomes the assertion.
//
// That second case is the characteristic failure of generated tests, and it is
// quiet: the suite is green, so nobody looks. Every prompt this package renders
// states its oracle provenance explicitly, and OracleCurrentBehavior is refused
// outright for anything touching money.
type OracleProvenance string

const (
	// OracleSpecification: a published contract. Stripe's idempotency rules,
	// Adyen's currency exponents, an RFC.
	OracleSpecification OracleProvenance = "specification"

	// OracleInvariant: a property of the domain. Debits equal credits. No
	// balance goes negative. The strongest oracle available, because it is true
	// independently of any implementation.
	OracleInvariant OracleProvenance = "invariant"

	// OracleMetamorphic: a relation between two runs. Neither answer needs to be
	// known for the relation to be checkable.
	OracleMetamorphic OracleProvenance = "metamorphic"

	// OracleReference: a second implementation, deliberately simple.
	OracleReference OracleProvenance = "reference"

	// OracleHumanApproved: a person read the value and signed it off.
	OracleHumanApproved OracleProvenance = "humanApproved"

	// OracleCurrentBehavior: whatever the code does today. Legitimate for a
	// refactoring safety net and ONLY that. Never for money arithmetic.
	OracleCurrentBehavior OracleProvenance = "currentBehavior"
)

// CanFindBugs reports whether a test with this oracle is capable of failing on
// incorrect code.
func (o OracleProvenance) CanFindBugs() bool { return o != OracleCurrentBehavior }

// Properties describes what a technique can and cannot do. Attached to the
// technique so the prompt can state the limits rather than hoping the agent
// knows them.
type Properties struct {
	Technique Technique
	// Uniquely is what this technique catches that no cheaper one can.
	Uniquely string
	// Cannot is what it will never catch. This is the field most often left out
	// of a taxonomy, and it is the one that produces good anti-goals.
	Cannot string
	// Needs is the infrastructure required.
	Needs string
	// FailureMode is how this technique goes wrong in practice.
	FailureMode string
	// Deterministic reports whether a correct implementation always passes.
	Deterministic bool
	// DefaultSize is the smallest size this technique usually fits in.
	DefaultSize Size
}

var techniqueProperties = map[Technique]Properties{
	TechniqueUnit: {
		Uniquely:      "pins one function's behaviour with no setup cost, so it runs on every save",
		Cannot:        "see how the pieces fit together, or any behaviour that only exists in a real database or across a network",
		Needs:         "nothing beyond fakes",
		FailureMode:   "passes while the system is broken, because every collaborator was faked into agreeing",
		Deterministic: true, DefaultSize: SizeSmall,
	},
	TechniqueTable: {
		Uniquely:      "covers a finite domain exhaustively, so a missing case is visible as a missing row",
		Cannot:        "find anything outside the enumerated rows",
		Needs:         "nothing",
		FailureMode:   "the table is filled from the implementation rather than the specification, and encodes the same mistake",
		Deterministic: true, DefaultSize: SizeSmall,
	},
	TechniqueNarrowIntegration: {
		Uniquely:      "observes real database behaviour: isolation levels, constraint violations, lock contention, actual SQL errors",
		Cannot:        "run fast, or run at all without a container",
		Needs:         "a real dependency, usually via testcontainers-go",
		FailureMode:   "slow enough that people stop running it locally",
		Deterministic: true, DefaultSize: SizeMedium,
	},
	TechniqueEndToEnd: {
		Uniquely:      "proves the wiring is right, which every narrower test assumes",
		Cannot:        "tell you WHERE the failure is when it goes red",
		Needs:         "the whole system running",
		FailureMode:   "brittle and non-deterministic; Fowler's specific objection to the top of the pyramid",
		Deterministic: false, DefaultSize: SizeMedium,
	},
	TechniqueProperty: {
		Uniquely:      "finds inputs a person would never think to write down, and shrinks a failure to a minimal counterexample",
		Cannot:        "check a property nobody stated; it needs an invariant to test",
		Needs:         "a generator and an invariant",
		FailureMode:   "the property is too weak to fail, so thousands of cases pass and prove nothing",
		Deterministic: true, DefaultSize: SizeSmall,
	},
	TechniqueMetamorphic: {
		Uniquely:      "tests correctness where the right answer is unknown, by asserting a relation between two runs",
		Cannot:        "detect a bug that affects both runs equally",
		Needs:         "a stated relation",
		FailureMode:   "the relation is assumed rather than true — order-independence is the usual mistake, and real ledgers are often order-DEPENDENT",
		Deterministic: true, DefaultSize: SizeSmall,
	},
	TechniqueFuzz: {
		Uniquely:      "finds panics, precision loss and overflow from inputs nobody imagined",
		Cannot:        "check business logic; it only knows about crashes and whatever you assert",
		Needs:         "go test -fuzz and a seed corpus",
		FailureMode:   "runs forever finding nothing because the assertion is only 'did not panic'",
		Deterministic: false, DefaultSize: SizeSmall,
	},
	TechniqueStateMachine: {
		Uniquely:      "finds sequences of legal operations that together reach an illegal state",
		Cannot:        "find anything about a single operation in isolation",
		Needs:         "a model of the legal transitions",
		FailureMode:   "the model is copied from the implementation, so both are wrong in the same way",
		Deterministic: true, DefaultSize: SizeSmall,
	},
	TechniqueFaultInjection: {
		Uniquely:      "exercises the error paths, which is where payment bugs live and where coverage is always lowest",
		Cannot:        "inject anything at a seam that is a concrete type",
		Needs:         "interfaces at the I/O boundaries",
		FailureMode:   "the injected fault never actually fires and the test passes green having done nothing",
		Deterministic: true, DefaultSize: SizeSmall,
	},
	TechniqueConcurrency: {
		Uniquely:      "finds lost updates, races and non-atomic pairs that are invisible to any sequential test",
		Cannot:        "prove absence: a race that did not happen this run may still happen next run",
		Needs:         "a barrier, and -race",
		FailureMode:   "the goroutines never actually overlapped, so it is green and meaningless",
		Deterministic: false, DefaultSize: SizeSmall,
	},
	TechniqueDifferential: {
		Uniquely:      "catches disagreement between an implementation and a simple model, including error codes",
		Cannot:        "run without a second implementation to compare against",
		Needs:         "a reference model",
		FailureMode:   "the reference is derived from the implementation and agrees with its bugs",
		Deterministic: true, DefaultSize: SizeSmall,
	},
	TechniqueGolden: {
		Uniquely:      "detects any unintended change to an output format, down to a byte",
		Cannot:        "tell whether the committed golden file was ever correct",
		Needs:         "a committed expected file",
		FailureMode:   "regenerated with -update whenever it goes red, which turns it into no test at all",
		Deterministic: true, DefaultSize: SizeSmall,
	},
}

// Props returns what is known about a technique.
func (t Technique) Props() Properties {
	p := techniqueProperties[t]
	p.Technique = t
	return p
}
