package interfaces

import platformhttp "muse-backend/internal/platform/http"

func (h *Handlers) RegisterRoutes(router *platformhttp.Router) {
	router.Handle("POST /analytics/events", h.HandleRecordEvents)
}
