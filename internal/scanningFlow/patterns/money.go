package patterns

var MoneyField = anyOf(
	"amount", "amt",
	"balance", "bal",
	"subtotal", "total",
	"price", "fee", "cost", "charge", "tax", "vat",
	"discount", "refund", "payout", "settlement",
	"credit", "debit",
	"money", "cash", "fund", "funds",
	"cents", "rials", "toman", "minor",
	"principal", "premium", "commission", "interest",
	"wage", "salary", "bonus", "penalty", "fine",
	"deposit", "withdraw", "transfer",
)

var MoneyType = MustEndIn(
	"money", "amount", "currency", "decimal", "cents", "minorunits", "minorunit",
	"price", "fee", "balance", "rial", "toman",
)

func MustEndIn(words ...string) *anchored { return &anchored{re: endingWith(words...)} }

type anchored struct {
	re interface{ MatchString(string) bool }
}

func (a *anchored) MatchString(s string) bool { return a.re.MatchString(s) }

var CurrencyField = anyOf("currency", "curr", "ccy", "iso4217")

var IdempotencyField = anyOf(
	"idempotency", "idempotent", "idem",
	"requestid", "request_id",
	"referenceid", "reference_id", "refid", "ref_id",
	"correlationid", "correlation_id",
	"traceid", "trace_id",
	"dedup", "deduplication",
	"clientkey", "client_key", "clienttoken",
	"uniquekey", "unique_key",
	"orderid", "order_id", "invoiceid", "invoice_id",
)
