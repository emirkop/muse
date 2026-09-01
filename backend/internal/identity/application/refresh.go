package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"muse-backend/internal/identity/domain"
)

type RefreshService struct {
	sessions     SessionRepository
	accessTokens AccessTokenIssuer
	refreshGen   RefreshTokenGenerator
	refreshTTL   time.Duration
}

func NewRefreshService(
	sessions SessionRepository,
	accessTokens AccessTokenIssuer,
	refreshGen RefreshTokenGenerator,
	refreshTTL time.Duration,
) *RefreshService {
	return &RefreshService{
		sessions:     sessions,
		accessTokens: accessTokens,
		refreshGen:   refreshGen,
		refreshTTL:   refreshTTL,
	}
}

func (s *RefreshService) Refresh(ctx context.Context, rawRefreshToken string) (SessionResult, error) {
	digest := domain.DigestRefreshToken(rawRefreshToken)

	session, refresh, err := s.sessions.FindByRefreshDigest(ctx, digest)
	if err != nil {
		return SessionResult{}, err
	}

	now := time.Now()

	if refresh.WasRotated() {
		if revokeErr := s.sessions.RevokeFamily(ctx, refresh.FamilyID); revokeErr != nil {
			return SessionResult{}, fmt.Errorf("refresh: reuse detected AND family revocation failed: %w", errors.Join(domain.ErrRefreshTokenReused, revokeErr))
		}
		return SessionResult{}, domain.ErrRefreshTokenReused
	}

	if session.IsRevoked() {
		return SessionResult{}, domain.ErrSessionRevoked
	}
	if session.IsExpired(now) {
		return SessionResult{}, domain.ErrSessionExpired
	}
	if refresh.IsExpired(now) {
		return SessionResult{}, domain.ErrRefreshTokenExpired
	}

	rawNewRefresh, newDigest, err := s.refreshGen.Generate()
	if err != nil {
		return SessionResult{}, fmt.Errorf("refresh: generate replacement refresh token: %w", err)
	}

	newRefreshExpiresAt := now.Add(s.refreshTTL)
	newRefresh := domain.RefreshToken{
		SessionID: session.ID,
		FamilyID:  refresh.FamilyID,
		Digest:    newDigest,
		IssuedAt:  now,
		ExpiresAt: newRefreshExpiresAt,
	}

	if err := s.sessions.RotateRefreshToken(ctx, digest, newRefresh); err != nil {
		return SessionResult{}, fmt.Errorf("refresh: rotate refresh token: %w", err)
	}

	accessToken, accessExpiresAt, err := s.accessTokens.Sign(session.AccountID, session.ID)
	if err != nil {
		return SessionResult{}, fmt.Errorf("refresh: sign access token: %w", err)
	}

	return SessionResult{
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshToken:          rawNewRefresh,
		RefreshTokenExpiresAt: newRefreshExpiresAt,
	}, nil
}
