package interfaces

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"muse-backend/internal/museum/application"
	"muse-backend/internal/museum/domain"
	platformhttp "muse-backend/internal/platform/http"
	"muse-backend/internal/platform/observability"
)

type AccountAuthenticating interface {
	AuthenticatedAccountID(r *http.Request) (string, error)
}

type Handlers struct {
	museums *application.MuseumService
	auth    AccountAuthenticating
	logger  *slog.Logger
}

func NewHandlers(museums *application.MuseumService, auth AccountAuthenticating, logger *slog.Logger) *Handlers {
	return &Handlers{museums: museums, auth: auth, logger: logger}
}

func (h *Handlers) RegisterRoutes(router *platformhttp.Router) {
	router.Handle("POST /museum", h.HandleCreateMuseum)
	router.Handle("GET /museum/me", h.HandleGetMuseum)
	router.Handle("PATCH /museum/me/style", h.HandleChangeStyle)
	router.Handle("PATCH /museum/me/privacy", h.HandleChangePrivacy)
	router.Handle("POST /museum/me/rooms", h.HandleCreateRoom)
	router.Handle("GET /museum/me/rooms", h.HandleListRooms)
	router.Handle("GET /museum/me/rooms/{roomID}", h.HandleGetRoom)
	router.Handle("PATCH /museum/me/rooms/{roomID}", h.HandleUpdateRoom)
	router.Handle("DELETE /museum/me/rooms/{roomID}", h.HandleDeleteRoom)

	router.Handle("GET /museums/{museumID}", h.HandleGetVisibleMuseum)
	router.Handle("GET /museums/{museumID}/rooms/{roomID}", h.HandleGetVisibleRoom)
	router.Handle("POST /museum/me/rooms/{roomID}/photos", h.HandleAddPhotos)
	router.Handle("GET /museum/me/rooms/{roomID}/photo-urls", h.HandlePhotoURLs)
	router.Handle("PUT /museum/me/rooms/{roomID}/photo-order", h.HandleReorderPhotos)
	router.Handle("PUT /museum/me/rooms/{roomID}/photos/{photoAssetID}/caption", h.HandleSetPhotoCaption)
	router.Handle("POST /museum/me/rooms/{roomID}/photos/{photoAssetID}/replacement", h.HandleReplacePhoto)
	router.Handle("DELETE /museum/me/rooms/{roomID}/photos/{photoAssetID}", h.HandleDeletePhoto)
	router.Handle("POST /museum/me/rooms/{roomID}/sculptures", h.HandleAddSculpture)
	router.Handle("DELETE /museum/me/rooms/{roomID}/sculptures/{slotIndex}", h.HandleRemoveSculpture)
	router.Handle("PUT /museum/me/rooms/{roomID}/music", h.HandleAssignRoomMusic)
	router.Handle("DELETE /museum/me/rooms/{roomID}/music", h.HandleRemoveRoomMusic)
}

func (h *Handlers) HandleAddSculpture(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req addSculptureRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	roomID := domain.RoomID(r.PathValue("roomID"))
	if _, err := h.museums.AddSculpture(r.Context(), accountID, roomID, req.CatalogID); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	room, err := h.museums.FindRoom(r.Context(), accountID, roomID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, sculptureListResponse{Sculptures: newRoomResponse(room).Sculptures})
}

func (h *Handlers) HandleRemoveSculpture(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	slotIndex, err := strconv.Atoi(r.PathValue("slotIndex"))
	if err != nil {
		writeCodedError(w, http.StatusBadRequest, "slot index must be an integer", "invalid_sculpture_slot", "")
		return
	}

	roomID := domain.RoomID(r.PathValue("roomID"))
	if err := h.museums.RemoveSculpture(r.Context(), accountID, roomID, slotIndex); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	room, err := h.museums.FindRoom(r.Context(), accountID, roomID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sculptureListResponse{Sculptures: newRoomResponse(room).Sculptures})
}

func (h *Handlers) HandleDeletePhoto(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	roomID := domain.RoomID(r.PathValue("roomID"))
	photoAssetID := r.PathValue("photoAssetID")
	if err := h.museums.DeletePhoto(r.Context(), accountID, roomID, photoAssetID); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	room, err := h.museums.FindRoom(r.Context(), accountID, roomID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, photoOrderResponse{PhotoSlots: newRoomResponse(room).PhotoSlots})
}

func (h *Handlers) HandleReplacePhoto(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req replacePhotoRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	roomID := domain.RoomID(r.PathValue("roomID"))
	photoAssetID := r.PathValue("photoAssetID")
	if err := h.museums.ReplacePhoto(r.Context(), accountID, roomID, photoAssetID, req.AssetID); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	room, err := h.museums.FindRoom(r.Context(), accountID, roomID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, photoOrderResponse{PhotoSlots: newRoomResponse(room).PhotoSlots})
}

func (h *Handlers) HandleSetPhotoCaption(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req setPhotoCaptionRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	roomID := domain.RoomID(r.PathValue("roomID"))
	photoAssetID := r.PathValue("photoAssetID")
	if err := h.museums.SetPhotoCaption(r.Context(), accountID, roomID, photoAssetID, req.Caption); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	room, err := h.museums.FindRoom(r.Context(), accountID, roomID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, photoOrderResponse{PhotoSlots: newRoomResponse(room).PhotoSlots})
}

func (h *Handlers) HandleReorderPhotos(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req reorderPhotosRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	roomID := domain.RoomID(r.PathValue("roomID"))
	if err := h.museums.ReorderPhotos(r.Context(), accountID, roomID, req.PhotoAssetIDs); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	room, err := h.museums.FindRoom(r.Context(), accountID, roomID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, photoOrderResponse{PhotoSlots: newRoomResponse(room).PhotoSlots})
}

func (h *Handlers) HandleAddPhotos(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req addPhotosRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	roomID := domain.RoomID(r.PathValue("roomID"))
	if _, err := h.museums.AddPhotos(r.Context(), accountID, roomID, req.AssetIDs); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	room, err := h.museums.FindRoom(r.Context(), accountID, roomID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, addPhotosResponse{PhotoSlots: newRoomResponse(room).PhotoSlots})
}

func (h *Handlers) HandlePhotoURLs(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	tickets, err := h.museums.PhotoDownloadTickets(r.Context(), accountID, domain.RoomID(r.PathValue("roomID")))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, newPhotoURLsResponse(tickets))
}

func (h *Handlers) HandleCreateMuseum(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req createMuseumRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	museum, err := h.museums.CreateMuseum(r.Context(), accountID, req.StyleID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newMuseumResponse(museum))
}

func (h *Handlers) HandleGetMuseum(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	museum, err := h.museums.FindMuseum(r.Context(), accountID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newMuseumResponse(museum))
}

func (h *Handlers) HandleChangeStyle(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req changeStyleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.museums.ChangeStyle(r.Context(), accountID, req.StyleID); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	museum, err := h.museums.FindMuseum(r.Context(), accountID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newMuseumResponse(museum))
}

func (h *Handlers) HandleChangePrivacy(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req changePrivacyRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.museums.ChangePrivacy(r.Context(), accountID, domain.Privacy(req.Privacy)); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	museum, err := h.museums.FindMuseum(r.Context(), accountID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newMuseumResponse(museum))
}

func (h *Handlers) HandleCreateRoom(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req createRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	room, err := h.museums.CreateRoom(r.Context(), accountID, req.Name, req.VariantID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newRoomResponse(room))
}

func (h *Handlers) HandleListRooms(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	rooms, err := h.museums.ListRooms(r.Context(), accountID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	responses := make([]roomResponse, 0, len(rooms))
	for _, room := range rooms {
		responses = append(responses, newRoomResponse(room))
	}
	writeJSON(w, http.StatusOK, roomListResponse{Rooms: responses})
}

func (h *Handlers) HandleGetRoom(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	room, err := h.museums.FindRoom(r.Context(), accountID, domain.RoomID(r.PathValue("roomID")))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newRoomResponse(room))
}

func (h *Handlers) HandleUpdateRoom(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	var req updateRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	roomID := domain.RoomID(r.PathValue("roomID"))
	var patch domain.RoomPatch
	patch.Name = req.Name
	patch.VariantID = req.VariantID
	if req.Privacy != nil {
		privacy := domain.Privacy(*req.Privacy)
		patch.Privacy = &privacy
	}
	if err := h.museums.UpdateRoom(r.Context(), accountID, roomID, patch); err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	room, err := h.museums.FindRoom(r.Context(), accountID, roomID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newRoomResponse(room))
}

func (h *Handlers) HandleDeleteRoom(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	if err := h.museums.DeleteRoom(r.Context(), accountID, domain.RoomID(r.PathValue("roomID"))); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (h *Handlers) HandleAssignRoomMusic(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var req assignRoomMusicRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	roomID := domain.RoomID(r.PathValue("roomID"))
	if err := h.museums.AssignRoomMusic(r.Context(), accountID, roomID, req.MusicTrackID); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	h.writeRoom(w, r, accountID, roomID)
}

func (h *Handlers) HandleRemoveRoomMusic(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	roomID := domain.RoomID(r.PathValue("roomID"))
	if err := h.museums.RemoveRoomMusic(r.Context(), accountID, roomID); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	h.writeRoom(w, r, accountID, roomID)
}

func (h *Handlers) writeRoom(w http.ResponseWriter, r *http.Request, accountID string, roomID domain.RoomID) {
	room, err := h.museums.FindRoom(r.Context(), accountID, roomID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newRoomResponse(room))
}

func (h *Handlers) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	return accountID, true
}

func (h *Handlers) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	h.logRefusal(r, err)
	switch {
	case errors.Is(err, domain.ErrMuseumNotFound),
		errors.Is(err, domain.ErrRoomNotFound),
		errors.Is(err, domain.ErrNotOwner),
		errors.Is(err, domain.ErrNotVisible):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrMuseumAlreadyExists):
		writeError(w, http.StatusConflict, "account already owns a museum")
	case errors.Is(err, domain.ErrUnknownStyle):
		writeError(w, http.StatusBadRequest, "unknown style_id")
	case errors.Is(err, domain.ErrUnknownVariant):
		writeError(w, http.StatusBadRequest, "unknown variant_id")
	case errors.Is(err, domain.ErrVariantStyleMismatch):
		writeError(w, http.StatusBadRequest, "variant does not belong to the museum's style")
	case errors.Is(err, domain.ErrInvalidPrivacy):
		writeError(w, http.StatusBadRequest, "invalid privacy value")
	case errors.Is(err, domain.ErrSculptureCapacityReached):
		writeCodedError(w, http.StatusConflict, "room already holds the maximum of 3 sculptures", "sculpture_capacity_reached", "")
	case errors.Is(err, domain.ErrPhotoCapacityReached):
		writeError(w, http.StatusConflict, "capacity reached")
	case errors.Is(err, domain.ErrInvalidSlotIndex), errors.Is(err, domain.ErrSlotOccupied):
		writeError(w, http.StatusBadRequest, "invalid slot")

	case errors.Is(err, domain.ErrUnknownMusicTrack):
		writeError(w, http.StatusBadRequest, "unknown music track")
	case errors.Is(err, domain.ErrPhotosUnavailable):
		writeError(w, http.StatusServiceUnavailable, "photo storage is not configured")
	case errors.Is(err, domain.ErrNoPhotosSupplied):
		writeCodedError(w, http.StatusBadRequest, "asset_ids is required", "no_photos", "")
	case errors.Is(err, domain.ErrDuplicatePhotoAssetIDs):
		writeCodedError(w, http.StatusBadRequest, "asset_ids contains duplicates", "duplicate_asset_ids", "")
	case errors.Is(err, domain.ErrPhotoAssetAlreadyAssigned):
		writeCodedError(w, http.StatusConflict, "photo is already assigned to a room", "asset_already_assigned", assetIDOf(err))
	case errors.Is(err, domain.ErrPhotoAssetNotFound):
		writeCodedError(w, http.StatusNotFound, "photo asset not found", "asset_not_found", assetIDOf(err))
	case errors.Is(err, domain.ErrPhotoAssetNotUploaded):
		writeCodedError(w, http.StatusConflict, "photo bytes have not been uploaded yet", "asset_not_uploaded", assetIDOf(err))
	case errors.Is(err, domain.ErrPhotoAssetInvalid):
		writeCodedError(w, http.StatusUnprocessableEntity, "uploaded photo failed verification", "asset_invalid", assetIDOf(err))
	case errors.Is(err, domain.ErrPhotoAssetDiscarded):
		writeCodedError(w, http.StatusGone, "photo upload was discarded; upload it again", "asset_discarded", assetIDOf(err))
	case errors.Is(err, domain.ErrSlotLayoutInconsistent):
		h.logger.Error("museum: room slot layout inconsistent", "error", err)
		writeError(w, http.StatusConflict, "room slot layout is inconsistent")

	case errors.Is(err, domain.ErrInvalidPhotoOrder):
		writeCodedError(w, http.StatusBadRequest, "photo_asset_ids must list each of the room's photographs exactly once", "invalid_order", "")
	case errors.Is(err, domain.ErrPhotoOrderMismatch):
		writeCodedError(w, http.StatusConflict, "photo order does not match the room's current photographs; reload and retry", "order_mismatch", "")
	case errors.Is(err, domain.ErrTransactionsUnavailable):
		writeError(w, http.StatusServiceUnavailable, "transactional storage is not configured")

	case errors.Is(err, domain.ErrCaptionTooLong):
		writeCodedError(w, http.StatusBadRequest, "caption is too long", "caption_too_long", "")
	case errors.Is(err, domain.ErrInvalidCaption):
		writeCodedError(w, http.StatusBadRequest, "caption is not valid text", "invalid_caption", "")
	case errors.Is(err, domain.ErrPhotoNotInRoom):
		writeCodedError(w, http.StatusNotFound, "photo is not in this room", "photo_not_in_room", "")

	case errors.Is(err, domain.ErrUnknownSculpture):
		writeCodedError(w, http.StatusBadRequest, "sculpture is not in the catalog", "unknown_sculpture", "")
	case errors.Is(err, domain.ErrSculptureNotInRoom):
		writeCodedError(w, http.StatusNotFound, "no sculpture in that slot", "sculpture_not_in_room", "")
	case errors.Is(err, domain.ErrInvalidSculptureSlot):
		writeCodedError(w, http.StatusBadRequest, "sculpture slot index is outside the permitted range", "invalid_sculpture_slot", "")

	case errors.Is(err, domain.ErrInvalidReplacement):
		writeCodedError(w, http.StatusBadRequest, "asset_id is required and must differ from the photograph being replaced", "invalid_replacement", "")
	default:
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:      observability.EventPersistenceFailed,
			Category:  observability.CategoryPersistence,
			Outcome:   observability.OutcomeFailed,
			AccountID: h.accountIDForLogging(r),
			Err:       err,
		})
		writeError(w, http.StatusInternalServerError, "request failed")
	}
}

func (h *Handlers) logRefusal(r *http.Request, err error) {
	var reason string
	switch {
	case errors.Is(err, domain.ErrMuseumNotFound):
		reason = observability.ReasonNoMuseumForCaller
	case errors.Is(err, domain.ErrRoomNotFound):
		reason = observability.ReasonNotFound
	case errors.Is(err, domain.ErrNotOwner):
		reason = observability.ReasonNotOwner
	case errors.Is(err, domain.ErrNotVisible):
		reason = observability.ReasonNotVisible
	default:
		return
	}
	observability.Log(r.Context(), h.logger, observability.Event{
		Name:      observability.EventAuthorizationRefused,
		Category:  observability.CategoryAuthz,
		Outcome:   observability.OutcomeRefused,
		Reason:    reason,
		AccountID: h.accountIDForLogging(r),
	})
}

func (h *Handlers) accountIDForLogging(r *http.Request) string {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		return ""
	}
	return accountID
}

func assetIDOf(err error) string {
	var assetErr *domain.PhotoAssetError
	if errors.As(err, &assetErr) {
		return assetErr.AssetID
	}
	return ""
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

func writeCodedError(w http.ResponseWriter, status int, message, code, assetID string) {
	writeJSON(w, status, errorResponse{Error: message, Code: code, AssetID: assetID})
}
