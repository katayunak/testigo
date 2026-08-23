package webhook

import (
	"context"
	"database/sql"
	"net/http"

	"example.com/paysvc/domain"
	"example.com/paysvc/ledger"
)

type Handler struct {
	DB     *sql.DB
	Ledger ledger.Ledger
}

func (h *Handler) PSPCallback(w http.ResponseWriter, r *http.Request) {
	p := &domain.Payment{ID: r.URL.Query().Get("id")}
	go func() {
		_ = h.capture(context.Background(), p)
	}()
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) capture(ctx context.Context, p *domain.Payment) error {
	p.Status = domain.StatusCaptured
	p.Mode = domain.ModeAccepted
	if _, err := h.DB.ExecContext(ctx,
		"update payments set status=$1 where id=$2", p.Status, p.ID); err != nil {
		return err
	}
	return h.Ledger.Post(ctx, p.ID, p.FeeCents)
}
