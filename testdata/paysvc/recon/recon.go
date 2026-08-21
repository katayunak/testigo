package recon

import (
	"context"
	"database/sql"
	"time"

	"example.com/paysvc/domain"
	"example.com/paysvc/psp"
)

type Job struct {
	DB  *sql.DB
	PSP psp.Gateway
}

func (j *Job) Reconcile(ctx context.Context) error {
	cutoff := time.Now().Add(-30 * time.Minute)
	rows, err := j.DB.QueryContext(ctx,
		"select id from payments where status=$1 and created_at < $2",
		domain.StatusPending, cutoff)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if _, err := j.PSP.Authorize(ctx, id, 0); err != nil {
			return err
		}
		p := &domain.Payment{ID: id}
		p.Status = domain.StatusFailed
		if _, err := j.DB.ExecContext(ctx, "update payments set status=$1 where id=$2", p.Status, id); err != nil {
			return err
		}
	}
	return rows.Err()
}
