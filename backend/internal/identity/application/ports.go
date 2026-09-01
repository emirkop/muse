package application

import (
	"context"
	"time"

	"muse-backend/internal/identity/domain"
)

type IdentityVerifier interface {
	VerifyApple(ctx context.Context, identityToken, nonce string) (domain.ExternalIdentity, error)
	VerifyGoogle(ctx context.Context, identityToken string) (domain.ExternalIdentity, error)
}

type AccessTokenIssuer interface {
	Sign(accountID domain.AccountID, sessionID domain.SessionID) (token string, expiresAt time.Time, err error)
	Verify(token string) (domain.AccessTokenClaims, error)
}

type RefreshTokenGenerator interface {
	Generate() (raw string, digest string, err error)
}

type AccountResolver interface {
	ResolveOrCreateAccount(ctx context.Context, identity domain.ExternalIdentity) (accountID domain.AccountID, isNewAccount bool, err error)
}

type AccountRepository interface {
	FindByLinkedIdentity(ctx context.Context, provider domain.Provider, subject string) (domain.Account, error)

	CreateWithLinkedIdentity(ctx context.Context, account domain.Account, identity domain.LinkedIdentity) (domain.Account, error)

	FindByID(ctx context.Context, id domain.AccountID) (domain.Account, error)

	UpdateDisplayName(ctx context.Context, id domain.AccountID, displayName string) error

	UpdateAvatar(ctx context.Context, id domain.AccountID, avatarID domain.AvatarID) error

	Deactivate(ctx context.Context, id domain.AccountID) error
}

type SessionRepository interface {
	CreateSession(ctx context.Context, session domain.Session, refresh domain.RefreshToken) error

	FindByRefreshDigest(ctx context.Context, digest string) (domain.Session, domain.RefreshToken, error)

	RotateRefreshToken(ctx context.Context, oldDigest string, newToken domain.RefreshToken) error

	RevokeSession(ctx context.Context, sessionID domain.SessionID) error

	RevokeAllForAccount(ctx context.Context, accountID domain.AccountID) error

	RevokeFamily(ctx context.Context, familyID domain.FamilyID) error
}
