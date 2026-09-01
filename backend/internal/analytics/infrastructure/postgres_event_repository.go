package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/analytics/domain"
)

type PostgresEventRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresEventRepository(pool *pgxpool.Pool) *PostgresEventRepository {
	return &PostgresEventRepository{pool: pool}
}

func (r *PostgresEventRepository) Record(ctx context.Context, accountID string, events []domain.Event) (int, error) {
	batch := &pgx.Batch{}
	for _, event := range events {
		batch.Queue(`
			INSERT INTO analytics_events (
				event_uuid, name, account_id,
				step, category_id, result_bucket, outcome, reason,
				surface, classification, retried, retry_succeeded
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (event_uuid) DO NOTHING`,
			event.UUID, event.Name, accountID,
			event.Step, event.CategoryID, event.Result, event.Outcome, event.Reason,
			event.Surface, event.Class, event.Retried, event.RetryOK,
		)
	}

	results := r.pool.SendBatch(ctx, batch)
	defer results.Close()

	inserted := 0
	for range events {
		tag, err := results.Exec()
		if err != nil {
			return inserted, fmt.Errorf("analytics: record event: %w", err)
		}
		inserted += int(tag.RowsAffected())
	}
	return inserted, nil
}

func (r *PostgresEventRepository) Prune(ctx context.Context, before time.Time) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("analytics: begin prune: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO analytics_daily_counts (
			day, name, step, category_id, result_bucket, outcome, reason,
			surface, classification, retried, retry_succeeded, event_count
		)
		SELECT
			(received_at AT TIME ZONE 'UTC')::date,
			name,
			coalesce(step, ''), coalesce(category_id, ''), coalesce(result_bucket, ''),
			coalesce(outcome, ''), coalesce(reason, ''), coalesce(surface, ''),
			coalesce(classification, ''),
			CASE WHEN retried IS NULL THEN '' WHEN retried THEN 'true' ELSE 'false' END,
			CASE WHEN retry_succeeded IS NULL THEN '' WHEN retry_succeeded THEN 'true' ELSE 'false' END,
			count(*)
		FROM analytics_events
		WHERE received_at < $1
		GROUP BY 1,2,3,4,5,6,7,8,9,10,11
		ON CONFLICT (day, name, step, category_id, result_bucket, outcome, reason,
		             surface, classification, retried, retry_succeeded)
		DO UPDATE SET event_count = analytics_daily_counts.event_count + EXCLUDED.event_count`,
		before,
	); err != nil {
		return 0, fmt.Errorf("analytics: aggregate before prune: %w", err)
	}

	tag, err := tx.Exec(ctx, `DELETE FROM analytics_events WHERE received_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("analytics: delete pruned events: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("analytics: commit prune: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
