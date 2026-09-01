package database

import (
	"context"
	"errors"
	"os"
	"testing"
)

func uowPool(t *testing.T) *Pool {
	t.Helper()
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping PostgreSQL unit-of-work tests")
	}
	pool, err := Connect(context.Background(), connStr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	if _, err := pool.Pool().Exec(ctx, `CREATE TABLE IF NOT EXISTS uow_probe (id SERIAL PRIMARY KEY, label TEXT NOT NULL)`); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx, `TRUNCATE uow_probe`); err != nil {
		t.Fatalf("truncate probe table: %v", err)
	}
	return pool
}

func countProbe(t *testing.T, pool *Pool) int {
	t.Helper()
	var n int
	if err := pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM uow_probe`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func insertProbe(ctx context.Context, pool *Pool, label string) error {
	_, err := ExecutorFrom(ctx, pool.Pool()).Exec(ctx, `INSERT INTO uow_probe (label) VALUES ($1)`, label)
	return err
}

func TestRun_CommitsOnNil(t *testing.T) {
	pool := uowPool(t)

	err := pool.Run(context.Background(), func(ctx context.Context) error {
		if err := insertProbe(ctx, pool, "a"); err != nil {
			return err
		}
		return insertProbe(ctx, pool, "b")
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := countProbe(t, pool); got != 2 {
		t.Errorf("expected 2 rows committed, got %d", got)
	}
}

func TestRun_RollsBackEverythingOnError(t *testing.T) {
	pool := uowPool(t)
	sentinel := errors.New("business rule failed")

	err := pool.Run(context.Background(), func(ctx context.Context) error {
		if err := insertProbe(ctx, pool, "a"); err != nil {
			return err
		}
		if err := insertProbe(ctx, pool, "b"); err != nil {
			return err
		}
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("Run must return the caller's error, got %v", err)
	}
	if got := countProbe(t, pool); got != 0 {
		t.Errorf("expected rollback to leave 0 rows, got %d", got)
	}
}

func TestRun_RollsBackOnPanic_AndRepanics(t *testing.T) {
	pool := uowPool(t)

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Error("Run must re-panic after rolling back")
			}
		}()
		_ = pool.Run(context.Background(), func(ctx context.Context) error {
			_ = insertProbe(ctx, pool, "a")
			panic("boom")
		})
	}()

	if got := countProbe(t, pool); got != 0 {
		t.Errorf("a panic must roll back, got %d rows", got)
	}
}

func TestRun_NestedCallJoinsTheOuterTransaction(t *testing.T) {
	pool := uowPool(t)
	sentinel := errors.New("outer fails")

	err := pool.Run(context.Background(), func(ctx context.Context) error {
		if err := pool.Run(ctx, func(ctx context.Context) error {
			return insertProbe(ctx, pool, "inner")
		}); err != nil {
			return err
		}
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}
	if got := countProbe(t, pool); got != 0 {
		t.Errorf("inner write must roll back with the outer transaction, got %d rows", got)
	}
}

func TestExecutorFrom_OutsideATransaction_UsesThePool(t *testing.T) {
	pool := uowPool(t)

	if err := insertProbe(context.Background(), pool, "direct"); err != nil {
		t.Fatalf("direct insert: %v", err)
	}
	if got := countProbe(t, pool); got != 1 {
		t.Errorf("expected 1 row, got %d", got)
	}
}
