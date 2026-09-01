package application

import (
	"context"
	"fmt"

	"muse-backend/internal/collection/domain"
)

func (s *CollectionRoomService) AddItem(
	ctx context.Context,
	accountID string,
	id domain.CollectionRoomID,
	catalogModelID string,
	appAssetVersion int,
) (domain.CollectionRoom, error) {
	if s.uow == nil {
		return domain.CollectionRoom{}, domain.ErrTransactionsUnavailable
	}
	if err := domain.ValidateModelReference(catalogModelID); err != nil {
		return domain.CollectionRoom{}, err
	}

	room, err := s.ownedRoom(ctx, accountID, id)
	if err != nil {
		return domain.CollectionRoom{}, err
	}

	if err := s.requireItemCapacity(ctx, accountID, id); err != nil {
		return domain.CollectionRoom{}, err
	}

	if room.CategoryID == "" {
		return domain.CollectionRoom{}, domain.ErrModelNotAvailable
	}
	placeable, err := s.models.IsCollectionModelPlaceable(ctx, catalogModelID, room.CategoryID)
	if err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("look up collection model: %w", err)
	}
	if !placeable {
		return domain.CollectionRoom{}, domain.ErrModelNotAvailable
	}

	err = s.uow.Run(ctx, func(ctx context.Context) error {
		if err := s.rooms.LockAccountItems(ctx, accountID); err != nil {
			return err
		}
		locked, err := s.rooms.LockRoomForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if locked.AccountID != accountID {
			return domain.ErrCollectionRoomNotFound
		}
		capacity, err := s.reachedTierCapacity(ctx, locked, appAssetVersion)
		if err != nil {
			return err
		}
		slot := domain.LowestFreeSlotIndex(locked.Items)
		if slot >= capacity {
			return domain.ErrTierCapacityReached
		}
		if err := s.requireItemCapacity(ctx, accountID, id); err != nil {
			return err
		}
		_, err = s.rooms.InsertItem(ctx, id, slot, catalogModelID)
		return err
	})
	if err != nil {
		return domain.CollectionRoom{}, err
	}
	return s.ownedRoom(ctx, accountID, id)
}

func (s *CollectionRoomService) requireItemCapacity(
	ctx context.Context,
	accountID string,
	id domain.CollectionRoomID,
) error {
	if s.capacity == nil {
		return nil
	}
	allowed, err := s.capacity.MayAddCollectionItem(ctx, accountID, string(id))
	if err != nil {
		return fmt.Errorf("check item capacity: %w", err)
	}
	if !allowed {
		return domain.ErrItemCapacityReached
	}
	return nil
}

func (s *CollectionRoomService) PlaceItemAtSlot(
	ctx context.Context,
	accountID string,
	id domain.CollectionRoomID,
	itemID domain.CollectionItemID,
	slotIndex int,
	appAssetVersion int,
) (domain.CollectionRoom, error) {
	if s.uow == nil {
		return domain.CollectionRoom{}, domain.ErrTransactionsUnavailable
	}
	if err := domain.ValidateSlotIndex(slotIndex); err != nil {
		return domain.CollectionRoom{}, err
	}
	if itemID == "" {
		return domain.CollectionRoom{}, domain.ErrItemNotInRoom
	}

	if _, err := s.ownedRoom(ctx, accountID, id); err != nil {
		return domain.CollectionRoom{}, err
	}

	err := s.uow.Run(ctx, func(ctx context.Context) error {
		locked, err := s.rooms.LockRoomForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if locked.AccountID != accountID {
			return domain.ErrCollectionRoomNotFound
		}

		capacity, err := s.reachedTierCapacity(ctx, locked, appAssetVersion)
		if err != nil {
			return err
		}
		change, err := domain.ResolveSlotChange(locked.Items, itemID, slotIndex, capacity)
		if err != nil {
			return err
		}
		switch change.Kind {
		case domain.SlotChangeNone:
			return nil
		case domain.SlotChangeSwap:
			return s.rooms.SwapItemSlots(ctx, id, change.Item.ID, change.Displaced.ID)
		default:
			return s.rooms.MoveItemToSlot(ctx, id, change.Item.ID, change.TargetSlotIndex)
		}
	})
	if err != nil {
		return domain.CollectionRoom{}, err
	}
	return s.ownedRoom(ctx, accountID, id)
}

func (s *CollectionRoomService) reachedTierCapacity(
	ctx context.Context,
	room domain.CollectionRoom,
	appAssetVersion int,
) (int, error) {
	if room.DesignID == "" {
		return 0, domain.ErrDesignRequiredForItems
	}
	capacity, found, err := s.designs.DesignSlotCapacity(ctx, room.DesignID, appAssetVersion, int(room.CurrentTier))
	if err != nil {
		return 0, fmt.Errorf("look up design slot capacity: %w", err)
	}
	if !found {
		return 0, domain.ErrDesignLayoutUnavailable
	}
	return capacity, nil
}
