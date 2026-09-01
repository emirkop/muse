package application

import (
	"context"
	"fmt"

	"muse-backend/internal/identity/domain"
)

type LogoutService struct {
	sessions SessionRepository
}

func NewLogoutService(sessions SessionRepository) *LogoutService {
	return &LogoutService{sessions: sessions}
}

func (s *LogoutService) Logout(ctx context.Context, rawRefreshToken string) error {
	digest := domain.DigestRefreshToken(rawRefreshToken)

	session, _, err := s.sessions.FindByRefreshDigest(ctx, digest)
	if err != nil {
		return err
	}

	if err := s.sessions.RevokeSession(ctx, session.ID); err != nil {
		return fmt.Errorf("logout: revoke session: %w", err)
	}
	return nil
}
