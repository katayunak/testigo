package naming

var moneyField = NewVocab(
	"amount", "amt",
	"balance", "bal",
	"subtotal", "total",
	"price", "fee", "cost", "charge", "tax", "vat",
	"discount", "refund", "payout", "settlement",
	"credit", "debit",
	"money", "cash", "fund",
	"cents", "cent", "rial", "toman", "minor",
	"principal", "premium", "commission", "interest",
	"wage", "salary", "bonus", "penalty", "fine",
	"deposit", "withdraw", "withdrawal", "transfer",
)

var moneyType = NewVocab(
	"money", "amount", "currency", "decimal", "cents", "minor units", "minor unit",
	"price", "fee", "balance", "rial", "toman",
)

var currency = NewVocab("currency", "curr", "ccy", "iso4217", "currency code")

// counter words mark a number that counts things; TotalPages is not money even
// though it is a total.
var counter = NewVocab(
	"page", "pagination", "paging", "count", "counter",
	"index", "idx", "offset", "limit", "len", "length", "size", "num", "number",
	"row", "record", "item", "quantity", "qty", "capacity", "attempt", "retry",
	"second", "millis", "ms", "duration", "timeout", "ttl", "interval", "job", "lock",
)

// rate words mark a ratio. A fee rate in a float is a modelling choice worth
// a look, but it is not money stored in a float.
var rate = NewVocab(
	"rate", "ratio", "percent", "percentage", "pct", "bps", "basis point",
	"factor", "multiplier", "weight", "score", "probability",
)

var container = NewVocab(
	"paging", "pagination", "page", "meta", "metadata", "cursor", "filter",
	"query", "search", "config", "configuration", "setting", "option",
)

var idempotencyField = NewVocab(
	"idempotency", "idempotent", "idem",
	"request id", "reference id", "ref id", "refid",
	"correlation id", "trace id",
	"dedup", "deduplication",
	"client key", "client token",
	"unique key",
	"order id", "invoice id",
	"tracking number", "tracking no", "tracking code",
	"rrn", "retrieval reference", "stan",
	"transfer id", "payment id",
)

var stateStrong = NewVocab(
	"status", "state", "phase", "stage", "step", "lifecycle", "disposition",
	"transition", "progress", "stat", "sts", "situation", "position",
)

var stateWeak = NewVocab(
	"kind", "type", "code", "result", "outcome", "mode", "reason", "category",
	"class", "level", "flag", "action", "event", "operation", "direction",
)

var finalState = NewVocab(
	"completed", "complete", "success", "succeeded", "settled", "captured",
	"finished", "done", "closed", "failed", "failure", "cancelled", "canceled",
	"rejected", "declined", "expired", "voided", "refunded", "reversed",
	"abandoned", "terminated", "archived",
)

var failureState = NewVocab(
	"failed", "failure", "declined", "rejected", "error", "errored", "cancelled",
	"canceled", "expired", "voided", "aborted",
)

var reopeningState = NewVocab(
	"refund", "chargeback", "dispute", "reversal", "reverse", "adjust",
	"adjustment", "correction", "compensate", "compensation", "retry", "reopen", "recall",
)

// IsMoneyName: a field, parameter or variable named like an amount and not
// like a counter.
func IsMoneyName(name string) bool { return moneyField.Matches(name) && !counter.Matches(name) }

// IsMoneyAmountName additionally excludes ratios, for the claim "money is
// stored in a float".
func IsMoneyAmountName(name string) bool { return IsMoneyName(name) && !rate.Matches(name) }

func IsMoneyType(name string) bool       { return moneyType.MatchesLast(name) }
func IsCurrencyName(name string) bool    { return currency.Matches(name) }
func IsCounterName(name string) bool     { return counter.Matches(name) }
func IsRateName(name string) bool        { return rate.Matches(name) }
func IsContainerName(name string) bool   { return container.Matches(name) }
func IsIdempotencyName(name string) bool { return idempotencyField.Matches(name) }

// IsStateType: named like a lifecycle (PaymentStatus).
func IsStateType(name string) bool { return stateStrong.MatchesLast(name) }

// IsWeakStateType: named like an enum that may or may not be a lifecycle
// (SettlementMode). Promotion needs behaviour, not more name.
func IsWeakStateType(name string) bool {
	return !stateStrong.MatchesLast(name) && stateWeak.MatchesLast(name)
}

func IsFinalState(name string) bool     { return finalState.Matches(name) }
func IsFailureState(name string) bool   { return failureState.Matches(name) }
func IsReopeningState(name string) bool { return reopeningState.Matches(name) }
