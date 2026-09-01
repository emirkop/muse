package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"muse-backend/internal/identity/domain"
)

type EmailJobKind string

const (
	EmailJobPasswordReset      EmailJobKind = "password_reset"
	EmailJobVerificationResend EmailJobKind = "verification_resend"
)

type EmailJob struct {
	ID         string
	Kind       EmailJobKind
	Email      domain.EmailAddress
	Attempts   int
	EnqueuedAt time.Time
}

type EmailOutbox interface {
	Enqueue(ctx context.Context, job EmailJob) error

	Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]EmailJob, error)

	Complete(ctx context.Context, id string) error

	Reschedule(ctx context.Context, id string, nextAttemptAt time.Time, reason string) error

	MarkDead(ctx context.Context, id string, reason string) error
}

type EmailOutboxPolicy struct {
	BatchSize   int
	Lease       time.Duration
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}

var DefaultEmailOutboxPolicy = EmailOutboxPolicy{
	BatchSize:   25,
	Lease:       time.Minute,
	MaxAttempts: 8,
	BaseBackoff: 5 * time.Second,
	MaxBackoff:  10 * time.Minute,
}

func (p EmailOutboxPolicy) Backoff(attempts int) time.Duration {
	delay := p.BaseBackoff
	for i := 1; i < attempts && delay < p.MaxBackoff; i++ {
		delay *= 2
	}
	if delay > p.MaxBackoff {
		delay = p.MaxBackoff
	}
	return delay
}

type DrainReport struct {
	Claimed   int
	Delivered int
	NoOps     int
	Retried   int
	Dead      int
}

const (
	emailFailureLookup = "lookup_failed"
	emailFailureStore  = "store_failed"
	emailFailureSend   = "send_failed"
	emailFailureKind   = "unknown_kind"
)

var errEmailOutboxNotConfigured = errors.New("email outbox: not configured")

func (s *PasswordService) SetEmailOutboxPolicy(policy EmailOutboxPolicy) { s.outboxPolicy = policy }

func (s *PasswordService) enqueueEmail(ctx context.Context, kind EmailJobKind, email domain.EmailAddress) error {
	if s.outbox == nil {
		return errEmailOutboxNotConfigured
	}
	if err := s.outbox.Enqueue(ctx, EmailJob{ID: newRandomID(), Kind: kind, Email: email, EnqueuedAt: s.now()}); err != nil {
		return fmt.Errorf("email outbox: enqueue: %w", err)
	}
	return nil
}

func (s *PasswordService) DrainEmailOutbox(ctx context.Context) (DrainReport, error) {
	var report DrainReport
	if s.outbox == nil {
		return report, errEmailOutboxNotConfigured
	}

	now := s.now()
	jobs, err := s.outbox.Claim(ctx, now, s.outboxPolicy.Lease, s.outboxPolicy.BatchSize)
	if err != nil {
		return report, fmt.Errorf("email outbox: claim: %w", err)
	}
	report.Claimed = len(jobs)

	for _, job := range jobs {
		outcome, reason, err := s.processEmailJob(ctx, job)
		if err == nil {
			switch outcome {
			case emailJobDelivered:
				report.Delivered++
			case emailJobNoOp:
				report.NoOps++
			}
			if completeErr := s.outbox.Complete(ctx, job.ID); completeErr != nil {
				return report, fmt.Errorf("email outbox: complete: %w", completeErr)
			}
			continue
		}

		if job.Attempts >= s.outboxPolicy.MaxAttempts {
			if deadErr := s.outbox.MarkDead(ctx, job.ID, reason); deadErr != nil {
				return report, fmt.Errorf("email outbox: mark dead: %w", deadErr)
			}
			report.Dead++
			continue
		}
		next := now.Add(s.outboxPolicy.Backoff(job.Attempts))
		if rescheduleErr := s.outbox.Reschedule(ctx, job.ID, next, reason); rescheduleErr != nil {
			return report, fmt.Errorf("email outbox: reschedule: %w", rescheduleErr)
		}
		report.Retried++
	}
	return report, nil
}

type emailJobOutcome int

const (
	emailJobDelivered emailJobOutcome = iota
	emailJobNoOp
)

func (s *PasswordService) processEmailJob(ctx context.Context, job EmailJob) (emailJobOutcome, string, error) {
	switch job.Kind {
	case EmailJobPasswordReset:
		return s.processPasswordResetJob(ctx, job)
	case EmailJobVerificationResend:
		return s.processVerificationResendJob(ctx, job)
	default:
		return emailJobNoOp, emailFailureKind, fmt.Errorf("email outbox: unknown job kind %q", job.Kind)
	}
}

func (s *PasswordService) processPasswordResetJob(ctx context.Context, job EmailJob) (emailJobOutcome, string, error) {
	credential, err := s.credentials.FindByEmail(ctx, job.Email)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			return emailJobNoOp, "", nil
		}
		return emailJobNoOp, emailFailureLookup, fmt.Errorf("reset job: look up credential: %w", err)
	}

	raw, digest, err := s.tokens.Generate()
	if err != nil {
		return emailJobNoOp, emailFailureStore, fmt.Errorf("reset job: generate token: %w", err)
	}
	now := s.now()
	if err := s.resets.Create(ctx, domain.PasswordReset{
		ID:          newRandomID(),
		AccountID:   credential.AccountID,
		TokenDigest: digest,
		ExpiresAt:   now.Add(PasswordResetTokenTTL),
		CreatedAt:   now,
	}); err != nil {
		return emailJobNoOp, emailFailureStore, fmt.Errorf("reset job: store token: %w", err)
	}
	if err := s.email.SendPasswordReset(ctx, job.Email, raw); err != nil {
		return emailJobNoOp, emailFailureSend, fmt.Errorf("reset job: send: %w", err)
	}
	return emailJobDelivered, "", nil
}

func (s *PasswordService) processVerificationResendJob(ctx context.Context, job EmailJob) (emailJobOutcome, string, error) {
	existing, err := s.pending.FindByEmail(ctx, job.Email)
	if err != nil {
		if errors.Is(err, domain.ErrVerificationTokenInvalid) {
			return emailJobNoOp, "", nil
		}
		return emailJobNoOp, emailFailureLookup, fmt.Errorf("resend job: look up pending signup: %w", err)
	}
	if existing.ConsumedAt != nil {
		return emailJobNoOp, "", nil
	}

	raw, digest, err := s.tokens.Generate()
	if err != nil {
		return emailJobNoOp, emailFailureStore, fmt.Errorf("resend job: generate token: %w", err)
	}
	now := s.now()
	if err := s.pending.Upsert(ctx, domain.PendingSignup{
		ID:           existing.ID,
		Email:        existing.Email,
		PasswordHash: existing.PasswordHash,
		TokenDigest:  digest,
		ExpiresAt:    now.Add(VerificationTokenTTL),
		CreatedAt:    now,
	}); err != nil {
		return emailJobNoOp, emailFailureStore, fmt.Errorf("resend job: store pending signup: %w", err)
	}
	if err := s.email.SendEmailVerification(ctx, job.Email, raw); err != nil {
		return emailJobNoOp, emailFailureSend, fmt.Errorf("resend job: send: %w", err)
	}
	return emailJobDelivered, "", nil
}
