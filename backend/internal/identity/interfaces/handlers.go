package interfaces

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	platformhttp "muse-backend/internal/platform/http"
	"muse-backend/internal/platform/observability"
)

var errMissingBearerToken = errors.New("interfaces: missing bearer token")

type Handlers struct {
	login        *application.LoginService
	refresh      *application.RefreshService
	logout       *application.LogoutService
	accounts     *application.AccountService
	accessTokens application.AccessTokenIssuer
	passwords    *application.PasswordService
	logger       *slog.Logger
}

func NewHandlers(
	login *application.LoginService,
	refresh *application.RefreshService,
	logout *application.LogoutService,
	accounts *application.AccountService,
	passwords *application.PasswordService,
	accessTokens application.AccessTokenIssuer,
	logger *slog.Logger,
) *Handlers {
	return &Handlers{
		login:        login,
		refresh:      refresh,
		logout:       logout,
		accounts:     accounts,
		passwords:    passwords,
		accessTokens: accessTokens,
		logger:       logger,
	}
}

func (h *Handlers) RegisterRoutes(router *platformhttp.Router) {
	router.Handle("POST /auth/apple", h.HandleAppleLogin)
	router.Handle("POST /auth/google", h.HandleGoogleLogin)
	router.Handle("POST /auth/refresh", h.HandleRefresh)
	router.Handle("POST /auth/logout", h.HandleLogout)

	router.Handle("POST /auth/email/signup", h.HandleEmailSignUp)
	router.Handle("POST /auth/email/verify", h.HandleEmailVerification)
	router.Handle("POST /auth/email/verification/resend", h.HandleEmailVerificationResend)
	router.Handle("POST /auth/email/login", h.HandleEmailLogin)
	router.Handle("POST /auth/email/password-reset", h.HandlePasswordResetRequest)
	router.Handle("POST /auth/email/password-reset/confirm", h.HandlePasswordResetConfirm)

	router.Handle("GET /profile/me", h.HandleGetOwnProfile)
	router.Handle("PATCH /profile/me", h.HandleUpdateOwnProfile)
	router.Handle("GET /profile/{accountID}", h.HandleGetProfile)
}

func (h *Handlers) HandleAppleLogin(w http.ResponseWriter, r *http.Request) {
	var req appleLoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.IdentityToken == "" {
		writeError(w, http.StatusBadRequest, "identity_token is required")
		return
	}

	result, err := h.login.LoginWithApple(r.Context(), req.IdentityToken, req.Nonce)
	if err != nil {
		h.handleLoginError(w, r, "apple", err)
		return
	}

	writeJSON(w, http.StatusOK, newSessionResponse(result))
}

func (h *Handlers) HandleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	var req googleLoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.IdentityToken == "" {
		writeError(w, http.StatusBadRequest, "identity_token is required")
		return
	}

	result, err := h.login.LoginWithGoogle(r.Context(), req.IdentityToken)
	if err != nil {
		h.handleLoginError(w, r, "google", err)
		return
	}

	writeJSON(w, http.StatusOK, newSessionResponse(result))
}

func (h *Handlers) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	result, err := h.refresh.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, domain.ErrRefreshTokenReused) {
			observability.Log(r.Context(), h.logger, observability.Event{
				Name:     observability.EventRefreshReuseDetected,
				Category: observability.CategoryAuthn,
				Outcome:  observability.OutcomeRefused,
				Reason:   observability.ReasonCredentialRejected,
			})
		}
		writeError(w, http.StatusUnauthorized, "refresh failed")
		return
	}

	writeJSON(w, http.StatusOK, newSessionResponse(result))
}

func (h *Handlers) HandleLogout(w http.ResponseWriter, r *http.Request) {
	var req logoutRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	if err := h.logout.Logout(r.Context(), req.RefreshToken); err != nil {
		writeJSON(w, http.StatusOK, struct{}{})
		return
	}

	writeJSON(w, http.StatusOK, struct{}{})
}

func (h *Handlers) HandleGetOwnProfile(w http.ResponseWriter, r *http.Request) {
	if h.accounts == nil {
		writeError(w, http.StatusServiceUnavailable, "profile is not available")
		return
	}

	accountID, err := h.authenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	account, err := h.accounts.FindByID(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	writeJSON(w, http.StatusOK, newProfileResponse(account))
}

func (h *Handlers) HandleUpdateOwnProfile(w http.ResponseWriter, r *http.Request) {
	if h.accounts == nil {
		writeError(w, http.StatusServiceUnavailable, "profile is not available")
		return
	}

	accountID, err := h.authenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req updateProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.DisplayName == nil && req.AvatarID == nil {
		writeError(w, http.StatusBadRequest, "at least one of display_name or avatar_id is required")
		return
	}

	if req.DisplayName != nil {
		if err := h.accounts.UpdateDisplayName(r.Context(), accountID, *req.DisplayName); err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
	}

	if req.AvatarID != nil {
		if err := h.accounts.UpdateAvatar(r.Context(), accountID, domain.AvatarID(*req.AvatarID)); err != nil {
			if errors.Is(err, domain.ErrInvalidAvatarID) {
				writeError(w, http.StatusBadRequest, "invalid avatar_id")
				return
			}
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
	}

	account, err := h.accounts.FindByID(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}

	writeJSON(w, http.StatusOK, newProfileResponse(account))
}

func (h *Handlers) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
	if h.accounts == nil {
		writeError(w, http.StatusServiceUnavailable, "profile is not available")
		return
	}

	callerID, err := h.authenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	accountID := domain.AccountID(r.PathValue("accountID"))
	account, err := h.accounts.FindByID(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if account.IsDeleted() {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	if account.ID == callerID {
		writeJSON(w, http.StatusOK, newProfileResponse(account))
		return
	}
	writeJSON(w, http.StatusOK, newPublicProfileResponse(account))
}

func (h *Handlers) authenticatedAccountID(r *http.Request) (domain.AccountID, error) {
	const bearerPrefix = "Bearer "

	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, bearerPrefix) {
		return "", errMissingBearerToken
	}

	token := strings.TrimPrefix(header, bearerPrefix)
	claims, err := h.accessTokens.Verify(token)
	if err != nil {
		return "", err
	}
	return claims.AccountID, nil
}

func (h *Handlers) handleLoginError(w http.ResponseWriter, r *http.Request, provider string, err error) {
	observability.LogWith(r.Context(), h.logger, observability.Event{
		Name:     observability.EventLoginRefused,
		Category: observability.CategoryAuthn,
		Outcome:  observability.OutcomeRefused,
		Reason:   observability.ReasonCredentialRejected,
	},
		"provider", provider,
	)
	writeError(w, http.StatusUnauthorized, "authentication failed")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, platformhttp.MaxJSONBodyBytes)).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
