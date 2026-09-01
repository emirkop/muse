package infrastructure_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/infrastructure"
)

func newOutbox(t *testing.T) (*infrastructure.PostgresEmailOutbox, func(context.Context, string, ...any) pgxRow) {
	t.Helper()
	pool := testPool(t)
	query := func(ctx context.Context, sql string, args ...any) pgxRow {
		return pool.Pool().QueryRow(ctx, sql, args...)
	}
	return infrastructure.NewPostgresEmailOutbox(pool.Pool()), query
}

type pgxRow interface{ Scan(dest ...any) error }

func enqueueN(t *testing.T, outbox *infrastructure.PostgresEmailOutbox, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("job-%02d", i)
		if err := outbox.Enqueue(context.Background(), application.EmailJob{
			ID: id, Kind: application.EmailJobPasswordReset, Email: "someone@example.com", EnqueuedAt: time.Now(),
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestPostgresEmailOutbox_EnqueueIsDurableAndPending(t *testing.T) {
	outbox, query := newOutbox(t)
	ctx := context.Background()

	if err := outbox.Enqueue(ctx, application.EmailJob{
		ID: "job-1", Kind: application.EmailJobVerificationResend, Email: "someone@example.com", EnqueuedAt: time.Now(),
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var (
		kind, email, status string
		attempts            int
		lockedUntil         *time.Time
	)
	if err := query(ctx, `SELECT kind, email, status, attempts, locked_until FROM email_outbox WHERE id = 'job-1'`).
		Scan(&kind, &email, &status, &attempts, &lockedUntil); err != nil {
		t.Fatalf("the job must be a row: %v", err)
	}
	if kind != "verification_resend" || email != "someone@example.com" || status != "pending" || attempts != 0 || lockedUntil != nil {
		t.Fatalf("unexpected row: kind=%s email=%s status=%s attempts=%d locked=%v", kind, email, status, attempts, lockedUntil)
	}
}

func TestPostgresEmailOutbox_ClaimLeasesAndCountsTheAttempt(t *testing.T) {
	outbox, query := newOutbox(t)
	ctx := context.Background()
	enqueueN(t, outbox, 1)
	now := time.Now()

	jobs, err := outbox.Claim(ctx, now, time.Minute, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Attempts != 1 || jobs[0].Email != "someone@example.com" {
		t.Fatalf("unexpected claim result: %+v", jobs)
	}

	var lockedUntil time.Time
	if err := query(ctx, `SELECT locked_until FROM email_outbox WHERE id = $1`, jobs[0].ID).Scan(&lockedUntil); err != nil {
		t.Fatalf("read lease: %v", err)
	}
	if lockedUntil.Before(now.Add(time.Minute - time.Second)) {
		t.Fatalf("lease %v is not ~now+1m", lockedUntil)
	}

	again, err := outbox.Claim(ctx, now, time.Minute, 10)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("a leased job must not be re-claimed, got %d", len(again))
	}
}

func TestPostgresEmailOutbox_ACrashedClaimIsRecoveredByAnotherInstanceAfterTheLease(t *testing.T) {
	pool := testPool(t)
	crashed := infrastructure.NewPostgresEmailOutbox(pool.Pool())
	restarted := infrastructure.NewPostgresEmailOutbox(pool.Pool())
	ctx := context.Background()
	if err := crashed.Enqueue(ctx, application.EmailJob{ID: "job-1", Kind: application.EmailJobPasswordReset, Email: "someone@example.com", EnqueuedAt: time.Now()}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	now := time.Now()

	first, err := crashed.Claim(ctx, now, time.Minute, 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %d jobs, %v", len(first), err)
	}

	during, err := restarted.Claim(ctx, now.Add(30*time.Second), time.Minute, 10)
	if err != nil {
		t.Fatalf("claim during lease: %v", err)
	}
	if len(during) != 0 {
		t.Fatal("the job must be invisible while its lease holds")
	}

	after, err := restarted.Claim(ctx, now.Add(time.Minute+time.Second), time.Minute, 10)
	if err != nil {
		t.Fatalf("claim after lease: %v", err)
	}
	if len(after) != 1 || after[0].ID != "job-1" {
		t.Fatalf("the job must be recoverable after the lease lapses, got %+v", after)
	}
	if after[0].Attempts != 2 {
		t.Fatalf("the recovery is the job's second attempt, got %d", after[0].Attempts)
	}
}

func TestPostgresEmailOutbox_ConcurrentClaimsNeverShareAJob(t *testing.T) {
	outbox, _ := newOutbox(t)
	ctx := context.Background()
	const jobs, drainers, perClaim = 20, 8, 5
	enqueueN(t, outbox, jobs)
	now := time.Now()

	var (
		mu      sync.Mutex
		claimed []string
		wg      sync.WaitGroup
	)
	wg.Add(drainers)
	for d := 0; d < drainers; d++ {
		go func() {
			defer wg.Done()
			got, err := outbox.Claim(ctx, now, time.Minute, perClaim)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			mu.Lock()
			for _, job := range got {
				claimed = append(claimed, job.ID)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	seen := map[string]int{}
	for _, id := range claimed {
		seen[id]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("job %s was claimed %d times — two drainers would have processed it concurrently", id, count)
		}
	}
	if len(seen) != jobs {
		t.Fatalf("expected every one of %d jobs to be claimed exactly once, got %d distinct", jobs, len(seen))
	}
}

func TestPostgresEmailOutbox_ClaimRespectsTheLimit(t *testing.T) {
	outbox, _ := newOutbox(t)
	enqueueN(t, outbox, 7)

	got, err := outbox.Claim(context.Background(), time.Now(), time.Minute, 3)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("claimed %d, want the limit of 3", len(got))
	}
}

func TestPostgresEmailOutbox_CompleteRemovesTheRow(t *testing.T) {
	outbox, query := newOutbox(t)
	ctx := context.Background()
	enqueueN(t, outbox, 1)

	if err := outbox.Complete(ctx, "job-00"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	var count int
	if err := query(ctx, `SELECT count(*) FROM email_outbox`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatal("a completed job must be deleted — the recipient's address is not retained")
	}
}

func TestPostgresEmailOutbox_RescheduleReleasesTheLeaseAndDefersTheJob(t *testing.T) {
	outbox, query := newOutbox(t)
	ctx := context.Background()
	enqueueN(t, outbox, 1)
	now := time.Now()
	if _, err := outbox.Claim(ctx, now, time.Minute, 10); err != nil {
		t.Fatalf("claim: %v", err)
	}
	next := now.Add(10 * time.Second)

	if err := outbox.Reschedule(ctx, "job-00", next, "send_failed"); err != nil {
		t.Fatalf("reschedule: %v", err)
	}

	var (
		lockedUntil *time.Time
		reason      string
		attempts    int
	)
	if err := query(ctx, `SELECT locked_until, last_error, attempts FROM email_outbox WHERE id = 'job-00'`).Scan(&lockedUntil, &reason, &attempts); err != nil {
		t.Fatalf("read: %v", err)
	}
	if lockedUntil != nil {
		t.Fatal("reschedule must release the lease")
	}
	if reason != "send_failed" || attempts != 1 {
		t.Fatalf("reason=%q attempts=%d", reason, attempts)
	}
	if got, _ := outbox.Claim(ctx, next.Add(-time.Second), time.Minute, 10); len(got) != 0 {
		t.Fatal("a rescheduled job must not be claimable before its due time")
	}
	got, err := outbox.Claim(ctx, next, time.Minute, 10)
	if err != nil || len(got) != 1 || got[0].Attempts != 2 {
		t.Fatalf("expected the job on its second attempt at the due time, got %+v, %v", got, err)
	}
}

func TestPostgresEmailOutbox_MarkDeadScrubsTheAddressAndRetires(t *testing.T) {
	outbox, query := newOutbox(t)
	ctx := context.Background()
	enqueueN(t, outbox, 1)

	if err := outbox.MarkDead(ctx, "job-00", "send_failed"); err != nil {
		t.Fatalf("mark dead: %v", err)
	}

	var (
		status, email, reason string
		completedAt           *time.Time
	)
	if err := query(ctx, `SELECT status, email, last_error, completed_at FROM email_outbox WHERE id = 'job-00'`).Scan(&status, &email, &reason, &completedAt); err != nil {
		t.Fatalf("read: %v", err)
	}
	if status != "dead" || email != "" || reason != "send_failed" || completedAt == nil {
		t.Fatalf("status=%s email=%q reason=%s completed=%v", status, email, reason, completedAt)
	}
	if got, _ := outbox.Claim(ctx, time.Now().Add(time.Hour), time.Minute, 10); len(got) != 0 {
		t.Fatal("a dead job must never be claimed")
	}
}

func TestPostgresEmailOutbox_SchemaHasNoTokenColumn(t *testing.T) {
	pool := testPool(t)
	rows, err := pool.Pool().Query(context.Background(),
		`SELECT column_name FROM information_schema.columns WHERE table_name = 'email_outbox' ORDER BY ordinal_position`)
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		columns = append(columns, name)
	}
	for _, name := range columns {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "digest") || strings.Contains(lower, "password") {
			t.Fatalf("email_outbox must carry no credential material; found column %q", name)
		}
	}
	want := "id kind email status attempts next_attempt_at locked_until last_error created_at completed_at"
	if got := strings.Join(columns, " "); got != want {
		t.Fatalf("unexpected email_outbox columns:\n got  %s\n want %s", got, want)
	}
}

func TestPostgresEmailOutbox_DueTimeComesFromTheApplicationClock(t *testing.T) {
	outbox, query := newOutbox(t)
	ctx := context.Background()
	enqueuedAt := time.Now().Add(-time.Hour)
	if err := outbox.Enqueue(ctx, application.EmailJob{
		ID: "job-1", Kind: application.EmailJobPasswordReset, Email: "someone@example.com", EnqueuedAt: enqueuedAt,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var nextAttemptAt time.Time
	if err := query(ctx, `SELECT next_attempt_at FROM email_outbox WHERE id = 'job-1'`).Scan(&nextAttemptAt); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !nextAttemptAt.Equal(enqueuedAt) {
		t.Fatalf("next_attempt_at = %v, want the application's EnqueuedAt %v", nextAttemptAt, enqueuedAt)
	}

	got, err := outbox.Claim(ctx, enqueuedAt, time.Minute, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("a job must be claimable at its own enqueue instant, got %d, %v", len(got), err)
	}
}
