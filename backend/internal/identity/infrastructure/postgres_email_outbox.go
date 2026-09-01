package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
)

type PostgresEmailOutbox struct {
	pool *pgxpool.Pool
}

func NewPostgresEmailOutbox(pool *pgxpool.Pool) *PostgresEmailOutbox {
	return &PostgresEmailOutbox{pool: pool}
}

func (o *PostgresEmailOutbox) Enqueue(ctx context.Context, job application.EmailJob) error {
	_, err := o.pool.Exec(ctx,
		`INSERT INTO email_outbox (id, kind, email, created_at, next_attempt_at)
		 VALUES ($1, $2, $3, $4, $4)`,
		job.ID, string(job.Kind), string(job.Email), job.EnqueuedAt,
	)
	if err != nil {
		return fmt.Errorf("email_outbox: enqueue: %w", err)
	}
	return nil
}

func (o *PostgresEmailOutbox) Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]application.EmailJob, error) {
	rows, err := o.pool.Query(ctx,
		`WITH due AS (
		     SELECT id FROM email_outbox
		      WHERE status = 'pending'
		        AND next_attempt_at <= $1
		        AND (locked_until IS NULL OR locked_until <= $1)
		      ORDER BY next_attempt_at, created_at
		      LIMIT $3
		      FOR UPDATE SKIP LOCKED
		 )
		 UPDATE email_outbox AS o
		    SET locked_until = $2,
		        attempts     = o.attempts + 1
		   FROM due
		  WHERE o.id = due.id
		 RETURNING o.id, o.kind, o.email, o.attempts`,
		now, now.Add(lease), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("email_outbox: claim: %w", err)
	}
	defer rows.Close()

	var jobs []application.EmailJob
	for rows.Next() {
		var (
			job   application.EmailJob
			kind  string
			email string
		)
		if err := rows.Scan(&job.ID, &kind, &email, &job.Attempts); err != nil {
			return nil, fmt.Errorf("email_outbox: scan claimed job: %w", err)
		}
		job.Kind = application.EmailJobKind(kind)
		job.Email = domain.EmailAddress(email)
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("email_outbox: iterate claimed jobs: %w", err)
	}
	return jobs, nil
}

func (o *PostgresEmailOutbox) Complete(ctx context.Context, id string) error {
	if _, err := o.pool.Exec(ctx, `DELETE FROM email_outbox WHERE id = $1`, id); err != nil {
		return fmt.Errorf("email_outbox: complete: %w", err)
	}
	return nil
}

func (o *PostgresEmailOutbox) Reschedule(ctx context.Context, id string, nextAttemptAt time.Time, reason string) error {
	_, err := o.pool.Exec(ctx,
		`UPDATE email_outbox
		    SET next_attempt_at = $2, locked_until = NULL, last_error = $3
		  WHERE id = $1 AND status = 'pending'`,
		id, nextAttemptAt, reason,
	)
	if err != nil {
		return fmt.Errorf("email_outbox: reschedule: %w", err)
	}
	return nil
}

func (o *PostgresEmailOutbox) MarkDead(ctx context.Context, id string, reason string) error {
	_, err := o.pool.Exec(ctx,
		`UPDATE email_outbox
		    SET status = 'dead', email = '', locked_until = NULL,
		        last_error = $2, completed_at = now()
		  WHERE id = $1`,
		id, reason,
	)
	if err != nil {
		return fmt.Errorf("email_outbox: mark dead: %w", err)
	}
	return nil
}
