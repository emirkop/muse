package application

import (
	"context"
	"time"

	"muse-backend/internal/sharing/domain"
)

type ShareLinkRepository interface {
	FindActiveByMuseum(ctx context.Context, museumID string) (domain.ShareLink, error)
	FindByCode(ctx context.Context, code domain.Code) (domain.ShareLink, error)
	EnsureActive(ctx context.Context, museumID string, code domain.Code, now time.Time) (domain.ShareLink, error)
	Rotate(ctx context.Context, museumID string, code domain.Code, now time.Time) (domain.ShareLink, error)
}

type CodeGenerator interface {
	NewCode() (domain.Code, error)
}

type Museum struct {
	ID             string
	OwnerAccountID string
	StyleID        string
	Public         bool
}

type MuseumReader interface {
	OwnedMuseum(ctx context.Context, accountID string) (Museum, error)
	MuseumByID(ctx context.Context, museumID string) (Museum, error)
}

type MuseumContent struct {
	ID      string
	StyleID string
	Rooms   []RoomSummary
}

type RoomSummary struct {
	ID        string
	Name      string
	VariantID string
}

type RoomContent struct {
	ID           string
	Name         string
	VariantID    string
	MusicTrackID string
	PhotoSlots   []PhotoSlot
	Sculptures   []Sculpture
}

type PhotoSlot struct {
	SlotIndex    int
	PhotoAssetID string
	Caption      string
}

type Sculpture struct {
	SlotIndex int
	CatalogID string
}

type PhotoTicket struct {
	PhotoAssetID string
	URL          string
	ExpiresAt    time.Time
	PixelWidth   int
	PixelHeight  int
}

type MuseumContentReader interface {
	VisitorMuseum(ctx context.Context, museumID string) (MuseumContent, error)
	VisitorRoom(ctx context.Context, museumID, roomID string) (RoomContent, error)
	VisitorRoomPhotoTickets(ctx context.Context, museumID, roomID string) ([]PhotoTicket, error)
}

type OwnerProfile struct {
	AvatarID string
}

type OwnerProfileReader interface {
	PublicProfile(ctx context.Context, accountID string) (OwnerProfile, error)
}

type VisitorMusicPolicy struct {
	AudibleToVisitors bool
}

type Clock func() time.Time
