package infrastructure

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"muse-backend/internal/sharing/domain"
)

type RandomCodeGenerator struct{}

func (RandomCodeGenerator) NewCode() (domain.Code, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("sharing: random code: %w", err)
	}
	code := base64.RawURLEncoding.EncodeToString(raw[:])
	if !domain.IsPlausibleCode(code) {
		return "", fmt.Errorf("sharing: generated code %q is not plausible", code)
	}
	return domain.Code(code), nil
}
