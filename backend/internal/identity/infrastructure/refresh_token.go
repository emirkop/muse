package infrastructure

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"muse-backend/internal/identity/domain"
)

const refreshTokenByteLength = 32

func GenerateRefreshToken() (raw string, digest string, err error) {
	buf := make([]byte, refreshTokenByteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("refresh_token: generate random value: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(buf)
	digest = domain.DigestRefreshToken(raw)
	return raw, digest, nil
}

type OpaqueRefreshTokenGenerator struct{}

func (OpaqueRefreshTokenGenerator) Generate() (raw string, digest string, err error) {
	return GenerateRefreshToken()
}
