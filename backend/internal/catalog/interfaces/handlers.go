package interfaces

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"muse-backend/internal/catalog/application"
	"muse-backend/internal/catalog/domain"
	platformhttp "muse-backend/internal/platform/http"
)

type CatalogReading interface {
	ListStyles(ctx context.Context) ([]domain.MuseumStyle, error)
	ListVariants(ctx context.Context, styleID domain.StyleID) ([]domain.RoomVariant, error)
	ListSculptures(ctx context.Context) ([]domain.Sculpture, error)
	ListMusicTracks(ctx context.Context) ([]domain.MusicTrack, error)
	FindVariant(ctx context.Context, variantID domain.VariantID) (domain.RoomVariant, bool, error)
	ListCollectionCategories(ctx context.Context) ([]domain.CollectionCategory, error)
}

type Handlers struct {
	catalog           CatalogReading
	auth              AccountAuthenticating
	music             *application.MusicDeliveryService
	bundles           *application.BundleService
	collectionDesigns *application.CollectionDesignService
	collectionCatalog *application.CollectionCatalogService
	searchAnalytics   application.SearchRecording
	logger            *slog.Logger
}

type AccountAuthenticating interface {
	AuthenticatedAccountID(r *http.Request) (string, error)
}

func NewHandlers(catalog CatalogReading, auth AccountAuthenticating, logger *slog.Logger) *Handlers {
	return &Handlers{catalog: catalog, auth: auth, logger: logger}
}

func (h *Handlers) WithSearchAnalytics(recorder application.SearchRecording) *Handlers {
	h.searchAnalytics = recorder
	return h
}

func (h *Handlers) WithMusicDelivery(music *application.MusicDeliveryService) *Handlers {
	h.music = music
	return h
}

func (h *Handlers) WithBundleDelivery(bundles *application.BundleService) *Handlers {
	h.bundles = bundles
	return h
}

func (h *Handlers) WithCollectionDesigns(designs *application.CollectionDesignService) *Handlers {
	h.collectionDesigns = designs
	return h
}

func (h *Handlers) WithCollectionCatalog(catalog *application.CollectionCatalogService) *Handlers {
	h.collectionCatalog = catalog
	return h
}

func (h *Handlers) RegisterRoutes(router *platformhttp.Router) {
	router.Handle("GET /catalog/styles", h.HandleListStyles)
	router.Handle("GET /catalog/styles/{styleID}/variants", h.HandleListVariants)
	router.Handle("GET /catalog/sculptures", h.HandleListSculptures)
	router.Handle("GET /catalog/music", h.HandleListMusicTracks)
	router.Handle("GET /catalog/music/{trackID}/audio-url", h.HandleMusicAudioURL)
	router.Handle("GET /catalog/bundles/{bundleID}/manifest", h.HandleBundleManifest)
	router.Handle("GET /catalog/room-variants/{variantID}", h.HandleGetRoomVariant)
	router.Handle("GET /catalog/collection-categories", h.HandleListCollectionCategories)
	router.Handle("GET /catalog/collection-designs", h.HandleListCollectionDesigns)
	router.Handle("GET /catalog/collection-models", h.HandleSearchCollectionModels)
	router.Handle("GET /catalog/collection-presentation-assets", h.HandleCollectionPresentationAssets)
}

func (h *Handlers) HandleSearchCollectionModels(w http.ResponseWriter, r *http.Request) {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if h.collectionCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "collection catalog is not configured")
		return
	}

	query := r.URL.Query()
	limit, parseErr := strconv.Atoi(query.Get("limit"))
	if parseErr != nil {
		limit = 0
	}

	var cursor *domain.ModelSearchCursor
	if name, id := query.Get("cursor_name"), query.Get("cursor_id"); name != "" || id != "" {
		if name == "" || id == "" {
			writeError(w, http.StatusBadRequest, "cursor_name and cursor_id must be supplied together")
			return
		}
		cursor = &domain.ModelSearchCursor{DisplayName: name, ID: domain.CollectionModelID(id)}
	}

	page, err := h.collectionCatalog.SearchModels(
		r.Context(), query.Get("category_id"), query.Get("q"), limit, cursor,
	)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrSearchCategoryRequired):
		writeError(w, http.StatusBadRequest, "category_id is required")
		return
	case errors.Is(err, domain.ErrSearchUnknownCategory):
		writeError(w, http.StatusBadRequest, "unknown category_id")
		return
	default:
		h.logger.Error("catalog model search failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}
	if h.searchAnalytics != nil {
		h.searchAnalytics.RecordCatalogSearch(r.Context(), accountID, query.Get("category_id"), len(page.Models))
	}
	writeJSON(w, http.StatusOK, newModelSearchResponse(page))
}

func (h *Handlers) HandleCollectionPresentationAssets(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(w, r) {
		return
	}
	if h.collectionCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "collection catalog is not configured")
		return
	}

	raw := r.URL.Query().Get("model_ids")
	var modelIDs []string
	for _, id := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			modelIDs = append(modelIDs, trimmed)
		}
	}

	mappings, err := h.collectionCatalog.PresentationAssetMappings(r.Context(), modelIDs)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrPresentationAssetIDsRequired):
		writeError(w, http.StatusBadRequest, "model_ids is required")
		return
	case errors.Is(err, domain.ErrPresentationAssetTooManyIDs):
		writeError(w, http.StatusBadRequest, "too many model_ids in one request")
		return
	default:
		h.logger.Error("catalog presentation asset lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}
	writeJSON(w, http.StatusOK, newPresentationAssetsResponse(mappings))
}

func (h *Handlers) HandleListCollectionDesigns(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(w, r) {
		return
	}
	if h.collectionDesigns == nil {
		writeError(w, http.StatusServiceUnavailable, "collection design registry is not configured")
		return
	}

	designs, err := h.collectionDesigns.ApplicableDesigns(r.Context(), r.URL.Query().Get("category_id"))
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrDesignCategoryRequired):
		writeError(w, http.StatusBadRequest, "category_id is required")
		return
	case errors.Is(err, domain.ErrDesignUnknownCategory):
		writeError(w, http.StatusBadRequest, "unknown category_id")
		return
	default:
		h.logger.Error("catalog list collection designs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}

	responses := make([]collectionDesignResponse, 0, len(designs))
	for _, design := range designs {
		responses = append(responses, newCollectionDesignResponse(design))
	}
	writeJSON(w, http.StatusOK, collectionDesignListResponse{CollectionDesigns: responses})
}

func (h *Handlers) HandleListCollectionCategories(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(w, r) {
		return
	}

	categories, err := h.catalog.ListCollectionCategories(r.Context())
	if err != nil {
		h.logger.Error("catalog list collection categories failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}

	responses := make([]collectionCategoryResponse, 0, len(categories))
	for _, category := range categories {
		responses = append(responses, newCollectionCategoryResponse(category))
	}
	writeJSON(w, http.StatusOK, collectionCategoryListResponse{CollectionCategories: responses})
}

func (h *Handlers) HandleListSculptures(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(w, r) {
		return
	}

	sculptures, err := h.catalog.ListSculptures(r.Context())
	if err != nil {
		h.logger.Error("catalog list sculptures failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}

	responses := make([]sculptureCatalogResponse, 0, len(sculptures))
	for _, sculpture := range sculptures {
		responses = append(responses, newSculptureCatalogResponse(sculpture))
	}
	writeJSON(w, http.StatusOK, sculptureCatalogListResponse{Sculptures: responses})
}

func (h *Handlers) HandleListStyles(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(w, r) {
		return
	}

	styles, err := h.catalog.ListStyles(r.Context())
	if err != nil {
		h.logger.Error("catalog list styles failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}

	responses := make([]styleResponse, 0, len(styles))
	for _, style := range styles {
		responses = append(responses, newStyleResponse(style))
	}
	writeJSON(w, http.StatusOK, styleListResponse{Styles: responses})
}

func (h *Handlers) HandleListVariants(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(w, r) {
		return
	}

	variants, err := h.catalog.ListVariants(r.Context(), domain.StyleID(r.PathValue("styleID")))
	if err != nil {
		h.logger.Error("catalog list variants failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}

	responses := make([]variantResponse, 0, len(variants))
	for _, variant := range variants {
		responses = append(responses, newVariantResponse(variant))
	}
	writeJSON(w, http.StatusOK, variantListResponse{Variants: responses})
}

func (h *Handlers) HandleListMusicTracks(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(w, r) {
		return
	}

	tracks, err := h.catalog.ListMusicTracks(r.Context())
	if err != nil {
		h.logger.Error("catalog list music tracks failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}

	responses := make([]musicTrackResponse, 0, len(tracks))
	for _, track := range tracks {
		responses = append(responses, newMusicTrackResponse(track))
	}
	writeJSON(w, http.StatusOK, musicTrackListResponse{Tracks: responses})
}

func (h *Handlers) HandleMusicAudioURL(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(w, r) {
		return
	}
	if h.music == nil {
		writeError(w, http.StatusServiceUnavailable, "audio delivery is not configured")
		return
	}

	audio, err := h.music.AudioURL(r.Context(), r.PathValue("trackID"))
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrMusicTrackNotFound):
		writeError(w, http.StatusNotFound, "music track not found")
		return
	case errors.Is(err, domain.ErrMusicTrackNotCleared):
		h.logger.Warn("music track refused: not cleared for this deployment", "error", err)
		writeError(w, http.StatusForbidden, "music track is not available")
		return
	default:
		h.logger.Error("music audio url failed", "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
		return
	}

	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, newAudioURLResponse(audio))
}

func (h *Handlers) authenticated(w http.ResponseWriter, r *http.Request) bool {
	if _, err := h.auth.AuthenticatedAccountID(r); err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
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
