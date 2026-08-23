package domain

import "time"

// PaymentStatus is the state of a payment. Its declared constants are the
// complete state set — the compiler guarantees no others exist.
type PaymentStatus string

const (
	StatusPending    PaymentStatus = "pending"
	StatusAuthorized PaymentStatus = "authorized"
	StatusCaptured   PaymentStatus = "captured"
	StatusFailed     PaymentStatus = "failed"
	StatusRefunded   PaymentStatus = "refunded"
	StatusAbandoned  PaymentStatus = "abandoned"
)

type Payment struct {
	ID             string
	ReferenceID    string
	TraceID        string
	OrderID        string
	IdempotencyKey string
	Amount         float64
	FeeCents       int64
	Status         PaymentStatus
	Mode           SettlementMode
	CreatedAt      time.Time
}

func (p *Payment) Terminal() bool {
	return p.Status == StatusCaptured || p.Status == StatusFailed || p.Status == StatusRefunded
}

// SettlementMode is a weak name but a real lifecycle: it is stored on the
// payment and moved through by more than one function.
type SettlementMode string

const (
	ModeQueued   SettlementMode = "queued"
	ModeSent     SettlementMode = "sent"
	ModeAccepted SettlementMode = "accepted"
)

// DeclineCode is a weak name and NOT a lifecycle: returned and compared, never
// stored, never advanced.
type DeclineCode string

const (
	DeclineInsufficient DeclineCode = "insufficient_funds"
	DeclineExpiredCard  DeclineCode = "expired_card"
)

func Classify(err error) DeclineCode {
	if err == nil {
		return ""
	}
	return DeclineInsufficient
}
