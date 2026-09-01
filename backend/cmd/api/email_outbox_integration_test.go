package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	identityapp "muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	identityinfra "muse-backend/internal/identity/infrastructure"
)

func (s *emailAuthStack) outboxCount(t *testing.T, where string) int {
	t.Helper()
	var count int
	if err := s.pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM email_outbox `+where).Scan(&count); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return count
}

func TestEmailOutbox_ResetRequestSendsNothingOnTheRequestPathForKnownOrUnknown(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "known@example.com", "the-real-passphrase")

	knownStatus, knownBody := s.post(t, "/auth/email/password-reset", map[string]string{"email": "known@example.com"})
	unknownStatus, unknownBody := s.post(t, "/auth/email/password-reset", map[string]string{"email": "stranger@example.com"})

	knownJSON, _ := json.Marshal(knownBody)
	unknownJSON, _ := json.Marshal(unknownBody)
	if knownStatus != unknownStatus || string(knownJSON) != string(unknownJSON) {
		t.Fatalf("responses differ: %d %s vs %d %s", knownStatus, knownJSON, unknownStatus, unknownJSON)
	}
	if _, resets, _ := s.email.counts(); resets != 0 {
		t.Fatalf("the request path must send nothing, sent %d", resets)
	}
	if got := s.outboxCount(t, `WHERE status = 'pending'`); got != 2 {
		t.Fatalf("expected two pending outbox rows, got %d", got)
	}
	var tokens int
	_ = s.pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM password_resets`).Scan(&tokens)
	if tokens != 0 {
		t.Fatalf("no reset token may exist before the drain, found %d", tokens)
	}

	report := s.drain(t)

	if report.Claimed != 2 || report.Delivered != 1 || report.NoOps != 1 {
		t.Fatalf("expected one delivery and one no-op, got %+v", report)
	}
	if _, resets, _ := s.email.counts(); resets != 1 {
		t.Fatalf("exactly one email must go out after the drain, got %d", resets)
	}
	if got := s.outboxCount(t, ""); got != 0 {
		t.Fatalf("both jobs must be removed after completion, %d remain", got)
	}
}

func TestEmailOutbox_ResendSendsNothingOnTheRequestPathForKnownOrUnknown(t *testing.T) {
	s := newEmailAuthStack(t)
	if status, _ := s.post(t, "/auth/email/signup", map[string]string{
		"email": "pending@example.com", "password": "a-good-passphrase",
	}); status != http.StatusAccepted {
		t.Fatal("signup failed")
	}
	verificationsAfterSignup, _, _ := s.email.counts()

	knownStatus, knownBody := s.post(t, "/auth/email/verification/resend", map[string]string{"email": "pending@example.com"})
	unknownStatus, unknownBody := s.post(t, "/auth/email/verification/resend", map[string]string{"email": "stranger@example.com"})

	knownJSON, _ := json.Marshal(knownBody)
	unknownJSON, _ := json.Marshal(unknownBody)
	if knownStatus != unknownStatus || string(knownJSON) != string(unknownJSON) {
		t.Fatalf("responses differ: %d %s vs %d %s", knownStatus, knownJSON, unknownStatus, unknownJSON)
	}
	if verifications, _, _ := s.email.counts(); verifications != verificationsAfterSignup {
		t.Fatal("a resend request must send nothing itself")
	}
	if got := s.outboxCount(t, `WHERE status = 'pending'`); got != 2 {
		t.Fatalf("expected two pending rows, got %d", got)
	}

	report := s.drain(t)

	if report.Delivered != 1 || report.NoOps != 1 {
		t.Fatalf("expected one delivery and one no-op, got %+v", report)
	}
	if verifications, _, _ := s.email.counts(); verifications != verificationsAfterSignup+1 {
		t.Fatalf("exactly one more verification email after the drain, got %d", verifications-verificationsAfterSignup)
	}
	newToken := s.email.lastVerification(t)
	if status, _ := s.post(t, "/auth/email/verify", map[string]string{"token": s.email.verifications[0]}); status != http.StatusBadRequest {
		t.Fatalf("the superseded link must stop working, got %d", status)
	}
	if status, _ := s.post(t, "/auth/email/verify", map[string]string{"token": newToken}); status != http.StatusOK {
		t.Fatalf("the resent link must work, got %d", status)
	}
}

func TestEmailOutbox_RestartRecoveryDeliversAClaimedJobAfterItsLeaseLapses(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "known@example.com", "the-real-passphrase")
	base := time.Now()
	s.passwords.SetClock(func() time.Time { return base })
	if status, _ := s.post(t, "/auth/email/password-reset", map[string]string{"email": "known@example.com"}); status != http.StatusAccepted {
		t.Fatal("reset request failed")
	}

	crashed := identityinfra.NewPostgresEmailOutbox(s.pool.Pool())
	taken, err := crashed.Claim(context.Background(), base, identityapp.DefaultEmailOutboxPolicy.Lease, 10)
	if err != nil || len(taken) != 1 {
		t.Fatalf("simulated crash claim: %d, %v", len(taken), err)
	}

	if report := s.drain(t); report.Claimed != 0 {
		t.Fatalf("the surviving process must not touch a leased job, got %+v", report)
	}
	if _, resets, _ := s.email.counts(); resets != 0 {
		t.Fatal("nothing may have been delivered yet")
	}

	s.passwords.SetClock(func() time.Time { return base.Add(identityapp.DefaultEmailOutboxPolicy.Lease + time.Second) })
	report := s.drain(t)

	if report.Claimed != 1 || report.Delivered != 1 {
		t.Fatalf("the job must be recovered and delivered, got %+v", report)
	}
	if _, resets, _ := s.email.counts(); resets != 1 {
		t.Fatalf("expected exactly one email, got %d", resets)
	}
	if s.outboxCount(t, "") != 0 {
		t.Fatal("the delivered job must be removed")
	}
}

func TestEmailOutbox_ProviderFailureRetriesWithBackoffOverTheRealStack(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "known@example.com", "the-real-passphrase")
	policy := identityapp.EmailOutboxPolicy{BatchSize: 10, Lease: time.Minute, MaxAttempts: 5, BaseBackoff: 10 * time.Second, MaxBackoff: time.Minute}
	s.passwords.SetEmailOutboxPolicy(policy)
	base := time.Now()
	s.passwords.SetClock(func() time.Time { return base })
	if status, _ := s.post(t, "/auth/email/password-reset", map[string]string{"email": "known@example.com"}); status != http.StatusAccepted {
		t.Fatal("reset request failed")
	}
	s.email.failNextReset = true

	first := s.drain(t)
	if first.Retried != 1 {
		t.Fatalf("a provider failure must reschedule, got %+v", first)
	}
	var (
		attempts int
		reason   string
	)
	if err := s.pool.Pool().QueryRow(context.Background(), `SELECT attempts, last_error FROM email_outbox`).Scan(&attempts, &reason); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if attempts != 1 || reason != "send_failed" {
		t.Fatalf("attempts=%d reason=%q", attempts, reason)
	}

	if again := s.drain(t); again.Claimed != 0 {
		t.Fatalf("not due yet, got %+v", again)
	}

	s.passwords.SetClock(func() time.Time { return base.Add(policy.BaseBackoff + time.Second) })
	third := s.drain(t)
	if third.Delivered != 1 {
		t.Fatalf("the retry must deliver, got %+v", third)
	}
	if _, resets, _ := s.email.counts(); resets != 1 {
		t.Fatalf("expected exactly one email, got %d", resets)
	}
	if status, _ := s.post(t, "/auth/email/password-reset/confirm", map[string]string{
		"token": s.email.lastReset(t), "password": "a-brand-new-passphrase",
	}); status != http.StatusOK {
		t.Fatalf("the delivered token must redeem, got %d", status)
	}
}

func TestEmailOutbox_NoPlaintextTokenIsEverPersisted(t *testing.T) {
	s := newEmailAuthStack(t)
	s.signUpAndVerify(t, "known@example.com", "the-real-passphrase")
	ctx := context.Background()

	if status, _ := s.post(t, "/auth/email/password-reset", map[string]string{"email": "known@example.com"}); status != http.StatusAccepted {
		t.Fatal("reset request failed")
	}
	var rowText string
	if err := s.pool.Pool().QueryRow(ctx, `SELECT row_to_json(o)::text FROM email_outbox o LIMIT 1`).Scan(&rowText); err != nil {
		t.Fatalf("read outbox row: %v", err)
	}

	s.drain(t)
	raw := s.email.lastReset(t)

	if contains(rowText, raw) {
		t.Fatal("the outbox row contained the raw token")
	}
	var stored string
	if err := s.pool.Pool().QueryRow(ctx, `SELECT token_digest FROM password_resets`).Scan(&stored); err != nil {
		t.Fatalf("read digest: %v", err)
	}
	if stored == raw {
		t.Fatal("the raw token must never be stored")
	}
	if stored != domain.DigestOpaqueToken(raw) {
		t.Fatal("the stored value must be the token's digest")
	}
	if s.outboxCount(t, "") != 0 {
		t.Fatal("no outbox row may remain after delivery")
	}
}

func TestEmailOutbox_RateLimitStillPrecedesTheEnqueueOverHTTP(t *testing.T) {
	s := newEmailAuthStack(t)
	for i := 0; i < identityinfra.DefaultAttemptPolicy.MaxFailures; i++ {
		s.post(t, "/auth/email/password-reset", map[string]string{"email": "flood@example.com"})
	}
	before := s.outboxCount(t, "")

	status, _ := s.post(t, "/auth/email/password-reset", map[string]string{"email": "flood@example.com"})

	if status != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", status)
	}
	if s.outboxCount(t, "") != before {
		t.Fatal("a throttled request must not enqueue a job")
	}
}

func contains(haystack, needle string) bool {
	return needle != "" && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
