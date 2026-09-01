package infrastructure

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"muse-backend/internal/identity/domain"
)

const testGoogleAudience = "test-client-id.apps.googleusercontent.com"

func TestGoogleVerifier_ValidToken_Succeeds(t *testing.T) {
	priv := generateRSAKeyPair(t)
	server := serveRSAJWKS(t, "google-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewGoogleVerifier(testGoogleAudience, client)

	claims := jwt.MapClaims{
		"iss":            "https://accounts.google.com",
		"aud":            testGoogleAudience,
		"exp":            time.Now().Add(time.Hour).Unix(),
		"sub":            "google-subject-456",
		"email":          "user@gmail.com",
		"email_verified": true,
	}
	token := signRS256(t, priv, "google-test-kid", claims)

	identity, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if identity.Provider != domain.ProviderGoogle {
		t.Errorf("expected provider %q, got %q", domain.ProviderGoogle, identity.Provider)
	}
	if identity.Subject != "google-subject-456" {
		t.Errorf("expected subject %q, got %q", "google-subject-456", identity.Subject)
	}
	if !identity.EmailVerified {
		t.Error("expected EmailVerified true")
	}
}

func TestGoogleVerifier_AcceptsBothValidIssuerForms(t *testing.T) {
	for _, iss := range []string{"https://accounts.google.com", "accounts.google.com"} {
		t.Run(iss, func(t *testing.T) {
			priv := generateRSAKeyPair(t)
			server := serveRSAJWKS(t, "google-test-kid", &priv.PublicKey)
			defer server.Close()
			client := NewJWKSClient(server.URL, nil, time.Hour)
			verifier := NewGoogleVerifier(testGoogleAudience, client)

			claims := jwt.MapClaims{
				"iss": iss,
				"aud": testGoogleAudience,
				"exp": time.Now().Add(time.Hour).Unix(),
				"sub": "google-subject-456",
			}
			token := signRS256(t, priv, "google-test-kid", claims)

			if _, err := verifier.Verify(context.Background(), token); err != nil {
				t.Fatalf("Verify with issuer %q: %v", iss, err)
			}
		})
	}
}

func TestGoogleVerifier_WrongIssuer_Fails(t *testing.T) {
	priv := generateRSAKeyPair(t)
	server := serveRSAJWKS(t, "google-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewGoogleVerifier(testGoogleAudience, client)

	claims := jwt.MapClaims{
		"iss": "https://not-google.example.com",
		"aud": testGoogleAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "google-subject-456",
	}
	token := signRS256(t, priv, "google-test-kid", claims)

	if _, err := verifier.Verify(context.Background(), token); err == nil {
		t.Fatal("expected an error for a wrong issuer, got nil")
	}
}

func TestGoogleVerifier_WrongAudience_Fails(t *testing.T) {
	priv := generateRSAKeyPair(t)
	server := serveRSAJWKS(t, "google-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewGoogleVerifier(testGoogleAudience, client)

	claims := jwt.MapClaims{
		"iss": "https://accounts.google.com",
		"aud": "someone-elses-client-id",
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "google-subject-456",
	}
	token := signRS256(t, priv, "google-test-kid", claims)

	if _, err := verifier.Verify(context.Background(), token); err == nil {
		t.Fatal("expected an error for a wrong audience, got nil")
	}
}

func TestGoogleVerifier_ExpiredToken_Fails(t *testing.T) {
	priv := generateRSAKeyPair(t)
	server := serveRSAJWKS(t, "google-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewGoogleVerifier(testGoogleAudience, client)

	claims := jwt.MapClaims{
		"iss": "https://accounts.google.com",
		"aud": testGoogleAudience,
		"exp": time.Now().Add(-time.Hour).Unix(),
		"sub": "google-subject-456",
	}
	token := signRS256(t, priv, "google-test-kid", claims)

	if _, err := verifier.Verify(context.Background(), token); err == nil {
		t.Fatal("expected an error for an expired token, got nil")
	}
}

func TestGoogleVerifier_TamperedSignature_Fails(t *testing.T) {
	priv := generateRSAKeyPair(t)
	server := serveRSAJWKS(t, "google-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewGoogleVerifier(testGoogleAudience, client)

	forgedKey := generateRSAKeyPair(t)
	claims := jwt.MapClaims{
		"iss": "https://accounts.google.com",
		"aud": testGoogleAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "google-subject-456",
	}
	forgedToken := signRS256(t, forgedKey, "google-test-kid", claims)

	if _, err := verifier.Verify(context.Background(), forgedToken); err == nil {
		t.Fatal("expected an error for a token signed with the wrong key, got nil")
	}
}
