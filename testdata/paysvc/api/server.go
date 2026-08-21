package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"example.com/paysvc/domain"
	"example.com/paysvc/ledger"
	"example.com/paysvc/psp"
)

type Server struct {
	DB     *sql.DB
	Ledger ledger.Ledger
	PSP    psp.Gateway
}

func (s *Server) CreatePayment(w http.ResponseWriter, r *http.Request) {
	p := &domain.Payment{
		ID:             r.Header.Get("X-Request-Id"),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		Amount:         12.34,
		CreatedAt:      time.Now(),
	}
	if err := s.process(r.Context(), p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) process(ctx context.Context, p *domain.Payment) error {
	seen, err := s.alreadySeen(ctx, p.IdempotencyKey)
	if err != nil {
		return err
	}
	if seen {
		return nil
	}

	p.Status = domain.StatusPending

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	cents := int64(p.Amount * 100)
	if _, err := tx.ExecContext(ctx,
		"insert into payments(id, idem_key, cents, status) values($1,$2,$3,$4)",
		p.ID, p.IdempotencyKey, cents, p.Status); err != nil {
		return err
	}

	ref, err := s.PSP.Authorize(ctx, p.ID, cents)
	if err != nil {
		return err
	}
	if ref == "" {
		return errors.New("no auth ref")
	}

	p.Status = domain.StatusAuthorized
	if err := s.Ledger.Post(ctx, p.ID, -cents); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) alreadySeen(ctx context.Context, key string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		"select count(*) from payments where idem_key=$1", key).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Server) splitFee(totalCents int64, parts int64) int64 {
	return totalCents / parts
}

func (s *Server) applyDiscount(amount float64, pct float64) float64 {
	return amount - (amount * pct / 100)
}

var _ = (&Server{}).splitFee
var _ = (&Server{}).applyDiscount
