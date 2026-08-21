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
	IdempotencyKey string
	Amount         float64
	FeeCents       int64
	Status         PaymentStatus
	CreatedAt      time.Time
}

func (p *Payment) Terminal() bool {
	return p.Status == StatusCaptured || p.Status == StatusFailed || p.Status == StatusRefunded
}
