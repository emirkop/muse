package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"muse-backend/internal/identity/domain"
	"muse-backend/internal/platform/observability"
)

const (
	VerificationTokenTTL  = 24 * time.Hour
	PasswordResetTokenTTL = 1 * time.Hour
)

type PasswordService struct {
	credentials  PasswordCredentialRepository
	pending      PendingSignupRepository
	resets       PasswordResetRepository
	accounts     AccountRepository
	sessions     SessionRepository
	issuer       *LoginService
	hasher       PasswordHasher
	tokens       OpaqueTokenGenerator
	email        TransactionalEmailSender
	limiter      AttemptLimiter
	outbox       EmailOutbox
	outboxPolicy EmailOutboxPolicy
	now          func() time.Time
	logger       *slog.Logger
}

func NewPasswordService(
	credentials PasswordCredentialRepository,
	pending PendingSignupRepository,
	resets PasswordResetRepository,
	accounts AccountRepository,
	sessions SessionRepository,
	issuer *LoginService,
	hasher PasswordHasher,
	tokens OpaqueTokenGenerator,
	email TransactionalEmailSender,
	limiter AttemptLimiter,
	outbox EmailOutbox,
) *PasswordService {
	return &PasswordService{
		credentials:  credentials,
		pending:      pending,
		resets:       resets,
		accounts:     accounts,
		sessions:     sessions,
		issuer:       issuer,
		hasher:       hasher,
		tokens:       tokens,
		email:        email,
		limiter:      limiter,
		outbox:       outbox,
		outboxPolicy: DefaultEmailOutboxPolicy,
		now:          time.Now,
	}
}

func (s *PasswordService) WithLogger(logger *slog.Logger) *PasswordService {
	s.logger = logger
	return s
}

func (s *PasswordService) noteThrottleWrite(ctx context.Context, err error) {
	if err == nil {
		return
	}
	observability.Log(ctx, s.logger, observability.Event{
		Name:     observability.EventThrottleWriteFailed,
		Category: observability.CategoryAuthn,
		Outcome:  observability.OutcomeFailed,
		Err:      err,
	})
}

func (s *PasswordService) SetClock(now func() time.Time) { s.now = now }

func (s *PasswordService) SignUp(ctx context.Context, rawEmail, password, sourceKey string) error {
	email, err := domain.NormaliseEmail(rawEmail)
	if err != nil {
		return err
	}
	if err := domain.ValidatePassword(password); err != nil {
		return err
	}

	emailKey := domain.DigestOpaqueToken(email.String())
	if err := s.checkLimits(ctx, AttemptScopeSignup, emailKey, sourceKey); err != nil {
		return err
	}

	hash, err := s.hasher.Hash(password)
	if err != nil {
		return fmt.Errorf("signup: hash password: %w", err)
	}

	if _, err := s.credentials.FindByEmail(ctx, email); err == nil {
		s.noteThrottleWrite(ctx, s.limiter.RecordFailure(ctx, AttemptScopeSignup, emailKey))
		if sendErr := s.email.SendSignupOnExistingAccount(ctx, email); sendErr != nil {
			return fmt.Errorf("signup: notify existing account: %w", sendErr)
		}
		return nil
	} else if !errors.Is(err, domain.ErrCredentialNotFound) {
		return fmt.Errorf("signup: look up credential: %w", err)
	}

	raw, digest, err := s.tokens.Generate()
	if err != nil {
		return fmt.Errorf("signup: generate verification token: %w", err)
	}

	now := s.now()
	signup := domain.PendingSignup{
		ID:           newRandomID(),
		Email:        email,
		PasswordHash: hash,
		TokenDigest:  digest,
		ExpiresAt:    now.Add(VerificationTokenTTL),
		CreatedAt:    now,
	}
	if err := s.pending.Upsert(ctx, signup); err != nil {
		return fmt.Errorf("signup: store pending signup: %w", err)
	}

	if err := s.email.SendEmailVerification(ctx, email, raw); err != nil {
		return fmt.Errorf("signup: send verification: %w", err)
	}
	return nil
}

func (s *PasswordService) ResendVerification(ctx context.Context, rawEmail, sourceKey string) error {
	email, err := domain.NormaliseEmail(rawEmail)
	if err != nil {
		return err
	}

	emailKey := domain.DigestOpaqueToken(email.String())
	if err := s.checkLimits(ctx, AttemptScopeVerificationResend, emailKey, sourceKey); err != nil {
		return err
	}
	s.noteThrottleWrite(ctx, s.limiter.RecordFailure(ctx, AttemptScopeVerificationResend, emailKey))

	return s.enqueueEmail(ctx, EmailJobVerificationResend, email)
}

func (s *PasswordService) VerifyEmail(ctx context.Context, rawToken string) (SessionResult, error) {
	if rawToken == "" {
		return SessionResult{}, domain.ErrVerificationTokenInvalid
	}

	signup, err := s.pending.FindByTokenDigest(ctx, domain.DigestOpaqueToken(rawToken))
	if err != nil {
		return SessionResult{}, err
	}
	if !signup.IsUsable(s.now()) {
		return SessionResult{}, domain.ErrVerificationTokenInvalid
	}

	if err := s.pending.Consume(ctx, signup.ID); err != nil {
		return SessionResult{}, err
	}

	now := s.now()
	account := domain.Account{CreatedAt: now, UpdatedAt: now}
	credential := domain.PasswordCredential{
		Email:     signup.Email,
		Hash:      signup.PasswordHash,
		CreatedAt: now,
		UpdatedAt: now,
	}

	created, err := s.credentials.CreateAccountWithCredential(ctx, account, credential)
	if err != nil {
		if errors.Is(err, domain.ErrEmailAlreadyRegistered) {
			return SessionResult{}, domain.ErrVerificationTokenInvalid
		}
		return SessionResult{}, fmt.Errorf("verify: create account: %w", err)
	}

	return s.issuer.IssueSession(ctx, created.ID, true)
}

func (s *PasswordService) LogIn(ctx context.Context, rawEmail, password, sourceKey string) (SessionResult, error) {
	email, err := domain.NormaliseEmail(rawEmail)
	if err != nil {
		return SessionResult{}, domain.ErrCredentialNotFound
	}

	emailKey := domain.DigestOpaqueToken(email.String())
	if err := s.checkLimits(ctx, AttemptScopeLogin, emailKey, sourceKey); err != nil {
		return SessionResult{}, err
	}

	credential, err := s.credentials.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialNotFound) {
			s.hasher.VerifyDecoy(password)
			s.recordFailure(ctx, AttemptScopeLogin, emailKey, sourceKey)
			return SessionResult{}, domain.ErrCredentialNotFound
		}
		return SessionResult{}, fmt.Errorf("login: look up credential: %w", err)
	}

	ok, needsRehash, err := s.hasher.Verify(password, credential.Hash)
	if err != nil {
		return SessionResult{}, fmt.Errorf("login: verify password: %w", err)
	}
	if !ok {
		s.recordFailure(ctx, AttemptScopeLogin, emailKey, sourceKey)
		return SessionResult{}, domain.ErrInvalidPassword
	}

	account, err := s.accounts.FindByID(ctx, credential.AccountID)
	if err != nil {
		return SessionResult{}, fmt.Errorf("login: load account: %w", err)
	}
	if account.IsDeleted() {
		return SessionResult{}, domain.ErrAccountDeactivated
	}

	if needsRehash {
		if upgraded, hashErr := s.hasher.Hash(password); hashErr == nil {
			_ = s.credentials.UpdateHash(ctx, credential.AccountID, upgraded)
		}
	}

	s.noteThrottleWrite(ctx, s.limiter.Reset(ctx, AttemptScopeLogin, emailKey))
	s.noteThrottleWrite(ctx, s.limiter.Reset(ctx, AttemptScopeLogin, sourceKey))
	return s.issuer.IssueSession(ctx, credential.AccountID, false)
}

func (s *PasswordService) RequestPasswordReset(ctx context.Context, rawEmail, sourceKey string) error {
	email, err := domain.NormaliseEmail(rawEmail)
	if err != nil {
		return nil
	}

	emailKey := domain.DigestOpaqueToken(email.String())
	if err := s.checkLimits(ctx, AttemptScopePasswordReset, emailKey, sourceKey); err != nil {
		return err
	}
	s.noteThrottleWrite(ctx, s.limiter.RecordFailure(ctx, AttemptScopePasswordReset, emailKey))
	s.noteThrottleWrite(ctx, s.limiter.RecordFailure(ctx, AttemptScopePasswordReset, sourceKey))

	return s.enqueueEmail(ctx, EmailJobPasswordReset, email)
}

func (s *PasswordService) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	if rawToken == "" {
		return domain.ErrResetTokenInvalid
	}
	if err := domain.ValidatePassword(newPassword); err != nil {
		return err
	}

	reset, err := s.resets.FindByTokenDigest(ctx, domain.DigestOpaqueToken(rawToken))
	if err != nil {
		return err
	}
	if !reset.IsUsable(s.now()) {
		return domain.ErrResetTokenInvalid
	}

	if err := s.resets.Consume(ctx, reset.ID); err != nil {
		return err
	}

	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("reset: hash password: %w", err)
	}
	if err := s.credentials.UpdateHash(ctx, reset.AccountID, hash); err != nil {
		return fmt.Errorf("reset: update credential: %w", err)
	}
	if err := s.resets.InvalidateAllForAccount(ctx, reset.AccountID); err != nil {
		return fmt.Errorf("reset: invalidate outstanding tokens: %w", err)
	}
	if err := s.sessions.RevokeAllForAccount(ctx, reset.AccountID); err != nil {
		return fmt.Errorf("reset: revoke sessions: %w", err)
	}

	if credential, err := s.credentials.FindByAccountID(ctx, reset.AccountID); err == nil {
		s.noteThrottleWrite(ctx, s.limiter.Reset(ctx, AttemptScopeLogin, domain.DigestOpaqueToken(credential.Email.String())))
	}
	return nil
}

func (s *PasswordService) checkLimits(ctx context.Context, scope AttemptScope, emailKey, sourceKey string) error {
	if err := s.limiter.Check(ctx, scope, emailKey); err != nil {
		return err
	}
	if sourceKey != "" {
		if err := s.limiter.Check(ctx, scope, sourceKey); err != nil {
			return err
		}
	}
	return nil
}

func (s *PasswordService) recordFailure(ctx context.Context, scope AttemptScope, emailKey, sourceKey string) {
	s.noteThrottleWrite(ctx, s.limiter.RecordFailure(ctx, scope, emailKey))
	if sourceKey != "" {
		s.noteThrottleWrite(ctx, s.limiter.RecordFailure(ctx, scope, sourceKey))
	}
}
