package application

import (
	"context"

	"muse-backend/internal/collection/domain"
)

type CollectionRoomRepository interface {
	Create(ctx context.Context, room domain.CollectionRoom) (domain.CollectionRoom, error)

	ListForAccount(ctx context.Context, accountID string) ([]domain.CollectionRoom, error)

	Find(ctx context.Context, id domain.CollectionRoomID) (domain.CollectionRoom, error)

	Update(ctx context.Context, id domain.CollectionRoomID, patch domain.CollectionRoomPatch) error

	LockAccountItems(ctx context.Context, accountID string) error

	CountItemsForAccount(ctx context.Context, accountID string) (int, error)

	SetMusic(ctx context.Context, id domain.CollectionRoomID, trackID *string) error

	RatchetTier(ctx context.Context, id domain.CollectionRoomID, tier domain.Tier) (bool, error)

	LockRoomForUpdate(ctx context.Context, id domain.CollectionRoomID) (domain.CollectionRoom, error)

	InsertItem(
		ctx context.Context,
		roomID domain.CollectionRoomID,
		slotIndex int,
		catalogModelID string,
	) (domain.CollectionItem, error)

	MoveItemToSlot(
		ctx context.Context,
		roomID domain.CollectionRoomID,
		itemID domain.CollectionItemID,
		slotIndex int,
	) error

	SwapItemSlots(
		ctx context.Context,
		roomID domain.CollectionRoomID,
		first domain.CollectionItemID,
		second domain.CollectionItemID,
	) error

	Delete(ctx context.Context, id domain.CollectionRoomID) error
}

type ItemCapacityAuthority interface {
	MayAddCollectionItem(ctx context.Context, accountID string, collectionRoomID string) (bool, error)
}

type MusicTrackReading interface {
	MusicTrackExists(ctx context.Context, trackID string) (bool, error)
}

type CollectionCategoryReading interface {
	CollectionCategoryExists(ctx context.Context, categoryID string) (bool, error)
}

type CollectionDesignReading interface {
	IsDesignApplicable(ctx context.Context, designID string, categoryID string) (bool, error)

	DesignTierBound(ctx context.Context, designID string) (tierCount int, found bool, err error)

	DesignSlotCapacity(ctx context.Context, designID string, appAssetVersion int, tier int) (capacity int, found bool, err error)
}

type CollectionModelReading interface {
	IsCollectionModelPlaceable(ctx context.Context, modelID string, categoryID string) (bool, error)
}

type UnitOfWork interface {
	Run(ctx context.Context, fn func(ctx context.Context) error) error
}

type ItemRefusalRecording interface {
	RecordItemAddRefused(ctx context.Context, accountID string, reason string)
}
