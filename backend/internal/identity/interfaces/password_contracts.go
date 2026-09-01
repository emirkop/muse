package interfaces

import (
	"fmt"

	"muse-backend/internal/identity/domain"
)

type emailSignUpRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type emailLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type emailAddressRequest struct {
	Email string `json:"email"`
}

type emailVerificationRequest struct {
	Token string `json:"token"`
}

type passwordResetConfirmRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type verificationPendingResponse struct {
	Status string `json:"status"`
}

var weakPasswordMessage = fmt.Sprintf(
	"choose a password between %d and %d characters",
	domain.PasswordMinimumLength,
	domain.PasswordMaximumLength,
)
