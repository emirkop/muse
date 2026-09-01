package infrastructure

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"muse-backend/internal/identity/domain"
)

type AccessTokenSigner struct {
	signingKey []byte
	issuer     string
	ttl        time.Duration
}

func NewAccessTokenSigner(signingKey []byte, issuer string, ttl time.Duration) *AccessTokenSigner {
	return &AccessTokenSigner{signingKey: signingKey, issuer: issuer, ttl: ttl}
}

func (s *AccessTokenSigner) Sign(accountID domain.AccountID, sessionID domain.SessionID) (token string, expiresAt time.Time, err error) {
	now := time.Now()
	expiresAt = now.Add(s.ttl)

	claims := jwt.MapClaims{
		"iss": s.issuer,
		"sub": string(accountID),
		"sid": string(sessionID),
		"iat": now.Unix(),
		"exp": expiresAt.Unix(),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.signingKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("access_token: sign: %w", err)
	}
	return signed, expiresAt, nil
}

func (s *AccessTokenSigner) Verify(token string) (domain.AccessTokenClaims, error) {
	claims := jwt.MapClaims{}

	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("access_token: unexpected signing method %v", t.Header["alg"])
		}
		return s.signingKey, nil
	},
		jwt.WithIssuer(s.issuer),
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil || !parsed.Valid {
		return domain.AccessTokenClaims{}, fmt.Errorf("access_token: verify: %w", err)
	}

	subject, _ := claims.GetSubject()
	sessionID, _ := claims["sid"].(string)
	expiresAt, err := claims.GetExpirationTime()
	if err != nil || expiresAt == nil {
		return domain.AccessTokenClaims{}, fmt.Errorf("access_token: missing expiry claim")
	}

	return domain.AccessTokenClaims{
		AccountID: domain.AccountID(subject),
		SessionID: domain.SessionID(sessionID),
		ExpiresAt: expiresAt.Time,
	}, nil
}
