package interfaces

import (
	"errors"
	"net"
	"net/http"

	"muse-backend/internal/identity/domain"
	"muse-backend/internal/platform/observability"
)

func (h *Handlers) HandleEmailSignUp(w http.ResponseWriter, r *http.Request) {
	if h.passwords == nil {
		writeError(w, http.StatusServiceUnavailable, "email sign-up is not available")
		return
	}
	var req emailSignUpRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.passwords.SignUp(r.Context(), req.Email, req.Password, sourceKey(r))
	if err != nil {
		h.writePasswordError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, verificationPendingResponse{Status: "verification_sent"})
}

func (h *Handlers) HandleEmailVerification(w http.ResponseWriter, r *http.Request) {
	if h.passwords == nil {
		writeError(w, http.StatusServiceUnavailable, "email verification is not available")
		return
	}
	var req emailVerificationRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	result, err := h.passwords.VerifyEmail(r.Context(), req.Token)
	if err != nil {
		h.writePasswordError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newSessionResponse(result))
}

func (h *Handlers) HandleEmailVerificationResend(w http.ResponseWriter, r *http.Request) {
	if h.passwords == nil {
		writeError(w, http.StatusServiceUnavailable, "email verification is not available")
		return
	}
	var req emailAddressRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.passwords.ResendVerification(r.Context(), req.Email, sourceKey(r)); err != nil {
		h.writePasswordError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, verificationPendingResponse{Status: "verification_sent"})
}

func (h *Handlers) HandleEmailLogin(w http.ResponseWriter, r *http.Request) {
	if h.passwords == nil {
		writeError(w, http.StatusServiceUnavailable, "email log-in is not available")
		return
	}
	var req emailLoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	result, err := h.passwords.LogIn(r.Context(), req.Email, req.Password, sourceKey(r))
	if err != nil {
		h.writePasswordError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newSessionResponse(result))
}

func (h *Handlers) HandlePasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	if h.passwords == nil {
		writeError(w, http.StatusServiceUnavailable, "password reset is not available")
		return
	}
	var req emailAddressRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.passwords.RequestPasswordReset(r.Context(), req.Email, sourceKey(r)); err != nil {
		h.writePasswordError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, verificationPendingResponse{Status: "reset_requested"})
}

func (h *Handlers) HandlePasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	if h.passwords == nil {
		writeError(w, http.StatusServiceUnavailable, "password reset is not available")
		return
	}
	var req passwordResetConfirmRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.passwords.ResetPassword(r.Context(), req.Token, req.Password); err != nil {
		h.writePasswordError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (h *Handlers) writePasswordError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrTooManyAttempts):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:     observability.EventThrottled,
			Category: observability.CategoryAuthn,
			Outcome:  observability.OutcomeRefused,
		})
		writeError(w, http.StatusTooManyRequests, "too many attempts, please try again later")

	case errors.Is(err, domain.ErrInvalidEmail):
		writeError(w, http.StatusBadRequest, "enter a valid email address")

	case errors.Is(err, domain.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, weakPasswordMessage)

	case errors.Is(err, domain.ErrVerificationTokenInvalid):
		writeError(w, http.StatusBadRequest, "this verification link is no longer valid")

	case errors.Is(err, domain.ErrResetTokenInvalid):
		writeError(w, http.StatusBadRequest, "this password reset link is no longer valid")

	case errors.Is(err, domain.ErrCredentialNotFound),
		errors.Is(err, domain.ErrInvalidPassword),
		errors.Is(err, domain.ErrAccountDeactivated):
		writeError(w, http.StatusUnauthorized, "email or password is incorrect")

	default:
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:     observability.EventLoginFailed,
			Category: observability.CategoryAuthn,
			Outcome:  observability.OutcomeFailed,
		})
		writeError(w, http.StatusInternalServerError, "something went wrong, please try again")
	}
}

func sourceKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return ""
	}
	return domain.DigestOpaqueToken("source:" + host)
}
