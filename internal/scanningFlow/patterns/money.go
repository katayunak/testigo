package patterns

// Money naming.
//
// Deliberately excludes generic words like "value", "count", "number" and
// "size". A false positive here is worse than a miss: it trains a reader to
// scroll past the findings list, and then they scroll past the real one too.

// MoneyField matches an identifier that almost certainly holds an amount.
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

// MoneyType matches a named type that IS money by construction.
var MoneyType = MustEndIn(
	"money", "amount", "currency", "decimal", "cents", "minorunits", "minorunit",
	"price", "fee", "balance", "rial", "toman",
)

// MustEndIn is exported so the business rules file can add repository-specific
// type names without editing this package.
func MustEndIn(words ...string) *anchored { return &anchored{re: endingWith(words...)} }

type anchored struct {
	re interface{ MatchString(string) bool }
}

func (a *anchored) MatchString(s string) bool { return a.re.MatchString(s) }

// CurrencyField matches the field that pairs with an amount. An amount without
// one is not money, it is a number, and nothing stops a caller adding EUR minor
// units to USD minor units.
var CurrencyField = anyOf("currency", "curr", "ccy", "iso4217")

// IdempotencyField matches the field carrying a deduplication key.
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
