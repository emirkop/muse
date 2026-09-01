package interfaces

import (
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	platformhttp "muse-backend/internal/platform/http"
	"muse-backend/internal/platform/observability"
	"muse-backend/internal/sharing/application"
	"muse-backend/internal/sharing/domain"
)

type AccountAuthenticating interface {
	AuthenticatedAccountID(r *http.Request) (string, error)
}

type Config struct {
	ShareLinkBaseURL string
	AppStoreURL      string
	AppleAppID       string
}

type Handlers struct {
	links  *application.ShareLinkService
	auth   AccountAuthenticating
	cfg    Config
	logger *slog.Logger
}

func NewHandlers(links *application.ShareLinkService, auth AccountAuthenticating, cfg Config, logger *slog.Logger) *Handlers {
	return &Handlers{links: links, auth: auth, cfg: cfg, logger: logger}
}

func (h *Handlers) RegisterRoutes(router *platformhttp.Router) {
	router.Handle("POST /museum/me/share-link", h.HandleEnsureLink)
	router.Handle("GET /museum/me/share-link", h.HandleCurrentLink)
	router.Handle("POST /museum/me/share-link/regenerate", h.HandleRegenerateLink)

	router.Handle("GET /share-links/{code}", h.HandlePreview)
	router.Handle("GET /share-links/{code}/museum", h.HandleVisitorMuseum)
	router.Handle("GET /share-links/{code}/rooms/{roomID}", h.HandleVisitorRoom)
	router.Handle("GET /share-links/{code}/rooms/{roomID}/photo-urls", h.HandleVisitorRoomPhotoURLs)

	router.Handle("GET /m/{code}", h.HandleLanding)

	if h.cfg.AppleAppID != "" {
		router.Handle("GET /.well-known/apple-app-site-association", h.HandleAppleAppSiteAssociation)
	}
}

func (h *Handlers) ShareURL(code domain.Code) string {
	return strings.TrimRight(h.cfg.ShareLinkBaseURL, "/") + "/m/" + string(code)
}

// MARK: - Owner

func (h *Handlers) HandleEnsureLink(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	link, err := h.links.EnsureLink(r.Context(), accountID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.newLinkResponse(link))
}

func (h *Handlers) HandleCurrentLink(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	link, err := h.links.CurrentLink(r.Context(), accountID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.newLinkResponse(link))
}

func (h *Handlers) HandleRegenerateLink(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	link, err := h.links.RegenerateLink(r.Context(), accountID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.newLinkResponse(link))
}

// MARK: - Visitor

func (h *Handlers) HandlePreview(w http.ResponseWriter, r *http.Request) {
	preview, err := h.links.Preview(r.Context(), domain.Code(r.PathValue("code")))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newPreviewResponse(preview))
}

func (h *Handlers) HandleVisitorMuseum(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	content, err := h.links.VisitorMuseum(r.Context(), domain.Code(r.PathValue("code")))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newVisitorMuseumResponse(content))
}

func (h *Handlers) HandleVisitorRoom(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	room, err := h.links.VisitorRoom(r.Context(), domain.Code(r.PathValue("code")), r.PathValue("roomID"))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newVisitorRoomResponse(room))
}

func (h *Handlers) HandleVisitorRoomPhotoURLs(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	tickets, err := h.links.VisitorRoomPhotoTickets(
		r.Context(),
		domain.Code(r.PathValue("code")),
		r.PathValue("roomID"),
	)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, newPhotoTicketsResponse(tickets))
}

// MARK: - Landing page & Universal Link association

func (h *Handlers) HandleLanding(w http.ResponseWriter, r *http.Request) {
	code := domain.Code(r.PathValue("code"))
	_, err := h.links.Preview(r.Context(), code)
	if err != nil {
		if errors.Is(err, domain.ErrLinkNotAvailable) {
			writeHTML(w, http.StatusNotFound, unavailablePage, nil)
			return
		}
		h.logger.Error("share link landing failed", "error", err)
		writeHTML(w, http.StatusInternalServerError, unavailablePage, nil)
		return
	}
	writeHTML(w, http.StatusOK, availablePage, landingData{
		ShareURL:    h.ShareURL(code),
		AppStoreURL: h.cfg.AppStoreURL,
	})
}

func (h *Handlers) HandleAppleAppSiteAssociation(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{
		"applinks": map[string]any{
			"details": []map[string]any{{
				"appIDs": []string{h.cfg.AppleAppID},
				"components": []map[string]any{
					{"/": "/m/*", "comment": "Muse Museum share links"},
					{"/": "/c/*", "comment": "Muse Collection Room share links"},
				},
			}},
		},
	}
	writeJSON(w, http.StatusOK, body)
}

// MARK: - Plumbing

func (h *Handlers) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	accountID, err := h.auth.AuthenticatedAccountID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	return accountID, true
}

func (h *Handlers) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrLinkNotAvailable),
		errors.Is(err, domain.ErrNoMuseum),
		errors.Is(err, domain.ErrNoActiveLink):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:     observability.EventShareLinkUnavailable,
			Category: observability.CategorySharing,
			Outcome:  observability.OutcomeRefused,
			Reason:   observability.ReasonNoActiveLink,
		})
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, application.ErrPhotosUnavailable):
		observability.Log(r.Context(), h.logger, observability.Event{
			Name:     observability.EventDependencyUnavailable,
			Category: observability.CategoryConfig,
			Outcome:  observability.OutcomeUnavailable,
			Reason:   observability.ReasonNotConfigured,
		})
		writeError(w, http.StatusServiceUnavailable, "photo storage is not configured")
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

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeHTML(w http.ResponseWriter, status int, page *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(status)
	_ = page.Execute(w, data)
}

type landingData struct {
	ShareURL    string
	AppStoreURL string
}

var availablePage = template.Must(template.New("available").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Muse</title>
<style>body{font-family:-apple-system,system-ui,sans-serif;margin:0;padding:48px 24px;text-align:center;color:#111}
h1{font-size:22px;font-weight:600}p{color:#555;line-height:1.5}
a.button{display:inline-block;margin-top:16px;padding:12px 20px;border-radius:10px;background:#111;color:#fff;text-decoration:none}
code{display:block;margin-top:24px;word-break:break-all;color:#777;font-size:13px}</style></head>
<body>
<h1>You've been invited to a Museum on Muse.</h1>
<p>Muse is an iPhone app. Open this link on your iPhone with Muse installed to visit.</p>
{{if .AppStoreURL}}<a class="button" href="{{.AppStoreURL}}">Get Muse on the App Store</a>{{else}}<p>Muse is not on the App Store yet.</p>{{end}}
<p>Already have Muse? Open this link from Messages or Mail, or paste it into the app.</p>
<code>{{.ShareURL}}</code>
</body></html>
`))

var unavailablePage = template.Must(template.New("unavailable").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Muse</title>
<style>body{font-family:-apple-system,system-ui,sans-serif;margin:0;padding:48px 24px;text-align:center;color:#111}
h1{font-size:22px;font-weight:600}p{color:#555;line-height:1.5}</style></head>
<body>
<h1>This link is no longer available.</h1>
<p>The Museum it pointed to can't be reached with this link.</p>
</body></html>
`))
