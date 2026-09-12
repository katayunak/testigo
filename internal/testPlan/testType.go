package testPlan

type Technique string

const (
	TechniqueUnit Technique = "unit"

	TechniqueTable Technique = "table"

	TechniqueNarrowIntegration Technique = "narrowIntegration"

	TechniqueEndToEnd Technique = "endToEnd"

	TechniqueProperty Technique = "property"

	TechniqueMetamorphic Technique = "metamorphic"

	TechniqueFuzz Technique = "fuzz"

	TechniqueStateMachine Technique = "stateMachine"

	TechniqueFaultInjection Technique = "faultInjection"

	TechniqueConcurrency Technique = "concurrency"

	TechniqueDifferential Technique = "differential"

	TechniqueGolden Technique = "golden"
)

type Size string

const (
	SizeSmall Size = "small"

	SizeMedium Size = "medium"

	SizeLarge Size = "large"
)

func (s Size) BuildTag() string {
	if s == SizeSmall {
		return ""
	}
	return "integration"
}

type OracleProvenance string

const (
	OracleSpecification OracleProvenance = "specification"

	OracleInvariant OracleProvenance = "invariant"

	OracleMetamorphic OracleProvenance = "metamorphic"

	OracleReference OracleProvenance = "reference"

	OracleHumanApproved OracleProvenance = "humanApproved"

	OracleCurrentBehavior OracleProvenance = "currentBehavior"
)

func (o OracleProvenance) CanFindBugs() bool { return o != OracleCurrentBehavior }

type Properties struct {
	Uniquely string

	Cannot string

	FailureMode string

	DefaultSize Size
}

var techniqueProperties = map[Technique]Properties{
	TechniqueUnit: {
		Uniquely:    "pins one function's behaviour with no setup cost, so it runs on every save",
		Cannot:      "see how the pieces fit together, or any behaviour that only exists in a real database or across a network",
		FailureMode: "passes while the system is broken, because every collaborator was faked into agreeing",
		DefaultSize: SizeSmall,
	},
	TechniqueTable: {
		Uniquely:    "covers a finite domain exhaustively, so a missing case is visible as a missing row",
		Cannot:      "find anything outside the enumerated rows",
		FailureMode: "the table is filled from the implementation rather than the specification, and encodes the same mistake",
		DefaultSize: SizeSmall,
	},
	TechniqueNarrowIntegration: {
		Uniquely:    "observes real database behaviour: isolation levels, constraint violations, lock contention, actual SQL errors",
		Cannot:      "run fast, or run at all without a container",
		FailureMode: "slow enough that people stop running it locally",
		DefaultSize: SizeMedium,
	},
	TechniqueEndToEnd: {
		Uniquely:    "proves the wiring is right, which every narrower test assumes",
		Cannot:      "tell you WHERE the failure is when it goes red",
		FailureMode: "brittle and non-deterministic; Fowler's specific objection to the top of the pyramid",
		DefaultSize: SizeMedium,
	},
	TechniqueProperty: {
		Uniquely:    "finds inputs a person would never think to write down, and shrinks a failure to a minimal counterexample",
		Cannot:      "check a property nobody stated; it needs an invariant to test",
		FailureMode: "the property is too weak to fail, so thousands of cases pass and prove nothing",
		DefaultSize: SizeSmall,
	},
	TechniqueMetamorphic: {
		Uniquely:    "tests correctness where the right answer is unknown, by asserting a relation between two runs",
		Cannot:      "detect a bug that affects both runs equally",
		FailureMode: "the relation is assumed rather than true — order-independence is the usual mistake, and real ledgers are often order-DEPENDENT",
		DefaultSize: SizeSmall,
	},
	TechniqueFuzz: {
		Uniquely:    "finds panics, precision loss and overflow from inputs nobody imagined",
		Cannot:      "check business logic; it only knows about crashes and whatever you assert",
		FailureMode: "runs forever finding nothing because the assertion is only 'did not panic'",
		DefaultSize: SizeSmall,
	},
	TechniqueStateMachine: {
		Uniquely:    "finds sequences of legal operations that together reach an illegal state",
		Cannot:      "find anything about a single operation in isolation",
		FailureMode: "the model is copied from the implementation, so both are wrong in the same way",
		DefaultSize: SizeSmall,
	},
	TechniqueFaultInjection: {
		Uniquely:    "exercises the error paths, which is where payment bugs live and where coverage is always lowest",
		Cannot:      "inject anything at a seam that is a concrete type",
		FailureMode: "the injected fault never actually fires and the test passes green having done nothing",
		DefaultSize: SizeSmall,
	},
	TechniqueConcurrency: {
		Uniquely:    "finds lost updates, races and non-atomic pairs that are invisible to any sequential test",
		Cannot:      "prove absence: a race that did not happen this run may still happen next run",
		FailureMode: "the goroutines never actually overlapped, so it is green and meaningless",
		DefaultSize: SizeSmall,
	},
	TechniqueDifferential: {
		Uniquely:    "catches disagreement between an implementation and a simple model, including error codes",
		Cannot:      "run without a second implementation to compare against",
		FailureMode: "the reference is derived from the implementation and agrees with its bugs",
		DefaultSize: SizeSmall,
	},
	TechniqueGolden: {
		Uniquely:    "detects any unintended change to an output format, down to a byte",
		Cannot:      "tell whether the committed golden file was ever correct",
		FailureMode: "regenerated with -update whenever it goes red, which turns it into no test at all",
		DefaultSize: SizeSmall,
	},
}

func (t Technique) Props() Properties { return techniqueProperties[t] }
