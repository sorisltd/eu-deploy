package analytics

import (
	"context"
	"time"
)

func Aggregate(ctx context.Context, cfg Config, targetDate time.Time) error {
	db, err := OpenDatabase(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := EnsureSchema(ctx, db); err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, AggregateSQL(targetDate))
	return err
}
