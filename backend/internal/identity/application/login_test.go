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

type fakeVerifier struct {
	identity domain.ExternalIdentity
	err      error
}

func (f fakeVerifier) VerifyApple(_ context.Context, _, _ string) (domain.ExternalIdentity, error) {
	return f.identity, f.err
}

func (f fakeVerifier) VerifyGoogle(_ context.Context, _ string) (domain.ExternalIdentity, error) {
	return f.identity, f.err
}

func newTestLoginService(verifier application.IdentityVerifier) (*application.LoginService, *infrastructure.InMemorySessionStore) {
	sessions := infrastructure.NewInMemorySessionStore()
	accounts := infrastructure.NewInMemoryAccountResolver()
	accessTokens := infrastructure.NewAccessTokenSigner([]byte("test-key"), "muse-test", 15*time.Minute)
	refreshGen := infrastructure.OpaqueRefreshTokenGenerator{}

	svc := application.NewLoginService(verifier, accounts, sessions, accessTokens, refreshGen, 180*24*time.Hour, 30*24*time.Hour)
	return svc, sessions
}

func TestLoginService_LoginWithApple_Success(t *testing.T) {
	verifier := fakeVerifier{identity: domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-1"}}
	svc, _ := newTestLoginService(verifier)

	result, err := svc.LoginWithApple(context.Background(), "irrelevant-token", "")
	if err != nil {
		t.Fatalf("LoginWithApple: %v", err)
	}

	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatal("expected non-empty access and refresh tokens")
	}
	if !result.AccessTokenExpiresAt.After(time.Now()) {
		t.Error("expected AccessTokenExpiresAt to be in the future")
	}
	if !result.RefreshTokenExpiresAt.After(time.Now()) {
		t.Error("expected RefreshTokenExpiresAt to be in the future")
	}
}

func TestLoginService_SameIdentity_ReusesSameAccountAcrossLogins(t *testing.T) {
	verifier := fakeVerifier{identity: domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-1"}}
	svc, sessions := newTestLoginService(verifier)
	ctx := context.Background()

	first, err := svc.LoginWithApple(ctx, "token-1", "")
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	second, err := svc.LoginWithApple(ctx, "token-2", "")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}

	firstSession, _, err := sessions.FindByRefreshDigest(ctx, domain.DigestRefreshToken(first.RefreshToken))
	if err != nil {
		t.Fatalf("find first session: %v", err)
	}
	secondSession, _, err := sessions.FindByRefreshDigest(ctx, domain.DigestRefreshToken(second.RefreshToken))
	if err != nil {
		t.Fatalf("find second session: %v", err)
	}

	if firstSession.AccountID != secondSession.AccountID {
		t.Fatalf("expected both logins for the same identity to resolve to the same account, got %q and %q", firstSession.AccountID, secondSession.AccountID)
	}
	if firstSession.ID == secondSession.ID {
		t.Fatal("expected each login to create a distinct session, even for the same account")
	}
}

func TestLoginService_VerificationFailure_PropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	verifier := fakeVerifier{err: wantErr}
	svc, _ := newTestLoginService(verifier)

	_, err := svc.LoginWithApple(context.Background(), "bad-token", "")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the verifier's error to propagate, got %v", err)
	}
}
