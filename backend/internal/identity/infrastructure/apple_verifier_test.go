package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"muse-backend/internal/identity/domain"
)

const testAppleAudience = "com.muse.app.test"

func TestAppleVerifier_ValidToken_Succeeds(t *testing.T) {
	priv := generateECKeyPair(t)
	server := serveECJWKS(t, "apple-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewAppleVerifier(testAppleAudience, client)

	claims := jwt.MapClaims{
		"iss":            AppleIssuer,
		"aud":            testAppleAudience,
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
		"sub":            "apple-subject-123",
		"email":          "user@privaterelay.appleid.com",
		"email_verified": "true",
	}
	token := signES256(t, priv, "apple-test-kid", claims)

	identity, err := verifier.Verify(context.Background(), token, "")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if identity.Provider != domain.ProviderApple {
		t.Errorf("expected provider %q, got %q", domain.ProviderApple, identity.Provider)
	}
	if identity.Subject != "apple-subject-123" {
		t.Errorf("expected subject %q, got %q", "apple-subject-123", identity.Subject)
	}
	if !identity.EmailVerified {
		t.Error("expected EmailVerified true")
	}
}

func TestAppleVerifier_WithValidNonce_Succeeds(t *testing.T) {
	priv := generateECKeyPair(t)
	server := serveECJWKS(t, "apple-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewAppleVerifier(testAppleAudience, client)

	rawNonce := "client-generated-nonce-value"
	sum := sha256.Sum256([]byte(rawNonce))
	hashedNonce := hex.EncodeToString(sum[:])

	claims := jwt.MapClaims{
		"iss":   AppleIssuer,
		"aud":   testAppleAudience,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"sub":   "apple-subject-nonce",
		"nonce": hashedNonce,
	}
	token := signES256(t, priv, "apple-test-kid", claims)

	if _, err := verifier.Verify(context.Background(), token, rawNonce); err != nil {
		t.Fatalf("Verify with correct nonce: %v", err)
	}
}

func TestAppleVerifier_WrongNonce_Fails(t *testing.T) {
	priv := generateECKeyPair(t)
	server := serveECJWKS(t, "apple-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewAppleVerifier(testAppleAudience, client)

	sum := sha256.Sum256([]byte("the-real-nonce"))
	claims := jwt.MapClaims{
		"iss":   AppleIssuer,
		"aud":   testAppleAudience,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"sub":   "apple-subject-nonce",
		"nonce": hex.EncodeToString(sum[:]),
	}
	token := signES256(t, priv, "apple-test-kid", claims)

	_, err := verifier.Verify(context.Background(), token, "a-different-nonce-entirely")
	if err == nil {
		t.Fatal("expected an error for a mismatched nonce, got nil")
	}
}

func TestAppleVerifier_WrongIssuer_Fails(t *testing.T) {
	priv := generateECKeyPair(t)
	server := serveECJWKS(t, "apple-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewAppleVerifier(testAppleAudience, client)

	claims := jwt.MapClaims{
		"iss": "https://not-apple.example.com",
		"aud": testAppleAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "apple-subject-123",
	}
	token := signES256(t, priv, "apple-test-kid", claims)

	if _, err := verifier.Verify(context.Background(), token, ""); err == nil {
		t.Fatal("expected an error for a wrong issuer, got nil")
	}
}

func TestAppleVerifier_WrongAudience_Fails(t *testing.T) {
	priv := generateECKeyPair(t)
	server := serveECJWKS(t, "apple-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewAppleVerifier(testAppleAudience, client)

	claims := jwt.MapClaims{
		"iss": AppleIssuer,
		"aud": "com.someone.else",
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "apple-subject-123",
	}
	token := signES256(t, priv, "apple-test-kid", claims)

	if _, err := verifier.Verify(context.Background(), token, ""); err == nil {
		t.Fatal("expected an error for a wrong audience, got nil")
	}
}

func TestAppleVerifier_ExpiredToken_Fails(t *testing.T) {
	priv := generateECKeyPair(t)
	server := serveECJWKS(t, "apple-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewAppleVerifier(testAppleAudience, client)

	claims := jwt.MapClaims{
		"iss": AppleIssuer,
		"aud": testAppleAudience,
		"exp": time.Now().Add(-time.Hour).Unix(),
		"sub": "apple-subject-123",
	}
	token := signES256(t, priv, "apple-test-kid", claims)

	if _, err := verifier.Verify(context.Background(), token, ""); err == nil {
		t.Fatal("expected an error for an expired token, got nil")
	}
}

func TestAppleVerifier_TamperedSignature_Fails(t *testing.T) {
	priv := generateECKeyPair(t)
	server := serveECJWKS(t, "apple-test-kid", &priv.PublicKey)
	defer server.Close()
	client := NewJWKSClient(server.URL, nil, time.Hour)
	verifier := NewAppleVerifier(testAppleAudience, client)

	forgedKey := generateECKeyPair(t)
	claims := jwt.MapClaims{
		"iss": AppleIssuer,
		"aud": testAppleAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "apple-subject-123",
	}
	forgedToken := signES256(t, forgedKey, "apple-test-kid", claims)

	if _, err := verifier.Verify(context.Background(), forgedToken, ""); err == nil {
		t.Fatal("expected an error for a token signed with the wrong key, got nil")
	}
}
