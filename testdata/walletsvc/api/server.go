package api

import (
	"context"

	"example.com/walletsvc/domain"
	"example.com/walletsvc/service"
)

type Server struct{ Svc *service.Service }

func (s *Server) Transfer(ctx context.Context, in domain.TransferInput) error {
	return s.Svc.CoreTransfer(ctx, in)
}

func (s *Server) Deposit(ctx context.Context, walletID string, amount domain.Amount, tracking string) error {
	return s.Svc.Deposit(ctx, walletID, amount, tracking)
}

func (s *Server) Withdraw(ctx context.Context, walletID string, amount domain.Amount) error {
	return s.Svc.Withdraw(ctx, walletID, amount)
}

func (s *Server) Enable(ctx context.Context, walletID string) error {
	return s.Svc.EnableWallet(ctx, walletID)
}

func (s *Server) Delete(ctx context.Context, walletID string) error {
	return s.Svc.DeleteWallet(ctx, walletID)
}
