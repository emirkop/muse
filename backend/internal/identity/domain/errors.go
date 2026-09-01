package domain

import "errors"

var (
	ErrIdentityVerificationFailed = errors.New("identity: provider token verification failed")
	ErrSessionNotFound            = errors.New("identity: session not found")
	ErrSessionRevoked             = errors.New("identity: session has been revoked")
	ErrSessionExpired             = errors.New("identity: session has expired")
	ErrRefreshTokenNotFound       = errors.New("identity: refresh token not recognized")
	ErrRefreshTokenExpired        = errors.New("identity: refresh token has expired")
	ErrRefreshTokenReused         = errors.New("identity: refresh token reuse detected")

	ErrAccountNotFound             = errors.New("identity: account not found")
	ErrAccountDeactivated          = errors.New("identity: account has been deactivated")
	ErrLinkedIdentityAlreadyExists = errors.New("identity: external identity already linked to an account")

	ErrInvalidAvatarID = errors.New("identity: avatar_id is not one of the predefined avatars")
)
