package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
)

func TestRefreshService_ValidToken_RotatesAndReturnsNewTokens(t *testing.T) {
	verifier := fakeVerifier{identity: domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-1"}}
	loginSvc, sessions := newTestLoginService(verifier)
	ctx := context.Background()

	loginResult, err := loginSvc.LoginWithApple(ctx, "token", "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	accessTokens := infrastructure.NewAccessTokenSigner([]byte("test-key"), "muse-test", 15*time.Minute)
	refreshGen := infrastructure.OpaqueRefreshTokenGenerator{}
	refreshSvc := application.NewRefreshService(sessions, accessTokens, refreshGen, 30*24*time.Hour)

	refreshed, err := refreshSvc.Refresh(ctx, loginResult.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if refreshed.RefreshToken == loginResult.RefreshToken {
		t.Fatal("expected rotation to produce a new refresh token, got the same one back")
	}
	if refreshed.AccessToken == "" {
		t.Fatal("expected a new access token")
	}
}

func TestRefreshService_ReuseOfRotatedToken_RevokesFamilyAndFails(t *testing.T) {
	verifier := fakeVerifier{identity: domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-1"}}
	loginSvc, sessions := newTestLoginService(verifier)
	ctx := context.Background()

	loginResult, err := loginSvc.LoginWithApple(ctx, "token", "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	accessTokens := infrastructure.NewAccessTokenSigner([]byte("test-key"), "muse-test", 15*time.Minute)
	refreshGen := infrastructure.OpaqueRefreshTokenGenerator{}
	refreshSvc := application.NewRefreshService(sessions, accessTokens, refreshGen, 30*24*time.Hour)

	firstRefresh, err := refreshSvc.Refresh(ctx, loginResult.RefreshToken)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	_, err = refreshSvc.Refresh(ctx, loginResult.RefreshToken)
	if !errors.Is(err, domain.ErrRefreshTokenReused) {
		t.Fatalf("expected ErrRefreshTokenReused, got %v", err)
	}

	_, err = refreshSvc.Refresh(ctx, firstRefresh.RefreshToken)
	if !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("expected the legitimately-rotated token to also be rejected after family revocation (ErrSessionRevoked), got %v", err)
	}
}

func TestRefreshService_UnknownToken_Fails(t *testing.T) {
	sessions := infrastructure.NewInMemorySessionStore()
	accessTokens := infrastructure.NewAccessTokenSigner([]byte("test-key"), "muse-test", 15*time.Minute)
	refreshGen := infrastructure.OpaqueRefreshTokenGenerator{}
	refreshSvc := application.NewRefreshService(sessions, accessTokens, refreshGen, 30*24*time.Hour)

	_, err := refreshSvc.Refresh(context.Background(), "a-token-that-was-never-issued")
	if !errors.Is(err, domain.ErrRefreshTokenNotFound) {
		t.Fatalf("expected ErrRefreshTokenNotFound, got %v", err)
	}
}

func TestRefreshService_RevokedSession_Fails(t *testing.T) {
	verifier := fakeVerifier{identity: domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-1"}}
	loginSvc, sessions := newTestLoginService(verifier)
	ctx := context.Background()

	loginResult, err := loginSvc.LoginWithApple(ctx, "token", "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	session, _, err := sessions.FindByRefreshDigest(ctx, domain.DigestRefreshToken(loginResult.RefreshToken))
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	if err := sessions.RevokeSession(ctx, session.ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	accessTokens := infrastructure.NewAccessTokenSigner([]byte("test-key"), "muse-test", 15*time.Minute)
	refreshGen := infrastructure.OpaqueRefreshTokenGenerator{}
	refreshSvc := application.NewRefreshService(sessions, accessTokens, refreshGen, 30*24*time.Hour)

	_, err = refreshSvc.Refresh(ctx, loginResult.RefreshToken)
	if !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked, got %v", err)
	}
}
