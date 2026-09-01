package application

import (
	"context"

	"muse-backend/internal/identity/domain"
)

type PasswordHasher interface {
	Hash(password string) (encoded string, err error)

	Verify(password, encoded string) (ok bool, needsRehash bool, err error)

	VerifyDecoy(password string)
}

type OpaqueTokenGenerator interface {
	Generate() (raw string, digest string, err error)
}

type TransactionalEmailSender interface {
	SendEmailVerification(ctx context.Context, to domain.EmailAddress, token string) error

	SendPasswordReset(ctx context.Context, to domain.EmailAddress, token string) error

	SendSignupOnExistingAccount(ctx context.Context, to domain.EmailAddress) error
}

type PasswordCredentialRepository interface {
	CreateAccountWithCredential(ctx context.Context, account domain.Account, credential domain.PasswordCredential) (domain.Account, error)

	FindByEmail(ctx context.Context, email domain.EmailAddress) (domain.PasswordCredential, error)

	FindByAccountID(ctx context.Context, accountID domain.AccountID) (domain.PasswordCredential, error)

	UpdateHash(ctx context.Context, accountID domain.AccountID, encodedHash string) error
}

type PendingSignupRepository interface {
	Upsert(ctx context.Context, signup domain.PendingSignup) error

	FindByTokenDigest(ctx context.Context, digest string) (domain.PendingSignup, error)

	Consume(ctx context.Context, id string) error

	FindByEmail(ctx context.Context, email domain.EmailAddress) (domain.PendingSignup, error)
}

type PasswordResetRepository interface {
	Create(ctx context.Context, reset domain.PasswordReset) error

	FindByTokenDigest(ctx context.Context, digest string) (domain.PasswordReset, error)

	Consume(ctx context.Context, id string) error

	InvalidateAllForAccount(ctx context.Context, accountID domain.AccountID) error
}

type AttemptScope string

const (
	AttemptScopeLogin              AttemptScope = "login"
	AttemptScopePasswordReset      AttemptScope = "password_reset"
	AttemptScopeVerificationResend AttemptScope = "verification_resend"
	AttemptScopeSignup             AttemptScope = "signup"
)

type AttemptLimiter interface {
	Check(ctx context.Context, scope AttemptScope, key string) error

	RecordFailure(ctx context.Context, scope AttemptScope, key string) error

	Reset(ctx context.Context, scope AttemptScope, key string) error
}
