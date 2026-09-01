package domain_test

import (
	"testing"
	"time"

	"muse-backend/internal/identity/domain"
)

func TestSession_IsActive(t *testing.T) {
	now := time.Now()

	active := domain.Session{ExpiresAt: now.Add(time.Hour)}
	if !active.IsActive(now) {
		t.Error("expected a non-revoked, non-expired session to be active")
	}

	expired := domain.Session{ExpiresAt: now.Add(-time.Hour)}
	if expired.IsActive(now) {
		t.Error("expected an expired session to not be active")
	}

	revokedAt := now.Add(-time.Minute)
	revoked := domain.Session{ExpiresAt: now.Add(time.Hour), RevokedAt: &revokedAt}
	if revoked.IsActive(now) {
		t.Error("expected a revoked session to not be active, even if not expired")
	}
}

func TestRefreshToken_WasRotated(t *testing.T) {
	fresh := domain.RefreshToken{}
	if fresh.WasRotated() {
		t.Error("expected a freshly issued refresh token to not be marked rotated")
	}

	rotatedAt := time.Now()
	rotated := domain.RefreshToken{RotatedAt: &rotatedAt}
	if !rotated.WasRotated() {
		t.Error("expected a refresh token with RotatedAt set to report WasRotated true")
	}
}

func TestDigestRefreshToken_IsDeterministicAndDistinct(t *testing.T) {
	digestA1 := domain.DigestRefreshToken("value-a")
	digestA2 := domain.DigestRefreshToken("value-a")
	digestB := domain.DigestRefreshToken("value-b")

	if digestA1 != digestA2 {
		t.Error("expected digesting the same value twice to produce the same digest")
	}
	if digestA1 == digestB {
		t.Error("expected digesting different values to produce different digests")
	}
	if digestA1 == "value-a" {
		t.Error("expected the digest to not simply be the raw input")
	}
}
