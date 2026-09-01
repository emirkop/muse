package application

import (
	"context"
	"time"

	"muse-backend/internal/museum/domain"
)

type MuseumRepository interface {
	CreateMuseum(ctx context.Context, museum domain.Museum) (domain.Museum, error)

	FindMuseumByAccount(ctx context.Context, accountID string) (domain.Museum, error)

	FindMuseumByID(ctx context.Context, id domain.MuseumID) (domain.Museum, error)

	UpdateMuseumStyle(ctx context.Context, id domain.MuseumID, styleID string) error

	UpdateMuseumPrivacy(ctx context.Context, id domain.MuseumID, privacy domain.Privacy) error

	CreateRoom(ctx context.Context, room domain.Room) (domain.Room, error)

	ListRooms(ctx context.Context, museumID domain.MuseumID) ([]domain.Room, error)

	FindRoom(ctx context.Context, id domain.RoomID) (domain.Room, error)

	UpdateRoom(ctx context.Context, id domain.RoomID, patch domain.RoomPatch) error

	SetRoomMusic(ctx context.Context, id domain.RoomID, trackID *string) error

	DeleteRoom(ctx context.Context, id domain.RoomID) error

	LockRoomForUpdate(ctx context.Context, id domain.RoomID) (domain.Room, error)

	InsertPhotoSlots(ctx context.Context, roomID domain.RoomID, slots []domain.PhotoSlotAssignment) error

	FindPhotoSlotRoomsByAssetIDs(ctx context.Context, assetIDs []string) (map[string]domain.RoomID, error)

	ReorderPhotoSlots(ctx context.Context, roomID domain.RoomID, orderedAssetIDs []string) error

	UpdatePhotoCaption(ctx context.Context, roomID domain.RoomID, photoAssetID string, caption string) error

	ReplacePhotoSlotAsset(ctx context.Context, roomID domain.RoomID, photoAssetID string, replacementAssetID string) error

	DeletePhotoSlotCompacting(ctx context.Context, roomID domain.RoomID, photoAssetID string) error

	InsertSculpture(ctx context.Context, roomID domain.RoomID, sculpture domain.SculptureInstance) error

	DeleteSculpture(ctx context.Context, roomID domain.RoomID, slotIndex int) error
}

type UnitOfWork interface {
	Run(ctx context.Context, fn func(ctx context.Context) error) error
}

type PhotoAssetCommitting interface {
	VerifyPhotoAssets(ctx context.Context, accountID string, assetIDs []string) error

	CommitPhotoAssets(ctx context.Context, assetIDs []string) error

	ReleasePhotoAssets(ctx context.Context, assetIDs []string) error
}

type PhotoDownloadTicket struct {
	PhotoAssetID string
	URL          string
	ExpiresAt    time.Time
	PixelWidth   int
	PixelHeight  int
}

type PhotoDeliveryTicketing interface {
	IssuePhotoDownloadTickets(ctx context.Context, accountID string, assetIDs []string) ([]PhotoDownloadTicket, error)
}

type CatalogReading interface {
	StyleExists(ctx context.Context, styleID string) (bool, error)

	VariantStyle(ctx context.Context, variantID string) (styleID string, found bool, err error)

	SculptureExists(ctx context.Context, sculptureID string) (bool, error)
	MusicTrackExists(ctx context.Context, trackID string) (bool, error)
}
