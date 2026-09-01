package interfaces

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	platformhttp "muse-backend/internal/platform/http"
	"muse-backend/internal/platform/observability"
	"muse-backend/internal/sharing/application"
	"muse-backend/internal/sharing/domain"
)

type CollectionHandlers struct {
	links  *application.CollectionShareLinkService
	auth   AccountAuthenticating
	cfg    Config
	logger *slog.Logger
}

func NewCollectionHandlers(links *application.CollectionShareLinkService, auth AccountAuthenticating, cfg Config, logger *slog.Logger) *CollectionHandlers {
	return &CollectionHandlers{links: links, auth: auth, cfg: cfg, logger: logger}
}

func (h *CollectionHandlers) RegisterRoutes(router *platformhttp.Router) {
	router.Handle("POST /collection-rooms/{collectionRoomID}/share-link", h.HandleEnsureLink)
	router.Handle("GET /collection-rooms/{collectionRoomID}/share-link", h.HandleCurrentLink)
	router.Handle("POST /collection-rooms/{collectionRoomID}/share-link/regenerate", h.HandleRegenerateLink)
	router.Handle("DELETE /collection-rooms/{collectionRoomID}/share-link", h.HandleRevokeLink)

	router.Handle("GET /collection-share-links/{code}/collection-room", h.HandleVisitorCollectionRoom)

	router.Handle("GET /c/{code}", h.HandleLanding)
}

func (h *CollectionHandlers) ShareURL(code domain.Code) string {
	return strings.TrimRight(h.cfg.ShareLinkBaseURL, "/") + "/c/" + string(code)
}

// MARK: - Owner

func (h *CollectionHandlers) HandleEnsureLink(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	link, err := h.links.EnsureLink(r.Context(), accountID, r.PathValue("collectionRoomID"))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.newLinkResponse(link))
}

func (h *CollectionHandlers) HandleCurrentLink(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	link, err := h.links.CurrentLink(r.Context(), accountID, r.PathValue("collectionRoomID"))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.newLinkResponse(link))
}

func (h *CollectionHandlers) HandleRegenerateLink(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	link, err := h.links.RegenerateLink(r.Context(), accountID, r.PathValue("collectionRoomID"))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.newLinkResponse(link))
}

func (h *CollectionHandlers) HandleRevokeLink(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	if _, err := h.links.RevokeLink(r.Context(), accountID, r.PathValue("collectionRoomID")); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MARK: - Visitor

func (h *CollectionHandlers) HandleVisitorCollectionRoom(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	room, err := h.links.VisitorCollectionRoom(r.Context(), domain.Code(r.PathValue("code")))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newVisitorCollectionRoomResponse(room))
}

// MARK: - Landing

func (h *CollectionHandlers) HandleLanding(w http.ResponseWriter, r *http.Request) {
	writeHTML(w, http.StatusOK, collectionLandingPage, landingData{
		ShareURL:    h.ShareURL(domain.Code(r.PathValue("code"))),
		AppStoreURL: h.cfg.AppStoreURL,
	})
}

// MARK: - Plumbing

func (h *CollectionHandlers) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	return accountID, true
}

func (h *CollectionHandlers) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrLinkNotAvailable),
		errors.Is(err, domain.ErrNoCollectionRoom),
		errors.Is(err, domain.ErrNoActiveCollectionLink):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:     observability.EventShareLinkUnavailable,
			Category: observability.CategorySharing,
			Outcome:  observability.OutcomeRefused,
			Reason:   observability.ReasonNoActiveLink,
		})
		writeError(w, http.StatusNotFound, "not found")
	default:
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:     observability.EventShareLinkFailed,
			Category: observability.CategorySharing,
			Outcome:  observability.OutcomeFailed,
			Err:      err,
		})
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// MARK: - Wire shapes

type collectionLinkResponse struct {
	CollectionRoomID string    `json:"collection_room_id"`
	Code             string    `json:"code"`
	URL              string    `json:"url"`
	CreatedAt        time.Time `json:"created_at"`
}

func (h *CollectionHandlers) newLinkResponse(link domain.CollectionShareLink) collectionLinkResponse {
	return collectionLinkResponse{
		CollectionRoomID: link.CollectionRoomID,
		Code:             string(link.Code),
		URL:              h.ShareURL(link.Code),
		CreatedAt:        link.CreatedAt,
	}
}

type visitorCollectionRoomResponse struct {
	CollectionRoomID string                          `json:"collection_room_id"`
	Name             string                          `json:"name"`
	CategoryID       string                          `json:"category_id"`
	DesignID         string                          `json:"design_id"`
	CurrentTier      int                             `json:"current_tier"`
	MusicTrackID     string                          `json:"music_track_id,omitempty"`
	Items            []visitorCollectionItemResponse `json:"items"`
}

type visitorCollectionItemResponse struct {
	ID             string `json:"id"`
	SlotIndex      int    `json:"slot_index"`
	CatalogModelID string `json:"catalog_model_id"`
}

func newVisitorCollectionRoomResponse(room application.CollectionRoomContent) visitorCollectionRoomResponse {
	items := make([]visitorCollectionItemResponse, 0, len(room.Items))
	for _, item := range room.Items {
		items = append(items, visitorCollectionItemResponse{
			ID: item.ID, SlotIndex: item.SlotIndex, CatalogModelID: item.CatalogModelID,
		})
	}
	return visitorCollectionRoomResponse{
		CollectionRoomID: room.ID,
		Name:             room.Name,
		CategoryID:       room.CategoryID,
		DesignID:         room.DesignID,
		CurrentTier:      room.CurrentTier,
		MusicTrackID:     room.MusicTrackID,
		Items:            items,
	}
}

var collectionLandingPage = template.Must(template.New("collection-landing").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Muse</title>
<style>body{font-family:-apple-system,system-ui,sans-serif;margin:0;padding:48px 24px;text-align:center;color:#111}
h1{font-size:22px;font-weight:600}p{color:#555;line-height:1.5}
a.button{display:inline-block;margin-top:16px;padding:12px 20px;border-radius:10px;background:#111;color:#fff;text-decoration:none}
code{display:block;margin-top:24px;word-break:break-all;color:#777;font-size:13px}</style></head>
<body>
<h1>You've been invited to a Collection Room on Muse.</h1>
<p>Muse is an iPhone app. Open this link on your iPhone with Muse installed and sign in to visit.</p>
{{if .AppStoreURL}}<a class="button" href="{{.AppStoreURL}}">Get Muse on the App Store</a>{{else}}<p>Muse is not on the App Store yet.</p>{{end}}
<p>Already have Muse? Open this link from Messages or Mail, or paste it into the app.</p>
<code>{{.ShareURL}}</code>
</body></html>
`))
