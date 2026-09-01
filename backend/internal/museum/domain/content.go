package domain

import "time"

type MuseumID string

type RoomID string

type Privacy string

const (
	PrivacyPublic  Privacy = "public"
	PrivacyPrivate Privacy = "private"
)

func IsValidPrivacy(p Privacy) bool {
	return p == PrivacyPublic || p == PrivacyPrivate
}

const (
	MaxPhotosPerRoom     = 28
	MaxSculpturesPerRoom = 3
)

type Museum struct {
	ID        MuseumID
	AccountID string
	StyleID   string
	Privacy   Privacy
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Room struct {
	ID           RoomID
	MuseumID     MuseumID
	Name         string
	VariantID    string
	Privacy      Privacy
	MusicTrackID string
	PhotoSlots   []PhotoSlotAssignment
	Sculptures   []SculptureInstance
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (r Room) HasMusic() bool { return r.MusicTrackID != "" }

type PhotoSlotAssignment struct {
	SlotIndex    int
	PhotoAssetID string
	Caption      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type SculptureInstance struct {
	SlotIndex int
	CatalogID string
	CreatedAt time.Time
}

func (r Room) HasCapacityForPhoto() bool {
	return len(r.PhotoSlots) < MaxPhotosPerRoom
}

func (r Room) HasCapacityForSculpture() bool {
	return len(r.Sculptures) < MaxSculpturesPerRoom
}

func IsValidPhotoSlotIndex(index int) bool {
	return index >= 0 && index < MaxPhotosPerRoom
}

func IsValidSculptureSlotIndex(index int) bool {
	return index >= 0 && index < MaxSculpturesPerRoom
}
