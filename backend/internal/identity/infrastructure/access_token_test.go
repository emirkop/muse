package infrastructure

import (
	"testing"
	"time"

	"muse-backend/internal/identity/domain"
)

func TestAccessTokenSigner_SignAndVerify_RoundTrips(t *testing.T) {
	signer := NewAccessTokenSigner([]byte("test-signing-key"), "muse-backend-test", 15*time.Minute)

	token, expiresAt, err := signer.Sign("acct_1", "sess_1")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	claims, err := signer.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if claims.AccountID != "acct_1" {
		t.Errorf("expected AccountID %q, got %q", "acct_1", claims.AccountID)
	}
	if claims.SessionID != "sess_1" {
		t.Errorf("expected SessionID %q, got %q", "sess_1", claims.SessionID)
	}
	diff := claims.ExpiresAt.Sub(expiresAt)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("expected ExpiresAt near %v, got %v", expiresAt, claims.ExpiresAt)
	}
}

func TestAccessTokenSigner_ExpiredToken_FailsVerification(t *testing.T) {
	signer := NewAccessTokenSigner([]byte("test-signing-key"), "muse-backend-test", -time.Minute)

	token, _, err := signer.Sign(domain.AccountID("acct_1"), domain.SessionID("sess_1"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if _, err := signer.Verify(token); err == nil {
		t.Fatal("expected an error verifying an already-expired token, got nil")
	}
}

func TestAccessTokenSigner_WrongSigningKey_FailsVerification(t *testing.T) {
	signer := NewAccessTokenSigner([]byte("key-a"), "muse-backend-test", 15*time.Minute)
	token, _, err := signer.Sign("acct_1", "sess_1")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	otherSigner := NewAccessTokenSigner([]byte("key-b"), "muse-backend-test", 15*time.Minute)
	if _, err := otherSigner.Verify(token); err == nil {
		t.Fatal("expected an error verifying a token signed with a different key, got nil")
	}
}

func TestAccessTokenSigner_WrongIssuer_FailsVerification(t *testing.T) {
	signer := NewAccessTokenSigner([]byte("test-signing-key"), "issuer-a", 15*time.Minute)
	token, _, err := signer.Sign("acct_1", "sess_1")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	otherSigner := NewAccessTokenSigner([]byte("test-signing-key"), "issuer-b", 15*time.Minute)
	if _, err := otherSigner.Verify(token); err == nil {
		t.Fatal("expected an error verifying a token with an unexpected issuer, got nil")
	}
}
