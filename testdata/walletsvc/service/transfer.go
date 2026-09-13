package service

import (
	"context"
	"errors"
	"math"

	"example.com/walletsvc/domain"
	"example.com/walletsvc/repo"
)

type Service struct {
	Wallets repo.WalletRepository
	Core    repo.Notifier
}

func (s *Service) CoreTransfer(ctx context.Context, in domain.TransferInput) error {
	in.Amount = domain.Amount(math.Abs(float64(in.Amount)))

	from, err := s.Wallets.GetWallet(ctx, in.FromWalletID)
	if err != nil {
		return err
	}
	to, err := s.Wallets.GetWallet(ctx, in.ToWalletID)
	if err != nil {
		return err
	}

	if from.Balance < domain.Balance(in.Amount) {
		return errors.New("amount not enough")
	}

	if err := s.Wallets.UpdateBalance(ctx, from.ID, from.Balance-domain.Balance(in.Amount)); err != nil {
		return err
	}

	if err := s.Core.Settle(ctx, from.ID); err != nil {
		return err
	}

	return s.Wallets.UpdateBalance(ctx, to.ID, to.Balance+domain.Balance(in.Amount))
}

func (s *Service) Deposit(ctx context.Context, walletID string, amount domain.Amount, tracking string) error {
	w, err := s.Wallets.GetWallet(ctx, walletID)
	if err != nil {
		return err
	}

	max, err := s.Wallets.MaxBalance(ctx, w.WalletType)
	if err != nil {
		return err
	}
	if w.Balance.ToUint64()+uint64(amount) > max {
		return errors.New("max balance exceeded")
	}

	if err := s.Wallets.UpdateBalance(ctx, w.ID, w.Balance+domain.Balance(amount)); err != nil {
		return err
	}
	return s.Wallets.InsertTransaction(ctx, domain.Transaction{
		WalletID: w.ID, Amount: amount, TrackingNumber: tracking, Type: domain.TypeDeposit,
	})
}

func (s *Service) Withdraw(ctx context.Context, walletID string, amount domain.Amount) error {
	w, err := s.Wallets.GetWallet(ctx, walletID)
	if err != nil {
		return err
	}
	if !w.Active() {
		return errors.New("wallet not active")
	}
	return s.Wallets.Atomic(ctx, func(ctx context.Context) error {
		return s.Wallets.UpdateBalance(ctx, w.ID, w.Balance-domain.Balance(amount))
	})
}

func (s *Service) EnableWallet(ctx context.Context, walletID string) error {
	w, err := s.Wallets.GetWallet(ctx, walletID)
	if err != nil {
		return err
	}
	w.Status = domain.StatusEnable
	return s.Wallets.UpdateStatus(ctx, w.ID, w.Status)
}

func (s *Service) DeleteWallet(ctx context.Context, walletID string) error {
	w, err := s.Wallets.GetWallet(ctx, walletID)
	if err != nil {
		return err
	}
	w.Status = domain.StatusDeleted
	return s.Wallets.UpdateStatus(ctx, w.ID, w.Status)
}

func (s *Service) Fee(amount domain.Amount) domain.Amount { return amount / 100 }
