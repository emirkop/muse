package infrastructure

import (
	"context"
	"crypto/rsa"
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	"muse-backend/internal/identity/domain"
)

var GoogleIssuers = []string{"https://accounts.google.com", "accounts.google.com"}

const GoogleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

type GoogleVerifier struct {
	audience string
	jwks     *JWKSClient
}

func NewGoogleVerifier(audience string, jwksClient *JWKSClient) *GoogleVerifier {
	return &GoogleVerifier{audience: audience, jwks: jwksClient}
}

func (v *GoogleVerifier) Verify(ctx context.Context, identityToken string) (domain.ExternalIdentity, error) {
	claims := jwt.MapClaims{}

	parsed, err := jwt.ParseWithClaims(identityToken, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("google: unexpected signing method %v (expected RS256)", token.Header["alg"])
		}

		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("google: token header missing kid")
		}

		key, err := v.jwks.KeyForID(ctx, kid)
		if err != nil {
			return nil, fmt.Errorf("google: resolve signing key: %w", err)
		}

		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("google: key %q is not an RSA key", kid)
		}
		return rsaKey, nil
	},
		jwt.WithAudience(v.audience),
		jwt.WithValidMethods([]string{"RS256"}),
	)
	if err != nil || !parsed.Valid {
		return domain.ExternalIdentity{}, fmt.Errorf("%w: %v", domain.ErrIdentityVerificationFailed, err)
	}

	if err := verifyGoogleIssuer(claims); err != nil {
		return domain.ExternalIdentity{}, err
	}

	subject, err := claims.GetSubject()
	if err != nil || subject == "" {
		return domain.ExternalIdentity{}, fmt.Errorf("%w: missing subject claim", domain.ErrIdentityVerificationFailed)
	}

	email, _ := claims["email"].(string)
	emailVerified, _ := claims["email_verified"].(bool)

	return domain.ExternalIdentity{
		Provider:      domain.ProviderGoogle,
		Subject:       subject,
		Email:         email,
		EmailVerified: emailVerified,
	}, nil
}

func verifyGoogleIssuer(claims jwt.MapClaims) error {
	iss, _ := claims.GetIssuer()
	for _, valid := range GoogleIssuers {
		if iss == valid {
			return nil
		}
	}
	return fmt.Errorf("%w: unexpected issuer %q", domain.ErrIdentityVerificationFailed, iss)
}
