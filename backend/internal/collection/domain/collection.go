package domain

import "time"

type CollectionRoomID string

type CollectionItemID string

type CollectionRoom struct {
	ID        CollectionRoomID
	AccountID string

	Name string

	CategoryID string

	DesignID string

	CurrentTier Tier

	MusicTrackID string

	Items []CollectionItem

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (r CollectionRoom) ItemCount() int { return len(r.Items) }

type CollectionItem struct {
	ID        CollectionItemID
	SlotIndex int

	CatalogModelID string

	CreatedAt time.Time
	UpdatedAt time.Time
}

type CollectionRoomPatch struct {
	Name       *string
	CategoryID *string
	DesignID   *string
}

func (p CollectionRoomPatch) IsEmpty() bool {
	return p.Name == nil && p.CategoryID == nil && p.DesignID == nil
}
