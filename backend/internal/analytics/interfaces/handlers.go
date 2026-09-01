package interfaces

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"muse-backend/internal/analytics/application"
	"muse-backend/internal/analytics/domain"
	platformhttp "muse-backend/internal/platform/http"
)

type AccountAuthenticating interface {
	AuthenticatedAccountID(r *http.Request) (string, error)
}

const maxEventsPerRequest = 50

type Handlers struct {
	analytics *application.AnalyticsService
	auth      AccountAuthenticating
	logger    *slog.Logger
}

func NewHandlers(analytics *application.AnalyticsService, auth AccountAuthenticating, logger *slog.Logger) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handlers{analytics: analytics, auth: auth, logger: logger}
}

type eventBody struct {
	EventUUID      string  `json:"event_uuid"`
	Name           string  `json:"name"`
	Step           *string `json:"step,omitempty"`
	CategoryID     *string `json:"category_id,omitempty"`
	ResultBucket   *string `json:"result_bucket,omitempty"`
	Outcome        *string `json:"outcome,omitempty"`
	Reason         *string `json:"reason,omitempty"`
	Surface        *string `json:"surface,omitempty"`
	Classification *string `json:"classification,omitempty"`
	Retried        *bool   `json:"retried,omitempty"`
	RetrySucceeded *bool   `json:"retry_succeeded,omitempty"`
}

type requestBody struct {
	Events []eventBody `json:"events"`
}

type acceptedResponse struct {
	Accepted   int `json:"accepted"`
	Stored     int `json:"stored"`
	Duplicates int `json:"duplicates"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *Handlers) HandleRecordEvents(w http.ResponseWriter, r *http.Request) {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, platformhttp.MaxJSONBodyBytes))
	decoder.DisallowUnknownFields()

	var body requestBody
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.Events) == 0 || len(body.Events) > maxEventsPerRequest {
		writeError(w, http.StatusBadRequest, "invalid event count")
		return
	}

	drafts := make([]domain.Draft, 0, len(body.Events))
	for _, event := range body.Events {
		drafts = append(drafts, domain.Draft{
			UUID: event.EventUUID, Name: event.Name,
			Step: event.Step, CategoryID: event.CategoryID, Result: event.ResultBucket,
			Outcome: event.Outcome, Reason: event.Reason, Surface: event.Surface,
			Class: event.Classification, Retried: event.Retried, RetryOK: event.RetrySucceeded,
		})
	}

	accepted, err := h.analytics.RecordFromClient(r.Context(), accountID, drafts)
	if err != nil {
		if validation, ok := err.(domain.ValidationError); ok {
			writeError(w, http.StatusBadRequest, validation.Error())
			return
		}
		h.logger.Error("analytics request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}

	writeJSON(w, http.StatusAccepted, acceptedResponse{
		Accepted: accepted.Accepted, Stored: accepted.Stored, Duplicates: accepted.Duplicates,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
