package infrastructure

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	"muse-backend/internal/identity/domain"
)

const (
	AppleIssuer  = "https://appleid.apple.com"
	AppleJWKSURL = "https://appleid.apple.com/auth/keys"
)

type AppleVerifier struct {
	audience string
	jwks     *JWKSClient
}

func NewAppleVerifier(audience string, jwksClient *JWKSClient) *AppleVerifier {
	return &AppleVerifier{audience: audience, jwks: jwksClient}
}

func (v *AppleVerifier) Verify(ctx context.Context, identityToken string, expectedNonce string) (domain.ExternalIdentity, error) {
	claims := jwt.MapClaims{}

	parsed, err := jwt.ParseWithClaims(identityToken, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("apple: unexpected signing method %v (expected ES256)", token.Header["alg"])
		}

		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("apple: token header missing kid")
		}

		key, err := v.jwks.KeyForID(ctx, kid)
		if err != nil {
			return nil, fmt.Errorf("apple: resolve signing key: %w", err)
		}

		ecKey, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("apple: key %q is not an ECDSA key", kid)
		}
		return ecKey, nil
	},
		jwt.WithIssuer(AppleIssuer),
		jwt.WithAudience(v.audience),
		jwt.WithValidMethods([]string{"ES256"}),
	)
	if err != nil || !parsed.Valid {
		return domain.ExternalIdentity{}, fmt.Errorf("%w: %v", domain.ErrIdentityVerificationFailed, err)
	}

	if expectedNonce != "" {
		if err := verifyAppleNonce(claims, expectedNonce); err != nil {
			return domain.ExternalIdentity{}, err
		}
	}

	subject, err := claims.GetSubject()
	if err != nil || subject == "" {
		return domain.ExternalIdentity{}, fmt.Errorf("%w: missing subject claim", domain.ErrIdentityVerificationFailed)
	}

	email, _ := claims["email"].(string)
	emailVerified := parseAppleBoolClaim(claims["email_verified"])

	return domain.ExternalIdentity{
		Provider:      domain.ProviderApple,
		Subject:       subject,
		Email:         email,
		EmailVerified: emailVerified,
	}, nil
}

func verifyAppleNonce(claims jwt.MapClaims, expectedRawNonce string) error {
	tokenNonce, _ := claims["nonce"].(string)
	if tokenNonce == "" {
		return fmt.Errorf("%w: nonce expected but token carries none", domain.ErrIdentityVerificationFailed)
	}

	sum := sha256.Sum256([]byte(expectedRawNonce))
	expectedDigest := hex.EncodeToString(sum[:])

	if tokenNonce != expectedDigest {
		return fmt.Errorf("%w: nonce mismatch", domain.ErrIdentityVerificationFailed)
	}
	return nil
}

func parseAppleBoolClaim(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true"
	default:
		return false
	}
}
