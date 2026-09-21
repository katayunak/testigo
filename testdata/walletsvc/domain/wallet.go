package domain

import "time"

type Amount int64

type Balance int64

func (b Balance) ToUint64() uint64 { return uint64(b) }

type Status string

const (
	StatusInitial Status = "initial"
	StatusEnable  Status = "enable"
	StatusDisable Status = "disable"
	StatusPending Status = "pending"
	StatusDeleted Status = "deleted"
)

type Wallet struct {
	ID         string
	UserID     string
	Account    string
	WalletType string
	Balance    Balance
	Status     Status
	CreatedAt  time.Time
}

func (w Wallet) Active() bool { return w.Status == StatusEnable }

type TransactionType string

const (
	TypeDeposit  TransactionType = "deposit"
	TypeWithdraw TransactionType = "withdraw"
	TypeTransfer TransactionType = "transfer"
)

type Transaction struct {
	ID             string
	WalletID       string
	TransferID     string
	PaymentID      string
	OrderID        string
	Rrn            string
	TrackingNumber string
	TerminalID     string
	Balance        Balance
	Amount         Amount
	Fee            uint64
	Type           TransactionType
	CreatedAt      time.Time
}

type TransferInput struct {
	FromWalletID string
	ToWalletID   string
	Amount       Amount
	Fee          uint64
}
