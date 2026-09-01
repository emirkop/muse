package interfaces

import (
	"time"

	"muse-backend/internal/sharing/application"
	"muse-backend/internal/sharing/domain"
)

type linkResponse struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *Handlers) newLinkResponse(link domain.ShareLink) linkResponse {
	return linkResponse{Code: string(link.Code), URL: h.ShareURL(link.Code), CreatedAt: link.CreatedAt}
}

type previewResponse struct {
	Code    string        `json:"code"`
	StyleID string        `json:"style_id"`
	Owner   ownerResponse `json:"owner"`
}

type ownerResponse struct {
	AvatarID string `json:"avatar_id"`
}

func newPreviewResponse(p application.Preview) previewResponse {
	return previewResponse{
		Code:    string(p.Code),
		StyleID: p.StyleID,
		Owner:   ownerResponse{AvatarID: p.Owner.AvatarID},
	}
}

type visitorMuseumResponse struct {
	MuseumID string               `json:"museum_id"`
	StyleID  string               `json:"style_id"`
	Rooms    []visitorRoomSummary `json:"rooms"`
}

type visitorRoomSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	VariantID string `json:"variant_id"`
}

type visitorRoomResponse struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	VariantID    string              `json:"variant_id"`
	MusicTrackID string              `json:"music_track_id,omitempty"`
	PhotoSlots   []photoSlotResponse `json:"photo_slots"`
	Sculptures   []sculptureResponse `json:"sculptures"`
}

type photoSlotResponse struct {
	SlotIndex    int    `json:"slot_index"`
	PhotoAssetID string `json:"photo_asset_id"`
	Caption      string `json:"caption"`
}

type sculptureResponse struct {
	SlotIndex int    `json:"slot_index"`
	CatalogID string `json:"catalog_id"`
}

func newVisitorMuseumResponse(c application.MuseumContent) visitorMuseumResponse {
	rooms := make([]visitorRoomSummary, 0, len(c.Rooms))
	for _, r := range c.Rooms {
		rooms = append(rooms, visitorRoomSummary{ID: r.ID, Name: r.Name, VariantID: r.VariantID})
	}
	return visitorMuseumResponse{MuseumID: c.ID, StyleID: c.StyleID, Rooms: rooms}
}

func newVisitorRoomResponse(r application.RoomContent) visitorRoomResponse {
	slots := make([]photoSlotResponse, 0, len(r.PhotoSlots))
	for _, s := range r.PhotoSlots {
		slots = append(slots, photoSlotResponse{SlotIndex: s.SlotIndex, PhotoAssetID: s.PhotoAssetID, Caption: s.Caption})
	}
	sculptures := make([]sculptureResponse, 0, len(r.Sculptures))
	for _, s := range r.Sculptures {
		sculptures = append(sculptures, sculptureResponse{SlotIndex: s.SlotIndex, CatalogID: s.CatalogID})
	}
	return visitorRoomResponse{
		ID: r.ID, Name: r.Name, VariantID: r.VariantID,
		MusicTrackID: r.MusicTrackID,
		PhotoSlots:   slots, Sculptures: sculptures,
	}
}

type photoTicketResponse struct {
	PhotoAssetID string    `json:"photo_asset_id"`
	URL          string    `json:"url"`
	ExpiresAt    time.Time `json:"expires_at"`
	PixelWidth   int       `json:"pixel_width"`
	PixelHeight  int       `json:"pixel_height"`
}

type photoTicketsResponse struct {
	Tickets []photoTicketResponse `json:"tickets"`
}

func newPhotoTicketsResponse(tickets []application.PhotoTicket) photoTicketsResponse {
	out := make([]photoTicketResponse, 0, len(tickets))
	for _, t := range tickets {
		out = append(out, photoTicketResponse{
			PhotoAssetID: t.PhotoAssetID,
			URL:          t.URL,
			ExpiresAt:    t.ExpiresAt,
			PixelWidth:   t.PixelWidth,
			PixelHeight:  t.PixelHeight,
		})
	}
	return photoTicketsResponse{Tickets: out}
}

type errorResponse struct {
	Error string `json:"error"`
}
