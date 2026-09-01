package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
)

type requestPathWork struct {
	enqueues        int
	credentialLooks int
	pendingLooks    int
	derivations     int
	emails          int
	resetRows       int
}

func (h *passwordHarness) snapshotWork(spy *countingHasher) requestPathWork {
	return requestPathWork{
		enqueues:        h.outbox.enqueues,
		credentialLooks: h.credentials.findByEmailCalls,
		pendingLooks:    h.pending.findByEmailCalls,
		derivations:     spy.hashes + spy.derivations(),
		emails:          len(h.email.resets) + len(h.email.verifications) + len(h.email.existingNotices),
		resetRows:       h.resets.count(),
	}
}

func (a requestPathWork) minus(b requestPathWork) requestPathWork {
	return requestPathWork{
		enqueues:        a.enqueues - b.enqueues,
		credentialLooks: a.credentialLooks - b.credentialLooks,
		pendingLooks:    a.pendingLooks - b.pendingLooks,
		derivations:     a.derivations - b.derivations,
		emails:          a.emails - b.emails,
		resetRows:       a.resetRows - b.resetRows,
	}
}

func TestRequestPasswordReset_KnownAndUnknownDoIdenticalRequestPathWork(t *testing.T) {
	h, spy := newCountingHarness(t)
	h.signUpAndVerify(t, "known@example.com", "the-real-passphrase")

	before := h.snapshotWork(spy)
	if err := h.service.RequestPasswordReset(context.Background(), "known@example.com", "source"); err != nil {
		t.Fatalf("known: %v", err)
	}
	known := h.snapshotWork(spy).minus(before)

	before = h.snapshotWork(spy)
	if err := h.service.RequestPasswordReset(context.Background(), "stranger@example.com", "source"); err != nil {
		t.Fatalf("unknown: %v", err)
	}
	unknown := h.snapshotWork(spy).minus(before)

	want := requestPathWork{enqueues: 1}
	if known != want {
		t.Fatalf("known address request-path work = %+v, want exactly one enqueue and nothing else", known)
	}
	if unknown != want {
		t.Fatalf("unknown address request-path work = %+v, want exactly one enqueue and nothing else", unknown)
	}
	if known != unknown {
		t.Fatalf("timing oracle: known %+v vs unknown %+v", known, unknown)
	}
}

func TestResendVerification_KnownAndUnknownDoIdenticalRequestPathWork(t *testing.T) {
	h, spy := newCountingHarness(t)
	if err := h.service.SignUp(context.Background(), "pending@example.com", "a-good-passphrase", "source"); err != nil {
		t.Fatalf("sign up: %v", err)
	}
	pendingBefore, _ := h.pending.FindByEmail(context.Background(), "pending@example.com")

	before := h.snapshotWork(spy)
	if err := h.service.ResendVerification(context.Background(), "pending@example.com", "source"); err != nil {
		t.Fatalf("known: %v", err)
	}
	known := h.snapshotWork(spy).minus(before)

	before = h.snapshotWork(spy)
	if err := h.service.ResendVerification(context.Background(), "stranger@example.com", "source"); err != nil {
		t.Fatalf("unknown: %v", err)
	}
	unknown := h.snapshotWork(spy).minus(before)

	want := requestPathWork{enqueues: 1}
	if known != want || unknown != want {
		t.Fatalf("request-path work must be exactly one enqueue for both: known %+v, unknown %+v", known, unknown)
	}
	pendingAfter, _ := h.pending.FindByEmail(context.Background(), "pending@example.com")
	if pendingAfter.TokenDigest != pendingBefore.TokenDigest {
		t.Fatal("the request path must not mint or replace a token — that happens at dequeue")
	}
}

func TestRequestPasswordReset_RateLimitPrecedesEnqueue(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.limiter.lock(application.AttemptScopePasswordReset, domain.DigestOpaqueToken("anyone@example.com"))

	err := h.service.RequestPasswordReset(context.Background(), "anyone@example.com", "source")

	if !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("expected ErrTooManyAttempts, got %v", err)
	}
	if h.outbox.enqueues != 0 {
		t.Fatal("a throttled request must not reach the outbox")
	}
}

func TestResendVerification_RateLimitPrecedesEnqueue(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.limiter.lock(application.AttemptScopeVerificationResend, domain.DigestOpaqueToken("anyone@example.com"))

	err := h.service.ResendVerification(context.Background(), "anyone@example.com", "source")

	if !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("expected ErrTooManyAttempts, got %v", err)
	}
	if h.outbox.enqueues != 0 {
		t.Fatal("a throttled request must not reach the outbox")
	}
}

func TestRequestPasswordReset_EnqueueFailureIsSurfaced(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.outbox.failEnqueue = true

	if err := h.service.RequestPasswordReset(context.Background(), "anyone@example.com", "source"); err == nil {
		t.Fatal("an outbox failure must be reported to the caller")
	}
}

func TestPasswordService_WithoutAnOutboxRefusesRatherThanSendingInline(t *testing.T) {
	h, _ := newCountingHarness(t)
	service := application.NewPasswordService(
		h.credentials, h.pending, h.resets, h.accounts, h.sessions, h.login,
		testHasher(), infrastructure.OpaqueRefreshTokenGenerator{}, h.email, h.limiter,
		nil,
	)

	if err := service.RequestPasswordReset(context.Background(), "anyone@example.com", "source"); err == nil {
		t.Fatal("with no outbox the service must refuse, never fall back to sending on the request path")
	}
	if _, err := service.DrainEmailOutbox(context.Background()); err == nil {
		t.Fatal("draining a missing outbox must be an error")
	}
	if len(h.email.resets) != 0 {
		t.Fatal("nothing may have been sent inline")
	}
}

func TestDrain_UnknownAddressCompletesAsANoOp(t *testing.T) {
	h, _ := newCountingHarness(t)
	if err := h.service.RequestPasswordReset(context.Background(), "stranger@example.com", "source"); err != nil {
		t.Fatalf("request: %v", err)
	}

	report := h.drain(t)

	if report.Claimed != 1 || report.NoOps != 1 || report.Delivered != 0 {
		t.Fatalf("expected one claimed no-op, got %+v", report)
	}
	if len(h.email.resets) != 0 {
		t.Fatal("no email may be sent for an unknown address")
	}
	if h.resets.count() != 0 {
		t.Fatal("no reset token may be minted for an unknown address")
	}
	if h.outbox.rowCount() != 0 {
		t.Fatal("a completed job must be removed — a stranger's address is not retained")
	}
}

func TestDrain_KnownAddressDeliversOneResetWhoseDigestIsStored(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.signUpAndVerify(t, "known@example.com", "the-real-passphrase")
	if err := h.service.RequestPasswordReset(context.Background(), "known@example.com", "source"); err != nil {
		t.Fatalf("request: %v", err)
	}
	if len(h.email.resets) != 0 {
		t.Fatal("nothing may be sent before the drain — that is the whole point")
	}

	report := h.drain(t)

	if report.Delivered != 1 {
		t.Fatalf("expected one delivery, got %+v", report)
	}
	if len(h.email.resets) != 1 {
		t.Fatalf("expected exactly one reset email, got %d", len(h.email.resets))
	}
	raw := h.email.resets[0].Token
	if _, err := h.resets.FindByTokenDigest(context.Background(), domain.DigestOpaqueToken(raw)); err != nil {
		t.Fatal("the emailed token's digest must be what was stored")
	}
	if h.outbox.rowCount() != 0 {
		t.Fatal("a delivered job must be removed from the outbox")
	}
	if err := h.service.ResetPassword(context.Background(), raw, "a-brand-new-passphrase"); err != nil {
		t.Fatalf("the delivered token must redeem: %v", err)
	}
}

func TestDrain_TokenIsMintedAtDequeueNotAtEnqueue(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.signUpAndVerify(t, "known@example.com", "the-real-passphrase")

	if err := h.service.RequestPasswordReset(context.Background(), "known@example.com", "source"); err != nil {
		t.Fatalf("request: %v", err)
	}

	if h.resets.count() != 0 {
		t.Fatal("no reset token may exist before the job is drained")
	}
	row := h.outbox.only()
	if row == nil {
		t.Fatal("expected one outbox row")
	}
	if row.job.Kind != application.EmailJobPasswordReset || row.job.Email != "known@example.com" {
		t.Fatalf("unexpected job contents: %+v", row.job)
	}

	h.drain(t)

	if h.resets.count() != 1 {
		t.Fatal("exactly one reset token must exist after the drain")
	}
}

func TestDrain_ProviderFailureReschedulesWithBackoffThenDelivers(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.signUpAndVerify(t, "known@example.com", "the-real-passphrase")
	policy := application.EmailOutboxPolicy{BatchSize: 10, Lease: time.Minute, MaxAttempts: 5, BaseBackoff: 10 * time.Second, MaxBackoff: time.Minute}
	h.service.SetEmailOutboxPolicy(policy)
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	h.service.SetClock(func() time.Time { return base })
	if err := h.service.RequestPasswordReset(context.Background(), "known@example.com", "source"); err != nil {
		t.Fatalf("request: %v", err)
	}
	h.email.failNextDelivery = true

	first := h.drain(t)

	if first.Retried != 1 || first.Delivered != 0 {
		t.Fatalf("a failed send must be retried, got %+v", first)
	}
	if len(h.email.resets) != 0 {
		t.Fatal("nothing was delivered")
	}
	row := h.outbox.only()
	if row == nil || row.status != "pending" {
		t.Fatal("the job must remain pending")
	}
	if row.job.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", row.job.Attempts)
	}
	if !row.nextAttemptAt.Equal(base.Add(policy.BaseBackoff)) {
		t.Fatalf("next attempt at %v, want base + backoff %v", row.nextAttemptAt, base.Add(policy.BaseBackoff))
	}
	if row.lastError != "send_failed" {
		t.Fatalf("last_error = %q, want the fixed classification", row.lastError)
	}

	if again := h.drain(t); again.Claimed != 0 {
		t.Fatalf("a rescheduled job must not be claimed before its backoff, got %+v", again)
	}

	h.service.SetClock(func() time.Time { return base.Add(policy.BaseBackoff + time.Second) })
	third := h.drain(t)
	if third.Delivered != 1 {
		t.Fatalf("expected delivery on retry, got %+v", third)
	}
	if len(h.email.resets) != 1 {
		t.Fatalf("expected exactly one email, got %d", len(h.email.resets))
	}
	if h.resets.count() != 2 {
		t.Fatalf("expected two digest rows (one orphan, one delivered), got %d", h.resets.count())
	}
	if _, err := h.resets.FindByTokenDigest(context.Background(), domain.DigestOpaqueToken(h.email.resets[0].Token)); err != nil {
		t.Fatal("the delivered token must be one whose digest is stored")
	}
	if h.outbox.rowCount() != 0 {
		t.Fatal("the delivered job must be removed")
	}
}

func TestEmailOutboxPolicy_BackoffDoublesAndCaps(t *testing.T) {
	policy := application.EmailOutboxPolicy{BaseBackoff: 5 * time.Second, MaxBackoff: time.Minute}

	got := []time.Duration{}
	for attempts := 1; attempts <= 6; attempts++ {
		got = append(got, policy.Backoff(attempts))
	}

	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, time.Minute, time.Minute}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("backoff after %d attempt(s) = %v, want %v (all: %v)", i+1, got[i], want[i], got)
		}
	}
}

func TestDrain_DeadLettersAfterMaxAttemptsAndScrubsTheAddress(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.signUpAndVerify(t, "known@example.com", "the-real-passphrase")
	policy := application.EmailOutboxPolicy{BatchSize: 10, Lease: time.Minute, MaxAttempts: 3, BaseBackoff: time.Second, MaxBackoff: time.Second}
	h.service.SetEmailOutboxPolicy(policy)
	clock := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	h.service.SetClock(func() time.Time { return clock })
	if err := h.service.RequestPasswordReset(context.Background(), "known@example.com", "source"); err != nil {
		t.Fatalf("request: %v", err)
	}
	h.email.failAll = true

	var reports []application.DrainReport
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		reports = append(reports, h.drain(t))
		clock = clock.Add(policy.MaxBackoff + time.Second)
	}

	for i := 0; i < policy.MaxAttempts-1; i++ {
		if reports[i].Retried != 1 {
			t.Fatalf("attempt %d should retry, got %+v", i+1, reports[i])
		}
	}
	last := reports[policy.MaxAttempts-1]
	if last.Dead != 1 || last.Retried != 0 {
		t.Fatalf("the final attempt must dead-letter, got %+v", last)
	}
	if h.outbox.deadCount() != 1 || h.outbox.pendingCount() != 0 {
		t.Fatalf("expected one dead row and no pending: dead=%d pending=%d", h.outbox.deadCount(), h.outbox.pendingCount())
	}
	row := h.outbox.only()
	if row.job.Email != "" {
		t.Fatalf("a dead-lettered row must have its address scrubbed, got %q", row.job.Email)
	}
	if row.lastError != "send_failed" {
		t.Fatalf("last_error = %q", row.lastError)
	}
	h.email.failAll = false
	if after := h.drain(t); after.Claimed != 0 {
		t.Fatalf("a dead job must never be claimed again, got %+v", after)
	}
}

func TestDrain_ACrashedDrainersJobIsRecoveredAfterItsLeaseLapses(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.signUpAndVerify(t, "known@example.com", "the-real-passphrase")
	policy := application.DefaultEmailOutboxPolicy
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	h.service.SetClock(func() time.Time { return base })
	if err := h.service.RequestPasswordReset(context.Background(), "known@example.com", "source"); err != nil {
		t.Fatalf("request: %v", err)
	}

	claimed, err := h.outbox.Claim(context.Background(), base, policy.Lease, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("expected to claim one job, got %d, %v", len(claimed), err)
	}

	if report := h.drain(t); report.Claimed != 0 {
		t.Fatalf("a leased job must not be claimed by another drainer, got %+v", report)
	}

	h.service.SetClock(func() time.Time { return base.Add(policy.Lease + time.Second) })
	report := h.drain(t)
	if report.Claimed != 1 || report.Delivered != 1 {
		t.Fatalf("the job must be recovered after the lease lapses, got %+v", report)
	}
	if len(h.email.resets) != 1 {
		t.Fatalf("expected one email after recovery, got %d", len(h.email.resets))
	}
}

func TestDrain_ResendForAnAlreadyVerifiedSignupIsANoOp(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.signUpAndVerify(t, "done@example.com", "the-real-passphrase")
	pendingBefore, _ := h.pending.FindByEmail(context.Background(), "done@example.com")
	verificationsBefore := len(h.email.verifications)
	if err := h.service.ResendVerification(context.Background(), "done@example.com", "source"); err != nil {
		t.Fatalf("resend: %v", err)
	}

	report := h.drain(t)

	if report.NoOps != 1 || report.Delivered != 0 {
		t.Fatalf("a consumed sign-up has nothing outstanding to resend, got %+v", report)
	}
	if len(h.email.verifications) != verificationsBefore {
		t.Fatal("no verification email may be sent for an already-verified address")
	}
	pendingAfter, _ := h.pending.FindByEmail(context.Background(), "done@example.com")
	if pendingAfter.ConsumedAt == nil || pendingAfter.TokenDigest != pendingBefore.TokenDigest {
		t.Fatal("a consumed sign-up must never be reopened by a resend")
	}
}

func TestDrain_BatchSizeBoundsEachPass(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.service.SetEmailOutboxPolicy(application.EmailOutboxPolicy{BatchSize: 2, Lease: time.Minute, MaxAttempts: 3, BaseBackoff: time.Second, MaxBackoff: time.Second})
	for i := 0; i < 5; i++ {
		if err := h.service.RequestPasswordReset(context.Background(), fmt.Sprintf("s%d@example.com", i), "source"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}

	claimed := []int{h.drain(t).Claimed, h.drain(t).Claimed, h.drain(t).Claimed, h.drain(t).Claimed}

	if fmt.Sprint(claimed) != fmt.Sprint([]int{2, 2, 1, 0}) {
		t.Fatalf("claimed per pass = %v, want [2 2 1 0]", claimed)
	}
}

func TestDrain_OneFailingJobDoesNotStopTheBatch(t *testing.T) {
	h, _ := newCountingHarness(t)
	h.signUpAndVerify(t, "first@example.com", "the-real-passphrase")
	h.signUpAndVerify(t, "second@example.com", "the-real-passphrase")
	for _, email := range []string{"first@example.com", "second@example.com"} {
		if err := h.service.RequestPasswordReset(context.Background(), email, "source"); err != nil {
			t.Fatalf("request %s: %v", email, err)
		}
	}
	h.email.failNextDelivery = true

	report := h.drain(t)

	if report.Claimed != 2 || report.Delivered != 1 || report.Retried != 1 {
		t.Fatalf("one failure must not stop the other delivery, got %+v", report)
	}
}
