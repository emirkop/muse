package interfaces

import (
	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
)

type appleLoginRequest struct {
	IdentityToken string `json:"identity_token"`
	Nonce         string `json:"nonce,omitempty"`
}

type googleLoginRequest struct {
	IdentityToken string `json:"identity_token"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type sessionResponse struct {
	AccessToken           string `json:"access_token"`
	AccessTokenExpiresAt  string `json:"access_token_expires_at"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresAt string `json:"refresh_token_expires_at"`
	IsNewAccount          bool   `json:"is_new_account"`
}

func newSessionResponse(result application.SessionResult) sessionResponse {
	const layout = "2006-01-02T15:04:05Z07:00"
	return sessionResponse{
		AccessToken:           result.AccessToken,
		AccessTokenExpiresAt:  result.AccessTokenExpiresAt.Format(layout),
		RefreshToken:          result.RefreshToken,
		RefreshTokenExpiresAt: result.RefreshTokenExpiresAt.Format(layout),
		IsNewAccount:          result.IsNewAccount,
	}
}

type updateProfileRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	AvatarID    *string `json:"avatar_id,omitempty"`
}

type profileResponse struct {
	DisplayName string `json:"display_name"`
	AvatarID    string `json:"avatar_id"`
}

func newProfileResponse(account domain.Account) profileResponse {
	return profileResponse{
		DisplayName: account.DisplayName,
		AvatarID:    string(account.AvatarID),
	}
}

type publicProfileResponse struct {
	AvatarID string `json:"avatar_id"`
}

func newPublicProfileResponse(account domain.Account) publicProfileResponse {
	return publicProfileResponse{AvatarID: string(account.AvatarID)}
}

type errorResponse struct {
	Error string `json:"error"`
}
