package interfaces

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"muse-backend/internal/media/application"
	"muse-backend/internal/media/domain"
	platformhttp "muse-backend/internal/platform/http"
	"muse-backend/internal/platform/observability"
)

type AccountAuthenticating interface {
	AuthenticatedAccountID(r *http.Request) (string, error)
}

type Handlers struct {
	media  *application.MediaService
	auth   AccountAuthenticating
	logger *slog.Logger
}

func NewHandlers(media *application.MediaService, auth AccountAuthenticating, logger *slog.Logger) *Handlers {
	return &Handlers{media: media, auth: auth, logger: logger}
}

func (h *Handlers) RegisterRoutes(router *platformhttp.Router) {
	router.Handle("POST /media/photo-uploads", h.HandleInitiatePhotoUpload)
}

func (h *Handlers) HandleInitiatePhotoUpload(w http.ResponseWriter, r *http.Request) {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required", "")
		return
	}

	var req initiatePhotoUploadRequest
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, platformhttp.MaxJSONBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	ticket, err := h.media.InitiatePhotoUpload(r.Context(), accountID, req.declaration())
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	status := http.StatusOK
	if ticket.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, newInitiatePhotoUploadResponse(ticket))
}

func (h *Handlers) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	h.logMediaOutcome(r, err)
	switch {
	case errors.Is(err, domain.ErrInvalidClientUploadID):
		writeError(w, http.StatusBadRequest, "client_upload_id is required", "invalid_declaration")
	case errors.Is(err, domain.ErrUnsupportedContentType):
		writeError(w, http.StatusBadRequest, "only image/jpeg is accepted", "unsupported_content_type")
	case errors.Is(err, domain.ErrPhotoTooLarge):
		writeError(w, http.StatusBadRequest, "photo exceeds the 10 MiB limit", "photo_too_large")
	case errors.Is(err, domain.ErrPhotoDimensions):
		writeError(w, http.StatusBadRequest, "photo dimensions are outside the accepted range (long edge ≤ 3072, short edge ≥ 320)", "photo_dimensions")
	case errors.Is(err, domain.ErrInvalidChecksum):
		writeError(w, http.StatusBadRequest, "checksum_sha256 must be 64 lowercase hex characters", "invalid_checksum")
	case errors.Is(err, domain.ErrDeclarationMismatch):
		writeError(w, http.StatusConflict, "this client_upload_id was already used with a different declaration", "declaration_mismatch")
	case errors.Is(err, domain.ErrAssetDiscarded):
		writeError(w, http.StatusGone, "this upload was discarded; start a new one", "asset_discarded")
	default:
		writeError(w, http.StatusInternalServerError, "request failed", "")
	}
}

func (h *Handlers) logMediaOutcome(r *http.Request, err error) {
	refusals := []error{
		domain.ErrAssetNotFound, domain.ErrAssetNotUploaded,
		domain.ErrPhotoTooLarge, domain.ErrPhotoDimensions, domain.ErrInvalidChecksum,
		domain.ErrDeclarationMismatch, domain.ErrAssetDiscarded, domain.ErrUnsupportedContentType,
		domain.ErrAssetInvalid, domain.ErrAssetNotPending, domain.ErrAssetNotCommitted,
		domain.ErrInvalidClientUploadID,
	}
	for _, refusal := range refusals {
		if errors.Is(err, refusal) {
			observability.Log(r.Context(), h.logger, observability.Event{
				Name:      observability.EventMediaUploadRefused,
				Category:  observability.CategoryMedia,
				Outcome:   observability.OutcomeRefused,
				Reason:    refusal.Error(),
				AccountID: h.accountIDForLogging(r),
			})
			return
		}
	}
	observability.Log(r.Context(), h.logger, observability.Event{
		Name:      observability.EventMediaStorageFailed,
		Category:  observability.CategoryMedia,
		Outcome:   observability.OutcomeFailed,
		AccountID: h.accountIDForLogging(r),
		Err:       err,
	})
}

func (h *Handlers) accountIDForLogging(r *http.Request) string {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		return ""
	}
	return accountID
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, errorResponse{Error: message, Code: code})
}
