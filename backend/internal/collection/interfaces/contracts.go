package interfaces

import (
	"time"

	"muse-backend/internal/collection/domain"
)

type createCollectionRoomRequest struct {
	Name       string `json:"name"`
	CategoryID string `json:"category_id"`
	DesignID   string `json:"design_id,omitempty"`
}

type updateCollectionRoomRequest struct {
	Name       *string `json:"name,omitempty"`
	CategoryID *string `json:"category_id,omitempty"`
	DesignID   *string `json:"design_id,omitempty"`
}

type ratchetTierRequest struct {
	Tier int `json:"tier"`
}

type assignMusicRequest struct {
	MusicTrackID string `json:"music_track_id"`
}

type createCollectionItemRequest struct {
	CatalogModelID string `json:"catalog_model_id"`
}

type placeCollectionItemRequest struct {
	SlotIndex int `json:"slot_index"`
}

type collectionRoomResponse struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	CategoryID   string                   `json:"category_id"`
	DesignID     string                   `json:"design_id"`
	CurrentTier  int                      `json:"current_tier"`
	MusicTrackID string                   `json:"music_track_id,omitempty"`
	Items        []collectionItemResponse `json:"items"`
	CreatedAt    time.Time                `json:"created_at"`
	UpdatedAt    time.Time                `json:"updated_at"`
}

type collectionItemResponse struct {
	ID             string `json:"id"`
	SlotIndex      int    `json:"slot_index"`
	CatalogModelID string `json:"catalog_model_id"`
}

type collectionRoomListResponse struct {
	CollectionRooms []collectionRoomResponse `json:"collection_rooms"`
}

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func newCollectionRoomResponse(room domain.CollectionRoom) collectionRoomResponse {
	items := make([]collectionItemResponse, 0, len(room.Items))
	for _, item := range room.Items {
		items = append(items, collectionItemResponse{
			ID:             string(item.ID),
			SlotIndex:      item.SlotIndex,
			CatalogModelID: item.CatalogModelID,
		})
	}
	return collectionRoomResponse{
		ID:           string(room.ID),
		Name:         room.Name,
		CategoryID:   room.CategoryID,
		DesignID:     room.DesignID,
		CurrentTier:  int(room.CurrentTier),
		MusicTrackID: room.MusicTrackID,
		Items:        items,
		CreatedAt:    room.CreatedAt,
		UpdatedAt:    room.UpdatedAt,
	}
}
