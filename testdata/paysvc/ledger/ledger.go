package ledger

import (
	"context"
	"database/sql"
)

type Ledger interface {
	Post(ctx context.Context, account string, cents int64) error
}

type SQLLedger struct{ DB *sql.DB }

func (l *SQLLedger) Post(ctx context.Context, account string, cents int64) error {
	_, err := l.DB.ExecContext(ctx,
		"insert into ledger_entries(account, cents) values($1,$2)", account, cents)
	return err
}
