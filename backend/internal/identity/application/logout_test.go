package application_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
)

func TestLogoutService_RevokesSession(t *testing.T) {
	verifier := fakeVerifier{identity: domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-1"}}
	loginSvc, sessions := newTestLoginService(verifier)
	ctx := context.Background()

	loginResult, err := loginSvc.LoginWithApple(ctx, "token", "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	logoutSvc := application.NewLogoutService(sessions)
	if err := logoutSvc.Logout(ctx, loginResult.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	session, _, err := sessions.FindByRefreshDigest(ctx, domain.DigestRefreshToken(loginResult.RefreshToken))
	if err != nil {
		t.Fatalf("find session after logout: %v", err)
	}
	if !session.IsRevoked() {
		t.Fatal("expected the session to be revoked after logout")
	}
}

func TestLogoutService_UnknownToken_ReturnsError(t *testing.T) {
	verifier := fakeVerifier{identity: domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-1"}}
	_, sessions := newTestLoginService(verifier)

	logoutSvc := application.NewLogoutService(sessions)
	err := logoutSvc.Logout(context.Background(), "never-issued-token")
	if !errors.Is(err, domain.ErrRefreshTokenNotFound) {
		t.Fatalf("expected ErrRefreshTokenNotFound, got %v", err)
	}
}
