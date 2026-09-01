package interfaces

import (
	"net/http"

	"muse-backend/internal/museum/domain"
)

func (h *Handlers) HandleGetVisibleMuseum(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	museum, rooms, err := h.museums.VisibleMuseum(r.Context(), accountID, domain.MuseumID(r.PathValue("museumID")))
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newVisitorMuseumResponse(museum, rooms))
}

func (h *Handlers) HandleGetVisibleRoom(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	room, err := h.museums.VisibleRoom(
		r.Context(),
		accountID,
		domain.MuseumID(r.PathValue("museumID")),
		domain.RoomID(r.PathValue("roomID")),
	)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newVisitorRoomResponse(room))
}
