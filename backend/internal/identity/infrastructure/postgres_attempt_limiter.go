package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
)

type AttemptPolicy struct {
	MaxFailures int
	Window      time.Duration
	Lockout     time.Duration
}

var DefaultAttemptPolicy = AttemptPolicy{
	MaxFailures: 8,
	Window:      15 * time.Minute,
	Lockout:     15 * time.Minute,
}

type PostgresAttemptLimiter struct {
	pool   *pgxpool.Pool
	policy AttemptPolicy
	now    func() time.Time
}

func NewPostgresAttemptLimiter(pool *pgxpool.Pool, policy AttemptPolicy) *PostgresAttemptLimiter {
	return &PostgresAttemptLimiter{pool: pool, policy: policy, now: time.Now}
}

func (l *PostgresAttemptLimiter) SetClock(now func() time.Time) { l.now = now }

func (l *PostgresAttemptLimiter) Check(ctx context.Context, scope application.AttemptScope, key string) error {
	var lockedUntil *time.Time
	err := l.pool.QueryRow(ctx,
		`SELECT locked_until FROM auth_attempts WHERE scope = $1 AND key_digest = $2`,
		string(scope), key,
	).Scan(&lockedUntil)
	if err != nil {
		return nil
	}
	if lockedUntil != nil && l.now().Before(*lockedUntil) {
		return domain.ErrTooManyAttempts
	}
	return nil
}

func (l *PostgresAttemptLimiter) RecordFailure(ctx context.Context, scope application.AttemptScope, key string) error {
	now := l.now()
	_, err := l.pool.Exec(ctx,
		`INSERT INTO auth_attempts (scope, key_digest, window_started_at, failure_count, locked_until)
		 VALUES ($1, $2, $3, 1, CASE WHEN $5::int <= 1 THEN $6::timestamptz ELSE NULL END)
		 ON CONFLICT (scope, key_digest) DO UPDATE SET
		   window_started_at = CASE
		     WHEN auth_attempts.window_started_at < $4 THEN $3
		     ELSE auth_attempts.window_started_at END,
		   failure_count = CASE
		     WHEN auth_attempts.window_started_at < $4 THEN 1
		     ELSE auth_attempts.failure_count + 1 END,
		   locked_until = CASE
		     WHEN auth_attempts.window_started_at >= $4
		      AND auth_attempts.failure_count + 1 >= $5 THEN $6
		     ELSE auth_attempts.locked_until END`,
		string(scope), key, now,
		now.Add(-l.policy.Window),
		l.policy.MaxFailures,
		now.Add(l.policy.Lockout),
	)
	if err != nil {
		return fmt.Errorf("attempt_limiter: record failure: %w", err)
	}
	return nil
}

func (l *PostgresAttemptLimiter) Reset(ctx context.Context, scope application.AttemptScope, key string) error {
	_, err := l.pool.Exec(ctx,
		`DELETE FROM auth_attempts WHERE scope = $1 AND key_digest = $2`,
		string(scope), key,
	)
	if err != nil {
		return fmt.Errorf("attempt_limiter: reset: %w", err)
	}
	return nil
}
