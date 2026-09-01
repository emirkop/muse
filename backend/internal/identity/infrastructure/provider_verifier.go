package infrastructure

import (
	"context"

	"muse-backend/internal/identity/domain"
)

type ProviderVerifier struct {
	apple  *AppleVerifier
	google *GoogleVerifier
}

func NewProviderVerifier(apple *AppleVerifier, google *GoogleVerifier) *ProviderVerifier {
	return &ProviderVerifier{apple: apple, google: google}
}

func (p *ProviderVerifier) VerifyApple(ctx context.Context, identityToken, nonce string) (domain.ExternalIdentity, error) {
	return p.apple.Verify(ctx, identityToken, nonce)
}

func (p *ProviderVerifier) VerifyGoogle(ctx context.Context, identityToken string) (domain.ExternalIdentity, error) {
	return p.google.Verify(ctx, identityToken)
}
