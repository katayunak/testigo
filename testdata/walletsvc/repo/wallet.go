package repo

import (
	"context"
	"database/sql"

	"example.com/walletsvc/domain"
)

type WalletRepository interface {
	GetWallet(ctx context.Context, id string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, id string, balance domain.Balance) error
	UpdateStatus(ctx context.Context, id string, status domain.Status) error
	InsertTransaction(ctx context.Context, t domain.Transaction) error
	MaxBalance(ctx context.Context, walletType string) (uint64, error)
	Atomic(ctx context.Context, fn func(context.Context) error) error
}

type Notifier interface {
	Settle(ctx context.Context, walletID string) error
}

type Store struct{ DB *sql.DB }

func (s *Store) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	row := s.DB.QueryRowContext(ctx, "SELECT id, wallet_type, balance, status FROM wallets WHERE id = $1", id)
	var w domain.Wallet
	if err := row.Scan(&w.ID, &w.WalletType, &w.Balance, &w.Status); err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *Store) UpdateBalance(ctx context.Context, id string, balance domain.Balance) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE wallets SET balance = $1 WHERE id = $2", balance, id)
	return err
}

func (s *Store) UpdateStatus(ctx context.Context, id string, status domain.Status) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE wallets SET status = $1 WHERE id = $2", status, id)
	return err
}

func (s *Store) InsertTransaction(ctx context.Context, t domain.Transaction) error {
	_, err := s.DB.ExecContext(ctx,
		"INSERT INTO transactions (id, wallet_id, transfer_id, tracking_number, amount, type) VALUES ($1,$2,$3,$4,$5,$6)",
		t.ID, t.WalletID, t.TransferID, t.TrackingNumber, t.Amount, t.Type)
	return err
}

func (s *Store) MaxBalance(ctx context.Context, walletType string) (uint64, error) {
	row := s.DB.QueryRowContext(ctx, "SELECT max_balance FROM settings WHERE wallet_type = $1", walletType)
	var v uint64
	err := row.Scan(&v)
	return v, err
}

func (s *Store) Atomic(ctx context.Context, fn func(context.Context) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(ctx); err != nil {
		return err
	}
	return tx.Commit()
}
