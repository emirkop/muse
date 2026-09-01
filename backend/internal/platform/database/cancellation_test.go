package database

import (
	"context"
	"errors"
	"testing"
)

func TestClientDisconnectMidTransaction_CommitsNothing(t *testing.T) {
	pool := uowPool(t)

	ctx, cancel := context.WithCancel(context.Background())
	err := pool.Run(ctx, func(txCtx context.Context) error {
		if err := insertProbe(txCtx, pool, "first"); err != nil {
			return err
		}
		cancel()
		return insertProbe(txCtx, pool, "second")
	})
	if err == nil {
		t.Fatal("expected Run to fail once the request context was cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Logf("Run failed with %v (not context.Canceled — acceptable)", err)
	}
	if got := countProbe(t, pool); got != 0 {
		t.Errorf("a disconnected caller committed %d rows; expected 0 — a partial mutation is exactly what the client's no-retry rule must be able to assume cannot happen", got)
	}
}

func TestCancellationBeforeCommit_CommitsNothing(t *testing.T) {
	pool := uowPool(t)

	ctx, cancel := context.WithCancel(context.Background())
	err := pool.Run(ctx, func(txCtx context.Context) error {
		if err := insertProbe(txCtx, pool, "written"); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if err == nil {
		t.Fatal("expected the commit to fail on a cancelled context")
	}
	if got := countProbe(t, pool); got != 0 {
		t.Errorf("commit on a cancelled context persisted %d rows; expected 0", got)
	}
}

func TestPoolStillUsableAfterCancelledTransaction(t *testing.T) {
	pool := uowPool(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pool.Run(ctx, func(txCtx context.Context) error {
		return insertProbe(txCtx, pool, "doomed")
	}); err == nil {
		t.Fatal("expected Run on an already-cancelled context to fail")
	}

	if err := pool.Run(context.Background(), func(txCtx context.Context) error {
		return insertProbe(txCtx, pool, "retry")
	}); err != nil {
		t.Fatalf("the retry after a cancelled transaction failed: %v", err)
	}
	if got := countProbe(t, pool); got != 1 {
		t.Errorf("expected exactly the retry's 1 row, got %d", got)
	}
}
