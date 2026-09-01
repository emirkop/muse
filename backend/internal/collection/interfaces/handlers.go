package interfaces

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"muse-backend/internal/collection/application"
	"muse-backend/internal/collection/domain"
	platformhttp "muse-backend/internal/platform/http"
	"muse-backend/internal/platform/observability"
)

type AccountAuthenticating interface {
	AuthenticatedAccountID(r *http.Request) (string, error)
}

type Handlers struct {
	rooms         *application.CollectionRoomService
	auth          AccountAuthenticating
	itemAnalytics application.ItemRefusalRecording
	logger        *slog.Logger
}

func (h *Handlers) WithItemAnalytics(recorder application.ItemRefusalRecording) *Handlers {
	h.itemAnalytics = recorder
	return h
}

func NewHandlers(
	rooms *application.CollectionRoomService,
	auth AccountAuthenticating,
	logger *slog.Logger,
) *Handlers {
	return &Handlers{rooms: rooms, auth: auth, logger: logger}
}

func (h *Handlers) RegisterRoutes(router *platformhttp.Router) {
	router.Handle("POST /collection-rooms", h.HandleCreate)
	router.Handle("GET /collection-rooms", h.HandleList)
	router.Handle("GET /collection-rooms/{collectionRoomID}", h.HandleGet)
	router.Handle("PATCH /collection-rooms/{collectionRoomID}", h.HandleUpdate)
	router.Handle("DELETE /collection-rooms/{collectionRoomID}", h.HandleDelete)
	router.Handle("POST /collection-rooms/{collectionRoomID}/tier", h.HandleRatchetTier)
	router.Handle("POST /collection-rooms/{collectionRoomID}/items", h.HandleAddItem)
	router.Handle("PUT /collection-rooms/{collectionRoomID}/items/{collectionItemID}/slot", h.HandlePlaceItem)
	router.Handle("PUT /collection-rooms/{collectionRoomID}/music", h.HandleAssignMusic)
	router.Handle("DELETE /collection-rooms/{collectionRoomID}/music", h.HandleRemoveMusic)
}

func (h *Handlers) HandleAssignMusic(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var req assignMusicRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	room, err := h.rooms.AssignMusic(
		r.Context(), accountID, domain.CollectionRoomID(r.PathValue("collectionRoomID")), req.MusicTrackID,
	)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newCollectionRoomResponse(room))
}

func (h *Handlers) HandleRemoveMusic(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	room, err := h.rooms.RemoveMusic(
		r.Context(), accountID, domain.CollectionRoomID(r.PathValue("collectionRoomID")),
	)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newCollectionRoomResponse(room))
}

func (h *Handlers) HandleAddItem(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	appAssetVersion, ok := appAssetVersionFrom(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid app_asset_version", "invalid_app_asset_version")
		return
	}
	var req createCollectionItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	room, err := h.rooms.AddItem(
		r.Context(),
		accountID,
		domain.CollectionRoomID(r.PathValue("collectionRoomID")),
		req.CatalogModelID,
		appAssetVersion,
	)
	if err != nil {
		h.recordItemAddRefusal(r.Context(), accountID, err)
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newCollectionRoomResponse(room))
}

func (h *Handlers) recordItemAddRefusal(ctx context.Context, accountID string, err error) {
	if h.itemAnalytics == nil {
		return
	}
	var reason string
	switch {
	case errors.Is(err, domain.ErrModelNotAvailable):
		reason = "model_not_placeable"
	case errors.Is(err, domain.ErrTierCapacityReached):
		reason = "tier_capacity_reached"
	case errors.Is(err, domain.ErrItemCapacityReached):
		reason = "item_capacity_reached"
	case errors.Is(err, domain.ErrDesignLayoutUnavailable):
		reason = "design_layout_unavailable"
	case errors.Is(err, domain.ErrSlotNotAvailable):
		reason = "slot_not_available"
	case errors.Is(err, domain.ErrCollectionRoomNotFound):
		reason = "room_not_found"
	default:
		return
	}
	h.itemAnalytics.RecordItemAddRefused(ctx, accountID, reason)
}

func (h *Handlers) HandlePlaceItem(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	appAssetVersion, ok := appAssetVersionFrom(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid app_asset_version", "invalid_app_asset_version")
		return
	}
	var req placeCollectionItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	room, err := h.rooms.PlaceItemAtSlot(
		r.Context(),
		accountID,
		domain.CollectionRoomID(r.PathValue("collectionRoomID")),
		domain.CollectionItemID(r.PathValue("collectionItemID")),
		req.SlotIndex,
		appAssetVersion,
	)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newCollectionRoomResponse(room))
}

const (
	defaultAppAssetVersion = 1
	maxAppAssetVersion     = 1000
)

func appAssetVersionFrom(r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("app_asset_version")
	if raw == "" {
		return defaultAppAssetVersion, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > maxAppAssetVersion {
		return 0, false
	}
	return parsed, true
}

func (h *Handlers) HandleRatchetTier(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req ratchetTierRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	room, err := h.rooms.RatchetTier(
		r.Context(),
		accountID,
		domain.CollectionRoomID(r.PathValue("collectionRoomID")),
		domain.Tier(req.Tier),
	)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newCollectionRoomResponse(room))
}

func (h *Handlers) HandleCreate(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req createCollectionRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	room, err := h.rooms.Create(r.Context(), accountID, application.CreateInput{
		Name:       req.Name,
		CategoryID: req.CategoryID,
		DesignID:   req.DesignID,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newCollectionRoomResponse(room))
}

func (h *Handlers) HandleList(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	rooms, err := h.rooms.List(r.Context(), accountID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	payload := collectionRoomListResponse{CollectionRooms: make([]collectionRoomResponse, 0, len(rooms))}
	for _, room := range rooms {
		payload.CollectionRooms = append(payload.CollectionRooms, newCollectionRoomResponse(room))
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handlers) HandleGet(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	room, err := h.rooms.Find(r.Context(), accountID, domain.CollectionRoomID(r.PathValue("collectionRoomID")))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newCollectionRoomResponse(room))
}

func (h *Handlers) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req updateCollectionRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	room, err := h.rooms.Update(
		r.Context(),
		accountID,
		domain.CollectionRoomID(r.PathValue("collectionRoomID")),
		domain.CollectionRoomPatch{
			Name:       req.Name,
			CategoryID: req.CategoryID,
			DesignID:   req.DesignID,
		},
	)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newCollectionRoomResponse(room))
}

func (h *Handlers) HandleDelete(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	err := h.rooms.Delete(r.Context(), accountID, domain.CollectionRoomID(r.PathValue("collectionRoomID")))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required", "")
		return "", false
	}
	return accountID, true
}

func (h *Handlers) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	h.logRefusal(r, err)
	switch {
	case errors.Is(err, domain.ErrCollectionRoomNotFound),
		errors.Is(err, domain.ErrNotOwner):
		writeError(w, http.StatusNotFound, "not found", "")

	case errors.Is(err, domain.ErrItemNotInRoom):
		writeError(w, http.StatusNotFound, "item is not in this collection room", "item_not_in_room")

	case errors.Is(err, domain.ErrModelReferenceRequired):
		writeError(w, http.StatusBadRequest, "catalog_model_id is required", "model_required")
	case errors.Is(err, domain.ErrInvalidModelReference):
		writeError(w, http.StatusBadRequest, "catalog_model_id is malformed", "invalid_model")
	case errors.Is(err, domain.ErrModelNotAvailable):
		writeError(w, http.StatusBadRequest,
			"that catalog model is not available for this collection room", "model_not_available")
	case errors.Is(err, domain.ErrInvalidSlotIndex):
		writeError(w, http.StatusBadRequest, "slot_index must not be negative", "invalid_slot_index")
	case errors.Is(err, domain.ErrSlotNotAvailable):
		writeError(w, http.StatusBadRequest,
			"that slot is not available at the room's current tier", "slot_not_available")
	case errors.Is(err, domain.ErrItemCapacityReached):
		writeError(w, http.StatusPaymentRequired,
			"this account's item capacity is reached", "item_capacity_reached")
	case errors.Is(err, domain.ErrTierCapacityReached):
		writeError(w, http.StatusBadRequest,
			"the room's current tier is full — expand it before adding", "tier_capacity_reached")
	case errors.Is(err, domain.ErrDesignRequiredForItems):
		writeError(w, http.StatusBadRequest,
			"choose a design before adding or arranging items", "design_required")
	case errors.Is(err, domain.ErrDesignLayoutUnavailable):
		writeError(w, http.StatusBadRequest,
			"this room's design has no published slot table for its current tier", "design_layout_unavailable")
	case errors.Is(err, domain.ErrItemSlotTaken):
		writeError(w, http.StatusConflict, "that display slot is already taken", "slot_taken")
	case errors.Is(err, domain.ErrTransactionsUnavailable):
		writeError(w, http.StatusServiceUnavailable,
			"collection item storage is not configured", "")

	case errors.Is(err, domain.ErrNameRequired):
		writeError(w, http.StatusBadRequest, "name is required", "name_required")
	case errors.Is(err, domain.ErrNameTooLong):
		writeError(w, http.StatusBadRequest, "name is too long", "name_too_long")
	case errors.Is(err, domain.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "name is not valid text", "invalid_name")
	case errors.Is(err, domain.ErrInvalidCategoryReference):
		writeError(w, http.StatusBadRequest, "category_id is malformed", "invalid_category")
	case errors.Is(err, domain.ErrCategoryRequired):
		writeError(w, http.StatusBadRequest, "category_id is required", "category_required")
	case errors.Is(err, domain.ErrUnknownCategory):
		writeError(w, http.StatusBadRequest, "category is not in the catalog", "unknown_category")
	case errors.Is(err, domain.ErrInvalidDesignReference):
		writeError(w, http.StatusBadRequest, "design_id is malformed", "invalid_design")
	case errors.Is(err, domain.ErrUnknownMusicTrack):
		writeError(w, http.StatusBadRequest, "music track is not in the catalog", "unknown_music_track")
	case errors.Is(err, domain.ErrDesignNotApplicable):
		writeError(w, http.StatusBadRequest,
			"design is not available for this collection room's category", "design_not_applicable")
	case errors.Is(err, domain.ErrInvalidTier):
		writeError(w, http.StatusBadRequest, "tier must be at least 1", "invalid_tier")
	case errors.Is(err, domain.ErrTierNotAuthored):
		writeError(w, http.StatusBadRequest,
			"this room's design does not have that tier", "tier_not_authored")
	case errors.Is(err, domain.ErrDesignRequiredForTier):
		writeError(w, http.StatusBadRequest,
			"choose a design before this room can expand", "design_required")
	case errors.Is(err, domain.ErrEmptyPatch):
		writeError(w, http.StatusBadRequest, "no fields to update", "empty_patch")

	case errors.Is(err, domain.ErrStorageUnavailable):
		writeError(w, http.StatusServiceUnavailable, "collection storage is not configured", "")
	default:
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:      observability.EventPersistenceFailed,
			Category:  observability.CategoryPersistence,
			Outcome:   observability.OutcomeFailed,
			AccountID: h.accountIDForLogging(r),
			Err:       err,
		})
		writeError(w, http.StatusInternalServerError, "request failed", "")
	}
}

func (h *Handlers) logRefusal(r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrCollectionRoomNotFound):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:      observability.EventAuthorizationRefused,
			Category:  observability.CategoryAuthz,
			Outcome:   observability.OutcomeRefused,
			Reason:    observability.ReasonNotFound,
			AccountID: h.accountIDForLogging(r),
		})
	case errors.Is(err, domain.ErrItemCapacityReached):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:      observability.EventEntitlementCapacityReached,
			Category:  observability.CategoryEntitlement,
			Outcome:   observability.OutcomeRefused,
			AccountID: h.accountIDForLogging(r),
		})
	case errors.Is(err, domain.ErrTransactionsUnavailable):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:      observability.EventDependencyUnavailable,
			Category:  observability.CategoryConfig,
			Outcome:   observability.OutcomeUnavailable,
			Reason:    observability.ReasonNotConfigured,
			AccountID: h.accountIDForLogging(r),
		})
	}
}

func (h *Handlers) accountIDForLogging(r *http.Request) string {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		return ""
	}
	return accountID
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, platformhttp.MaxJSONBodyBytes)).Decode(dst); err != nil {
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
