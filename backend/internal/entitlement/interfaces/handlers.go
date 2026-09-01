package interfaces

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"muse-backend/internal/entitlement/application"
	"muse-backend/internal/entitlement/domain"
	platformhttp "muse-backend/internal/platform/http"
	"muse-backend/internal/platform/observability"
)

type AccountAuthenticating interface {
	AuthenticatedAccountID(r *http.Request) (string, error)
}

type Handlers struct {
	entitlements *application.EntitlementService
	auth         AccountAuthenticating
	logger       *slog.Logger
}

func NewHandlers(entitlements *application.EntitlementService, auth AccountAuthenticating, logger *slog.Logger) *Handlers {
	return &Handlers{entitlements: entitlements, auth: auth, logger: logger}
}

func (h *Handlers) RegisterRoutes(router *platformhttp.Router) {
	router.Handle("GET /entitlements/me", h.HandleStatus)
	router.Handle("POST /entitlements/app-account-token", h.HandleAppAccountToken)
	router.Handle("POST /entitlements/app-store/transactions", h.HandleRedeem)
	router.Handle("POST /app-store/notifications", h.HandleNotification)
}

func (h *Handlers) HandleStatus(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	status, err := h.entitlements.Status(r.Context(), accountID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newStatusResponse(status))
}

func (h *Handlers) HandleAppAccountToken(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	token, err := h.entitlements.AppAccountToken(r.Context(), accountID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, appAccountTokenResponse{AppAccountToken: token})
}

func (h *Handlers) HandleRedeem(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var req redeemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SignedTransaction == "" {
		writeError(w, http.StatusBadRequest, "signed_transaction is required", "invalid_signed_transaction")
		return
	}
	status, err := h.entitlements.RedeemSignedTransaction(r.Context(), accountID, req.SignedTransaction)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newStatusResponse(status))
}

func (h *Handlers) HandleNotification(w http.ResponseWriter, r *http.Request) {
	var req notificationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SignedPayload == "" {
		writeError(w, http.StatusBadRequest, "signedPayload is required", "invalid_signed_payload")
		return
	}
	if err := h.entitlements.ApplyNotification(r.Context(), req.SignedPayload); err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidSignedTransaction):
			writeError(w, http.StatusBadRequest, "notification did not verify", "invalid_signed_payload")
		case errors.Is(err, domain.ErrWrongBundle), errors.Is(err, domain.ErrWrongAppAppleID),
			errors.Is(err, domain.ErrWrongEnvironment), errors.Is(err, domain.ErrWrongProduct),
			errors.Is(err, domain.ErrNotNonConsumable), errors.Is(err, domain.ErrFamilyShared):
			observability.Log(r.Context(), h.logger, observability.Event{
				Name:     observability.EventEntitlementNotApplicable,
				Category: observability.CategoryEntitlement,
				Outcome:  observability.OutcomeRefused,
				Reason:   err.Error(),
			})
			writeError(w, http.StatusBadRequest, "notification does not apply to this deployment", "notification_not_applicable")
		case errors.Is(err, domain.ErrVerificationUnavailable):
			observability.Log(r.Context(), h.logger, observability.Event{
				Name:     observability.EventEntitlementVerificationUnavailable,
				Category: observability.CategoryEntitlement,
				Outcome:  observability.OutcomeUnavailable,
			})
			writeError(w, http.StatusServiceUnavailable, "notification verification is unavailable", "verification_unavailable")
		default:
			observability.Log(r.Context(), h.logger, observability.Event{
				Name:     observability.EventEntitlementVerificationFailed,
				Category: observability.CategoryEntitlement,
				Outcome:  observability.OutcomeFailed,
				Err:      err,
			})
			writeError(w, http.StatusInternalServerError, "internal error", "")
		}
		return
	}
	w.WriteHeader(http.StatusOK)
}

// MARK: - Plumbing

func (h *Handlers) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required", "")
		return "", false
	}
	return accountID, true
}

func (h *Handlers) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidSignedTransaction):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:      observability.EventEntitlementVerificationFailed,
			Category:  observability.CategoryEntitlement,
			Outcome:   observability.OutcomeRefused,
			Reason:    observability.ReasonCredentialRejected,
			AccountID: h.accountIDForLogging(r),
		})
		writeError(w, http.StatusBadRequest, "signed transaction did not verify", "invalid_signed_transaction")
	case errors.Is(err, domain.ErrWrongBundle), errors.Is(err, domain.ErrWrongProduct),
		errors.Is(err, domain.ErrNotNonConsumable), errors.Is(err, domain.ErrFamilyShared),
		errors.Is(err, domain.ErrWrongEnvironment), errors.Is(err, domain.ErrWrongAppAppleID),
		errors.Is(err, domain.ErrNoAppAccountToken):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:      observability.EventEntitlementNotApplicable,
			Category:  observability.CategoryEntitlement,
			Outcome:   observability.OutcomeRefused,
			Reason:    err.Error(),
			AccountID: h.accountIDForLogging(r),
		})
		writeError(w, http.StatusBadRequest, "this purchase does not apply to this account", "transaction_not_applicable")
	case errors.Is(err, domain.ErrAppAccountTokenMismatch):
		writeError(w, http.StatusConflict, "this purchase was made for a different account", "app_account_token_mismatch")
	case errors.Is(err, domain.ErrTransactionBoundToAnotherAccount):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:      observability.EventEntitlementBoundElsewhere,
			Category:  observability.CategoryEntitlement,
			Outcome:   observability.OutcomeRefused,
			AccountID: h.accountIDForLogging(r),
		})
		writeError(w, http.StatusConflict, "this purchase is already bound to another account", "transaction_bound_to_another_account")
	case errors.Is(err, domain.ErrVerificationUnavailable):
		writeError(w, http.StatusServiceUnavailable, "purchase verification is unavailable", "verification_unavailable")
	default:
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:      observability.EventEntitlementVerificationFailed,
			Category:  observability.CategoryEntitlement,
			Outcome:   observability.OutcomeFailed,
			AccountID: h.accountIDForLogging(r),
			Err:       err,
		})
		writeError(w, http.StatusInternalServerError, "internal error", "")
	}
}

func (h *Handlers) accountIDForLogging(r *http.Request) string {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		return ""
	}
	return accountID
}

// MARK: - Wire shapes

type redeemRequest struct {
	SignedTransaction string `json:"signed_transaction"`
}

type notificationRequest struct {
	SignedPayload string `json:"signedPayload"`
}

type appAccountTokenResponse struct {
	AppAccountToken string `json:"app_account_token"`
}

type statusResponse struct {
	State        string `json:"state"`
	ItemCapacity int    `json:"item_capacity"`
	ItemCount    int    `json:"item_count"`
}

func newStatusResponse(s application.AccountStatus) statusResponse {
	return statusResponse{
		State:        string(s.Entitlement.State),
		ItemCapacity: s.Entitlement.ItemCapacity,
		ItemCount:    s.ItemCount,
	}
}

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, errorResponse{Error: message, Code: code})
}
