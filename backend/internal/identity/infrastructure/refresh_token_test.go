package infrastructure

import (
	"testing"

	"muse-backend/internal/identity/domain"
)

func TestGenerateRefreshToken_ProducesDistinctValues(t *testing.T) {
	raw1, digest1, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken (1): %v", err)
	}
	raw2, digest2, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken (2): %v", err)
	}

	if raw1 == raw2 {
		t.Fatal("expected two calls to produce distinct raw values")
	}
	if digest1 == digest2 {
		t.Fatal("expected two calls to produce distinct digests")
	}
}

func TestGenerateRefreshToken_DigestMatchesDomainDigest(t *testing.T) {
	raw, digest, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}

	if want := domain.DigestRefreshToken(raw); digest != want {
		t.Fatalf("expected digest %q to match domain.DigestRefreshToken(raw) %q", digest, want)
	}
}

func TestOpaqueRefreshTokenGenerator_ImplementsGenerate(t *testing.T) {
	var gen OpaqueRefreshTokenGenerator

	raw, digest, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if raw == "" || digest == "" {
		t.Fatal("expected non-empty raw value and digest")
	}
}
