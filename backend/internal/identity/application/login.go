package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"muse-backend/internal/identity/domain"
)

type SessionResult struct {
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time

	IsNewAccount bool
}

type LoginService struct {
	verifier     IdentityVerifier
	accounts     AccountResolver
	sessions     SessionRepository
	accessTokens AccessTokenIssuer
	refreshGen   RefreshTokenGenerator
	sessionTTL   time.Duration
	refreshTTL   time.Duration
}

func NewLoginService(
	verifier IdentityVerifier,
	accounts AccountResolver,
	sessions SessionRepository,
	accessTokens AccessTokenIssuer,
	refreshGen RefreshTokenGenerator,
	sessionTTL, refreshTTL time.Duration,
) *LoginService {
	return &LoginService{
		verifier:     verifier,
		accounts:     accounts,
		sessions:     sessions,
		accessTokens: accessTokens,
		refreshGen:   refreshGen,
		sessionTTL:   sessionTTL,
		refreshTTL:   refreshTTL,
	}
}

func (s *LoginService) LoginWithApple(ctx context.Context, identityToken, nonce string) (SessionResult, error) {
	identity, err := s.verifier.VerifyApple(ctx, identityToken, nonce)
	if err != nil {
		return SessionResult{}, err
	}
	return s.completeLogin(ctx, identity)
}

func (s *LoginService) LoginWithGoogle(ctx context.Context, identityToken string) (SessionResult, error) {
	identity, err := s.verifier.VerifyGoogle(ctx, identityToken)
	if err != nil {
		return SessionResult{}, err
	}
	return s.completeLogin(ctx, identity)
}

func (s *LoginService) completeLogin(ctx context.Context, identity domain.ExternalIdentity) (SessionResult, error) {
	accountID, isNewAccount, err := s.accounts.ResolveOrCreateAccount(ctx, identity)
	if err != nil {
		return SessionResult{}, fmt.Errorf("login: resolve account: %w", err)
	}
	return s.IssueSession(ctx, accountID, isNewAccount)
}

func (s *LoginService) IssueSession(ctx context.Context, accountID domain.AccountID, isNewAccount bool) (SessionResult, error) {
	sessionID := domain.SessionID(newRandomID())
	familyID := domain.FamilyID(newRandomID())
	now := time.Now()

	session := domain.Session{
		ID:        sessionID,
		AccountID: accountID,
		FamilyID:  familyID,
		CreatedAt: now,
		ExpiresAt: now.Add(s.sessionTTL),
	}

	rawRefresh, refreshDigest, err := s.refreshGen.Generate()
	if err != nil {
		return SessionResult{}, fmt.Errorf("login: generate refresh token: %w", err)
	}

	refreshExpiresAt := now.Add(s.refreshTTL)
	refresh := domain.RefreshToken{
		SessionID: sessionID,
		FamilyID:  familyID,
		Digest:    refreshDigest,
		IssuedAt:  now,
		ExpiresAt: refreshExpiresAt,
	}

	if err := s.sessions.CreateSession(ctx, session, refresh); err != nil {
		return SessionResult{}, fmt.Errorf("login: persist session: %w", err)
	}

	accessToken, accessExpiresAt, err := s.accessTokens.Sign(accountID, sessionID)
	if err != nil {
		return SessionResult{}, fmt.Errorf("login: sign access token: %w", err)
	}

	return SessionResult{
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshToken:          rawRefresh,
		RefreshTokenExpiresAt: refreshExpiresAt,
		IsNewAccount:          isNewAccount,
	}, nil
}

func newRandomID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("application: crypto/rand unavailable: %v", err))
	}
	return hex.EncodeToString(buf)
}
