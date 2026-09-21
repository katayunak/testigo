package patterns

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
	"stat",
	"sts",
	"situation",
	"position",
)

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

func IsStateType(name string) bool { return StateStrong.MatchString(name) }

func IsWeakStateType(name string) bool {
	return !StateStrong.MatchString(name) && StateWeak.MatchString(name)
}

var FinalStateHint = anyOf(
	"completed", "complete", "success", "succeeded", "settled", "captured",
	"finished", "done", "closed", "failed", "failure", "cancelled", "canceled",
	"rejected", "declined", "expired", "voided", "refunded", "reversed",
	"abandoned", "terminated", "archived",
)

var ReopeningState = anyOf(
	"refund", "chargeback", "dispute", "reversal", "reverse", "adjust",
	"adjustment", "correction", "compensat", "retry", "reopen", "recall",
)
