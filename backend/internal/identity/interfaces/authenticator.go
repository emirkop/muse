package interfaces

import (
	"net/http"
	"strings"

	"muse-backend/internal/identity/application"
)

type BearerAuthenticator struct {
	accessTokens application.AccessTokenIssuer
}

func NewBearerAuthenticator(accessTokens application.AccessTokenIssuer) *BearerAuthenticator {
	return &BearerAuthenticator{accessTokens: accessTokens}
}

func (a *BearerAuthenticator) AuthenticatedAccountID(r *http.Request) (string, error) {
	const bearerPrefix = "Bearer "

	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, bearerPrefix) {
		return "", errMissingBearerToken
	}

	claims, err := a.accessTokens.Verify(strings.TrimPrefix(header, bearerPrefix))
	if err != nil {
		return "", err
	}
	return string(claims.AccountID), nil
}
