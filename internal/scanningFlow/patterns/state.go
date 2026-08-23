package patterns

// State type naming.
//
// testigo looks for a named type whose underlying type is a string or an integer
// and which has declared constants. That combination is Go's way of writing an
// enum, and a payment lifecycle is almost always written that way.
//
// The type NAME is the only signal available for telling a lifecycle enum apart
// from any other enum. `PaymentStatus` is a lifecycle. `ErrorCode` is not, even
// though both are named string types with constants. Getting this wrong in
// either direction is expensive:
//
//   - too narrow, and testigo silently reports no state machine, which removes
//     the transition questions, the illegal-transition tests and the final-state
//     tests all at once
//   - too wide, and it asks an agent which transitions of an ErrorCode are legal,
//     which is nonsense and costs money
//
// So there are two lists.

// StateStrong are words that mean "where this thing is in its life". A type
// ending in one of these is treated as a state machine without further question.
var StateStrong = endingWith(
	"status",
	"state",
	"phase",
	"stage",
	"step",
	"lifecycle",
	"disposition",
	"transition",
	"progress",
	"stat", // common abbreviation
	"sts",  // common abbreviation in payment switches
	"situation",
	"position", // as in "position in the flow"
)

// StateWeak are words that mark an enum which MIGHT be a lifecycle. A refund
// reason, a transaction kind and a settlement mode are all enums, and some of
// them do have ordering rules, but most do not.
//
// These are reported so a person can see them, and they are NOT sent to an agent
// as transition questions unless the business rules file says to include them.
// That keeps the common case cheap while leaving the door open for a repository
// where `SettlementMode` really does have a legal order.
var StateWeak = endingWith(
	"kind",
	"type",
	"code",
	"result",
	"outcome",
	"mode",
	"reason",
	"category",
	"class",
	"level",
	"flag",
	"action",
	"event",
	"operation",
	"direction",
)

// IsStateType reports whether a named type is a lifecycle by its name.
func IsStateType(name string) bool { return StateStrong.MatchString(name) }

// IsWeakStateType reports whether a name is an enum that may or may not be a
// lifecycle.
func IsWeakStateType(name string) bool {
	return !StateStrong.MatchString(name) && StateWeak.MatchString(name)
}

// FinalStateHint are constant names that usually mean "this is the end".
//
// Only a hint. Which states are truly final is a business rule, and testigo asks
// rather than assumes. But the hint is worth having: it lets the prompt say
// "these three look final to me, confirm or correct" instead of asking an open
// question, and a narrower question gets a better answer.
var FinalStateHint = anyOf(
	"completed", "complete", "success", "succeeded", "settled", "captured",
	"finished", "done", "closed", "failed", "failure", "cancelled", "canceled",
	"rejected", "declined", "expired", "voided", "refunded", "reversed",
	"abandoned", "terminated", "archived",
)

// ReopeningState are states that can follow a state that looked final. These are
// the exceptions that make "final means final" a question rather than a rule: a
// captured payment is done, until a chargeback arrives.
var ReopeningState = anyOf(
	"refund", "chargeback", "dispute", "reversal", "reverse", "adjust",
	"adjustment", "correction", "compensat", "retry", "reopen", "recall",
)
