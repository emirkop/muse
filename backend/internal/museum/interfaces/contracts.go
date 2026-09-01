package interfaces

import (
	"time"

	"muse-backend/internal/museum/application"
	"muse-backend/internal/museum/domain"
)

type createMuseumRequest struct {
	StyleID string `json:"style_id"`
}

type changeStyleRequest struct {
	StyleID string `json:"style_id"`
}

type changePrivacyRequest struct {
	Privacy string `json:"privacy"`
}

type createRoomRequest struct {
	Name      string `json:"name"`
	VariantID string `json:"variant_id"`
}

type updateRoomRequest struct {
	Name      *string `json:"name,omitempty"`
	VariantID *string `json:"variant_id,omitempty"`
	Privacy   *string `json:"privacy,omitempty"`
}

type visitorMuseumResponse struct {
	ID      string               `json:"id"`
	StyleID string               `json:"style_id"`
	Rooms   []visitorRoomSummary `json:"rooms"`
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

func newVisitorMuseumResponse(museum domain.Museum, rooms []domain.Room) visitorMuseumResponse {
	summaries := make([]visitorRoomSummary, 0, len(rooms))
	for _, room := range rooms {
		summaries = append(summaries, visitorRoomSummary{
			ID:        string(room.ID),
			Name:      room.Name,
			VariantID: room.VariantID,
		})
	}
	return visitorMuseumResponse{ID: string(museum.ID), StyleID: museum.StyleID, Rooms: summaries}
}

func newVisitorRoomResponse(room domain.Room) visitorRoomResponse {
	full := newRoomResponse(room)
	return visitorRoomResponse{
		ID:           full.ID,
		Name:         full.Name,
		VariantID:    full.VariantID,
		MusicTrackID: full.MusicTrackID,
		PhotoSlots:   full.PhotoSlots,
		Sculptures:   full.Sculptures,
	}
}

type assignRoomMusicRequest struct {
	MusicTrackID string `json:"music_track_id"`
}

type museumResponse struct {
	ID      string `json:"id"`
	StyleID string `json:"style_id"`
	Privacy string `json:"privacy"`
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

type roomResponse struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	VariantID    string              `json:"variant_id"`
	Privacy      string              `json:"privacy"`
	MusicTrackID string              `json:"music_track_id,omitempty"`
	PhotoSlots   []photoSlotResponse `json:"photo_slots"`
	Sculptures   []sculptureResponse `json:"sculptures"`
}

type roomListResponse struct {
	Rooms []roomResponse `json:"rooms"`
}

func newMuseumResponse(museum domain.Museum) museumResponse {
	return museumResponse{
		ID:      string(museum.ID),
		StyleID: museum.StyleID,
		Privacy: string(museum.Privacy),
	}
}

func newRoomResponse(room domain.Room) roomResponse {
	slots := make([]photoSlotResponse, 0, len(room.PhotoSlots))
	for _, slot := range room.PhotoSlots {
		slots = append(slots, photoSlotResponse{
			SlotIndex:    slot.SlotIndex,
			PhotoAssetID: slot.PhotoAssetID,
			Caption:      slot.Caption,
		})
	}

	sculptures := make([]sculptureResponse, 0, len(room.Sculptures))
	for _, sculpture := range room.Sculptures {
		sculptures = append(sculptures, sculptureResponse{
			SlotIndex: sculpture.SlotIndex,
			CatalogID: sculpture.CatalogID,
		})
	}
	return roomResponse{
		ID:           string(room.ID),
		Name:         room.Name,
		VariantID:    room.VariantID,
		Privacy:      string(room.Privacy),
		MusicTrackID: room.MusicTrackID,
		PhotoSlots:   slots,
		Sculptures:   sculptures,
	}
}

type addPhotosRequest struct {
	AssetIDs []string `json:"asset_ids"`
}

type addPhotosResponse struct {
	PhotoSlots []photoSlotResponse `json:"photo_slots"`
}

type reorderPhotosRequest struct {
	PhotoAssetIDs []string `json:"photo_asset_ids"`
}

type setPhotoCaptionRequest struct {
	Caption string `json:"caption"`
}

type replacePhotoRequest struct {
	AssetID string `json:"asset_id"`
}

type addSculptureRequest struct {
	CatalogID string `json:"catalog_id"`
}

type sculptureListResponse struct {
	Sculptures []sculptureResponse `json:"sculptures"`
}

type photoOrderResponse struct {
	PhotoSlots []photoSlotResponse `json:"photo_slots"`
}

type photoURLResponse struct {
	PhotoAssetID string    `json:"photo_asset_id"`
	URL          string    `json:"url"`
	ExpiresAt    time.Time `json:"expires_at"`
	PixelWidth   int       `json:"pixel_width"`
	PixelHeight  int       `json:"pixel_height"`
}

type photoURLsResponse struct {
	Tickets []photoURLResponse `json:"tickets"`
}

func newPhotoURLsResponse(tickets []application.PhotoDownloadTicket) photoURLsResponse {
	out := make([]photoURLResponse, 0, len(tickets))
	for _, ticket := range tickets {
		out = append(out, photoURLResponse{
			PhotoAssetID: ticket.PhotoAssetID,
			URL:          ticket.URL,
			ExpiresAt:    ticket.ExpiresAt,
			PixelWidth:   ticket.PixelWidth,
			PixelHeight:  ticket.PixelHeight,
		})
	}
	return photoURLsResponse{Tickets: out}
}

type errorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	AssetID string `json:"asset_id,omitempty"`
}
