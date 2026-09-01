package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"muse-backend/internal/museum/domain"
)

type MuseumService struct {
	repo    MuseumRepository
	catalog CatalogReading
	uow     UnitOfWork
	photos  *photoDependencies
}

type photoDependencies struct {
	assets   PhotoAssetCommitting
	delivery PhotoDeliveryTicketing
}

func NewMuseumService(repo MuseumRepository, catalog CatalogReading) *MuseumService {
	return &MuseumService{repo: repo, catalog: catalog}
}

func (s *MuseumService) WithUnitOfWork(uow UnitOfWork) *MuseumService {
	s.uow = uow
	return s
}

func (s *MuseumService) EnablePhotos(uow UnitOfWork, assets PhotoAssetCommitting, delivery PhotoDeliveryTicketing) *MuseumService {
	s.uow = uow
	s.photos = &photoDependencies{assets: assets, delivery: delivery}
	return s
}

func (s *MuseumService) CreateMuseum(ctx context.Context, accountID string, styleID string) (domain.Museum, error) {
	if err := s.requireKnownStyle(ctx, styleID); err != nil {
		return domain.Museum{}, err
	}

	now := time.Now()
	return s.repo.CreateMuseum(ctx, domain.Museum{
		AccountID: accountID,
		StyleID:   styleID,
		Privacy:   domain.PrivacyPrivate,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *MuseumService) FindMuseum(ctx context.Context, accountID string) (domain.Museum, error) {
	return s.repo.FindMuseumByAccount(ctx, accountID)
}

func (s *MuseumService) ChangeStyle(ctx context.Context, accountID string, styleID string) error {
	museum, err := s.requireOwnedMuseum(ctx, accountID)
	if err != nil {
		return err
	}
	if err := s.requireKnownStyle(ctx, styleID); err != nil {
		return err
	}
	return s.repo.UpdateMuseumStyle(ctx, museum.ID, styleID)
}

func (s *MuseumService) ChangePrivacy(ctx context.Context, accountID string, privacy domain.Privacy) error {
	if !domain.IsValidPrivacy(privacy) {
		return domain.ErrInvalidPrivacy
	}
	museum, err := s.requireOwnedMuseum(ctx, accountID)
	if err != nil {
		return err
	}
	return s.repo.UpdateMuseumPrivacy(ctx, museum.ID, privacy)
}

func (s *MuseumService) CreateRoom(ctx context.Context, accountID string, name string, variantID string) (domain.Room, error) {
	museum, err := s.requireOwnedMuseum(ctx, accountID)
	if err != nil {
		return domain.Room{}, err
	}
	if err := s.requireVariantInStyle(ctx, variantID, museum.StyleID); err != nil {
		return domain.Room{}, err
	}

	now := time.Now()
	return s.repo.CreateRoom(ctx, domain.Room{
		MuseumID:  museum.ID,
		Name:      name,
		VariantID: variantID,
		Privacy:   domain.PrivacyPrivate,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *MuseumService) ListRooms(ctx context.Context, accountID string) ([]domain.Room, error) {
	museum, err := s.requireOwnedMuseum(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListRooms(ctx, museum.ID)
}

func (s *MuseumService) UpdateRoom(
	ctx context.Context,
	accountID string,
	roomID domain.RoomID,
	patch domain.RoomPatch,
) error {
	if patch.Privacy != nil && !domain.IsValidPrivacy(*patch.Privacy) {
		return domain.ErrInvalidPrivacy
	}
	museum, _, err := s.requireOwnedRoom(ctx, accountID, roomID)
	if err != nil {
		return err
	}
	if patch.VariantID != nil {
		if err := s.requireVariantInStyle(ctx, *patch.VariantID, museum.StyleID); err != nil {
			return err
		}
	}
	if patch.IsEmpty() {
		return nil
	}
	return s.repo.UpdateRoom(ctx, roomID, patch)
}

// MARK: - Visibility

func (s *MuseumService) VisibleMuseum(ctx context.Context, callerAccountID string, museumID domain.MuseumID) (domain.Museum, []domain.Room, error) {
	museum, err := s.repo.FindMuseumByID(ctx, museumID)
	if err != nil {
		if errors.Is(err, domain.ErrMuseumNotFound) {
			return domain.Museum{}, nil, domain.ErrNotVisible
		}
		return domain.Museum{}, nil, err
	}
	if museum.AccountID != callerAccountID {
		return domain.Museum{}, nil, domain.ErrNotVisible
	}
	rooms, err := s.repo.ListRooms(ctx, museum.ID)
	if err != nil {
		return domain.Museum{}, nil, err
	}
	return museum, rooms, nil
}

func (s *MuseumService) VisibleRoom(ctx context.Context, callerAccountID string, museumID domain.MuseumID, roomID domain.RoomID) (domain.Room, error) {
	museum, err := s.repo.FindMuseumByID(ctx, museumID)
	if err != nil {
		if errors.Is(err, domain.ErrMuseumNotFound) {
			return domain.Room{}, domain.ErrNotVisible
		}
		return domain.Room{}, err
	}
	if museum.AccountID != callerAccountID {
		return domain.Room{}, domain.ErrNotVisible
	}
	room, err := s.repo.FindRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, domain.ErrRoomNotFound) {
			return domain.Room{}, domain.ErrNotVisible
		}
		return domain.Room{}, err
	}
	if room.MuseumID != museum.ID {
		return domain.Room{}, domain.ErrNotVisible
	}
	return room, nil
}

func (s *MuseumService) MuseumByID(ctx context.Context, museumID domain.MuseumID) (domain.Museum, error) {
	return s.repo.FindMuseumByID(ctx, museumID)
}

func (s *MuseumService) VisitorMuseum(ctx context.Context, museumID domain.MuseumID) (domain.Museum, []domain.Room, error) {
	museum, err := s.repo.FindMuseumByID(ctx, museumID)
	if err != nil {
		if errors.Is(err, domain.ErrMuseumNotFound) {
			return domain.Museum{}, nil, domain.ErrNotVisible
		}
		return domain.Museum{}, nil, err
	}
	if !domain.VisitorCanSeeMuseum(museum) {
		return domain.Museum{}, nil, domain.ErrNotVisible
	}
	rooms, err := s.repo.ListRooms(ctx, museum.ID)
	if err != nil {
		return domain.Museum{}, nil, err
	}
	return museum, domain.VisibleRooms(museum, rooms, ""), nil
}

func (s *MuseumService) VisitorRoom(ctx context.Context, museumID domain.MuseumID, roomID domain.RoomID) (domain.Room, error) {
	museum, err := s.repo.FindMuseumByID(ctx, museumID)
	if err != nil {
		if errors.Is(err, domain.ErrMuseumNotFound) {
			return domain.Room{}, domain.ErrNotVisible
		}
		return domain.Room{}, err
	}
	if !domain.VisitorCanSeeMuseum(museum) {
		return domain.Room{}, domain.ErrNotVisible
	}
	room, err := s.repo.FindRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, domain.ErrRoomNotFound) {
			return domain.Room{}, domain.ErrNotVisible
		}
		return domain.Room{}, err
	}
	if !domain.VisitorCanSeeRoom(museum, room) {
		return domain.Room{}, domain.ErrNotVisible
	}
	return room, nil
}

func (s *MuseumService) AssignRoomMusic(ctx context.Context, accountID string, roomID domain.RoomID, trackID string) error {
	if trackID == "" {
		return domain.ErrUnknownMusicTrack
	}
	if _, _, err := s.requireOwnedRoom(ctx, accountID, roomID); err != nil {
		return err
	}
	exists, err := s.catalog.MusicTrackExists(ctx, trackID)
	if err != nil {
		return err
	}
	if !exists {
		return domain.ErrUnknownMusicTrack
	}
	return s.repo.SetRoomMusic(ctx, roomID, &trackID)
}

func (s *MuseumService) RemoveRoomMusic(ctx context.Context, accountID string, roomID domain.RoomID) error {
	if _, _, err := s.requireOwnedRoom(ctx, accountID, roomID); err != nil {
		return err
	}
	return s.repo.SetRoomMusic(ctx, roomID, nil)
}

func (s *MuseumService) VisitorPhotoDownloadTickets(
	ctx context.Context,
	museumID domain.MuseumID,
	roomID domain.RoomID,
) ([]PhotoDownloadTicket, error) {
	if s.photos == nil {
		return nil, domain.ErrPhotosUnavailable
	}
	museum, err := s.repo.FindMuseumByID(ctx, museumID)
	if err != nil {
		if errors.Is(err, domain.ErrMuseumNotFound) {
			return nil, domain.ErrNotVisible
		}
		return nil, err
	}
	if !domain.VisitorCanSeeMuseum(museum) {
		return nil, domain.ErrNotVisible
	}
	room, err := s.repo.FindRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, domain.ErrRoomNotFound) {
			return nil, domain.ErrNotVisible
		}
		return nil, err
	}
	if !domain.VisitorCanSeeRoom(museum, room) {
		return nil, domain.ErrNotVisible
	}
	if len(room.PhotoSlots) == 0 {
		return []PhotoDownloadTicket{}, nil
	}
	assetIDs := make([]string, 0, len(room.PhotoSlots))
	for _, slot := range room.PhotoSlots {
		if slot.PhotoAssetID != "" {
			assetIDs = append(assetIDs, slot.PhotoAssetID)
		}
	}
	return s.photos.delivery.IssuePhotoDownloadTickets(ctx, museum.AccountID, assetIDs)
}

func (s *MuseumService) DeleteRoom(ctx context.Context, accountID string, roomID domain.RoomID) error {
	if _, _, err := s.requireOwnedRoom(ctx, accountID, roomID); err != nil {
		return err
	}
	return s.repo.DeleteRoom(ctx, roomID)
}

func (s *MuseumService) FindRoom(ctx context.Context, accountID string, roomID domain.RoomID) (domain.Room, error) {
	_, room, err := s.requireOwnedRoom(ctx, accountID, roomID)
	return room, err
}

// MARK: - Photos

func (s *MuseumService) AddPhotos(
	ctx context.Context,
	accountID string,
	roomID domain.RoomID,
	assetIDs []string,
) ([]domain.PhotoSlotAssignment, error) {
	if s.photos == nil || s.uow == nil {
		return nil, domain.ErrPhotosUnavailable
	}
	if len(assetIDs) == 0 {
		return nil, domain.ErrNoPhotosSupplied
	}
	if len(assetIDs) > domain.MaxPhotosPerRoom {
		return nil, domain.ErrPhotoCapacityReached
	}
	if hasDuplicates(assetIDs) {
		return nil, domain.ErrDuplicatePhotoAssetIDs
	}

	museum, _, err := s.requireOwnedRoom(ctx, accountID, roomID)
	if err != nil {
		return nil, err
	}
	if err := s.photos.assets.VerifyPhotoAssets(ctx, accountID, assetIDs); err != nil {
		return nil, err
	}

	var result []domain.PhotoSlotAssignment
	err = s.uow.Run(ctx, func(ctx context.Context) error {
		room, err := s.repo.LockRoomForUpdate(ctx, roomID)
		if err != nil {
			return err
		}
		if room.MuseumID != museum.ID {
			return domain.ErrNotOwner
		}
		if !slotsAreContiguous(room.PhotoSlots) {
			return domain.ErrSlotLayoutInconsistent
		}

		existingByAsset := make(map[string]domain.PhotoSlotAssignment, len(room.PhotoSlots))
		for _, slot := range room.PhotoSlots {
			existingByAsset[slot.PhotoAssetID] = slot
		}
		elsewhere, err := s.repo.FindPhotoSlotRoomsByAssetIDs(ctx, assetIDs)
		if err != nil {
			return err
		}

		now := time.Now()
		nextIndex := len(room.PhotoSlots)
		var toInsert []domain.PhotoSlotAssignment
		result = make([]domain.PhotoSlotAssignment, 0, len(assetIDs))

		for _, assetID := range assetIDs {
			if existing, ok := existingByAsset[assetID]; ok {
				result = append(result, existing)
				continue
			}
			if otherRoom, ok := elsewhere[assetID]; ok && otherRoom != roomID {
				return &domain.PhotoAssetError{AssetID: assetID, Err: domain.ErrPhotoAssetAlreadyAssigned}
			}
			slot := domain.PhotoSlotAssignment{
				SlotIndex:    nextIndex,
				PhotoAssetID: assetID,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			nextIndex++
			toInsert = append(toInsert, slot)
			result = append(result, slot)
		}

		if len(room.PhotoSlots)+len(toInsert) > domain.MaxPhotosPerRoom {
			return domain.ErrPhotoCapacityReached
		}
		if len(toInsert) == 0 {
			return nil
		}

		if err := s.repo.InsertPhotoSlots(ctx, roomID, toInsert); err != nil {
			return err
		}
		newIDs := make([]string, 0, len(toInsert))
		for _, slot := range toInsert {
			newIDs = append(newIDs, slot.PhotoAssetID)
		}
		return s.photos.assets.CommitPhotoAssets(ctx, newIDs)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *MuseumService) PhotoDownloadTickets(
	ctx context.Context,
	accountID string,
	roomID domain.RoomID,
) ([]PhotoDownloadTicket, error) {
	if s.photos == nil {
		return nil, domain.ErrPhotosUnavailable
	}
	_, room, err := s.requireOwnedRoom(ctx, accountID, roomID)
	if err != nil {
		return nil, err
	}
	if len(room.PhotoSlots) == 0 {
		return []PhotoDownloadTicket{}, nil
	}
	assetIDs := make([]string, 0, len(room.PhotoSlots))
	for _, slot := range room.PhotoSlots {
		if slot.PhotoAssetID != "" {
			assetIDs = append(assetIDs, slot.PhotoAssetID)
		}
	}
	return s.photos.delivery.IssuePhotoDownloadTickets(ctx, accountID, assetIDs)
}

func (s *MuseumService) ReorderPhotos(
	ctx context.Context,
	accountID string,
	roomID domain.RoomID,
	orderedAssetIDs []string,
) error {
	if s.uow == nil {
		return domain.ErrTransactionsUnavailable
	}
	if len(orderedAssetIDs) == 0 || len(orderedAssetIDs) > domain.MaxPhotosPerRoom || hasDuplicates(orderedAssetIDs) {
		return domain.ErrInvalidPhotoOrder
	}
	for _, id := range orderedAssetIDs {
		if id == "" {
			return domain.ErrInvalidPhotoOrder
		}
	}

	museum, _, err := s.requireOwnedRoom(ctx, accountID, roomID)
	if err != nil {
		return err
	}

	return s.uow.Run(ctx, func(ctx context.Context) error {
		room, err := s.repo.LockRoomForUpdate(ctx, roomID)
		if err != nil {
			return err
		}
		if room.MuseumID != museum.ID {
			return domain.ErrNotOwner
		}

		if len(orderedAssetIDs) != len(room.PhotoSlots) {
			return domain.ErrPhotoOrderMismatch
		}
		current := make(map[string]int, len(room.PhotoSlots))
		for _, slot := range room.PhotoSlots {
			current[slot.PhotoAssetID] = slot.SlotIndex
		}
		unchanged := true
		for newIndex, assetID := range orderedAssetIDs {
			oldIndex, present := current[assetID]
			if !present {
				return domain.ErrPhotoOrderMismatch
			}
			if oldIndex != newIndex {
				unchanged = false
			}
		}
		if unchanged {
			return nil
		}

		return s.repo.ReorderPhotoSlots(ctx, roomID, orderedAssetIDs)
	})
}

func (s *MuseumService) SetPhotoCaption(
	ctx context.Context,
	accountID string,
	roomID domain.RoomID,
	photoAssetID string,
	caption string,
) error {
	if s.uow == nil {
		return domain.ErrTransactionsUnavailable
	}
	if photoAssetID == "" {
		return domain.ErrPhotoNotInRoom
	}
	if err := domain.ValidateCaption(caption); err != nil {
		return err
	}
	normalised := domain.NormalisedCaption(caption)

	museum, _, err := s.requireOwnedRoom(ctx, accountID, roomID)
	if err != nil {
		return err
	}

	return s.uow.Run(ctx, func(ctx context.Context) error {
		room, err := s.repo.LockRoomForUpdate(ctx, roomID)
		if err != nil {
			return err
		}
		if room.MuseumID != museum.ID {
			return domain.ErrNotOwner
		}

		var current *domain.PhotoSlotAssignment
		for index := range room.PhotoSlots {
			if room.PhotoSlots[index].PhotoAssetID == photoAssetID {
				current = &room.PhotoSlots[index]
				break
			}
		}
		if current == nil {
			return domain.ErrPhotoNotInRoom
		}
		if current.Caption == normalised {
			return nil
		}

		return s.repo.UpdatePhotoCaption(ctx, roomID, photoAssetID, normalised)
	})
}

func (s *MuseumService) ReplacePhoto(
	ctx context.Context,
	accountID string,
	roomID domain.RoomID,
	photoAssetID string,
	replacementAssetID string,
) error {
	if s.photos == nil || s.uow == nil {
		return domain.ErrPhotosUnavailable
	}
	if photoAssetID == "" {
		return domain.ErrPhotoNotInRoom
	}
	if replacementAssetID == "" || replacementAssetID == photoAssetID {
		return domain.ErrInvalidReplacement
	}

	museum, _, err := s.requireOwnedRoom(ctx, accountID, roomID)
	if err != nil {
		return err
	}
	if err := s.photos.assets.VerifyPhotoAssets(ctx, accountID, []string{replacementAssetID}); err != nil {
		return err
	}

	return s.uow.Run(ctx, func(ctx context.Context) error {
		room, err := s.repo.LockRoomForUpdate(ctx, roomID)
		if err != nil {
			return err
		}
		if room.MuseumID != museum.ID {
			return domain.ErrNotOwner
		}

		var current *domain.PhotoSlotAssignment
		replacementPresent := false
		for index := range room.PhotoSlots {
			switch room.PhotoSlots[index].PhotoAssetID {
			case photoAssetID:
				current = &room.PhotoSlots[index]
			case replacementAssetID:
				replacementPresent = true
			}
		}
		if current == nil {
			if replacementPresent {
				return nil
			}
			return domain.ErrPhotoNotInRoom
		}
		if replacementPresent {
			return &domain.PhotoAssetError{AssetID: replacementAssetID, Err: domain.ErrPhotoAssetAlreadyAssigned}
		}
		elsewhere, err := s.repo.FindPhotoSlotRoomsByAssetIDs(ctx, []string{replacementAssetID})
		if err != nil {
			return err
		}
		if otherRoom, ok := elsewhere[replacementAssetID]; ok && otherRoom != roomID {
			return &domain.PhotoAssetError{AssetID: replacementAssetID, Err: domain.ErrPhotoAssetAlreadyAssigned}
		}

		if err := s.repo.ReplacePhotoSlotAsset(ctx, roomID, photoAssetID, replacementAssetID); err != nil {
			return err
		}
		if err := s.photos.assets.CommitPhotoAssets(ctx, []string{replacementAssetID}); err != nil {
			return err
		}
		return s.photos.assets.ReleasePhotoAssets(ctx, []string{photoAssetID})
	})
}

func (s *MuseumService) DeletePhoto(
	ctx context.Context,
	accountID string,
	roomID domain.RoomID,
	photoAssetID string,
) error {
	if s.photos == nil || s.uow == nil {
		return domain.ErrPhotosUnavailable
	}
	if photoAssetID == "" {
		return domain.ErrPhotoNotInRoom
	}

	museum, _, err := s.requireOwnedRoom(ctx, accountID, roomID)
	if err != nil {
		return err
	}

	return s.uow.Run(ctx, func(ctx context.Context) error {
		room, err := s.repo.LockRoomForUpdate(ctx, roomID)
		if err != nil {
			return err
		}
		if room.MuseumID != museum.ID {
			return domain.ErrNotOwner
		}
		if !slotsAreContiguous(room.PhotoSlots) {
			return domain.ErrSlotLayoutInconsistent
		}

		present := false
		for _, slot := range room.PhotoSlots {
			if slot.PhotoAssetID == photoAssetID {
				present = true
				break
			}
		}
		if !present {
			return domain.ErrPhotoNotInRoom
		}

		if err := s.repo.DeletePhotoSlotCompacting(ctx, roomID, photoAssetID); err != nil {
			return err
		}
		return s.photos.assets.ReleasePhotoAssets(ctx, []string{photoAssetID})
	})
}

// MARK: - Sculptures

func (s *MuseumService) AddSculpture(
	ctx context.Context,
	accountID string,
	roomID domain.RoomID,
	catalogID string,
) (domain.SculptureInstance, error) {
	if s.uow == nil {
		return domain.SculptureInstance{}, domain.ErrTransactionsUnavailable
	}

	museum, _, err := s.requireOwnedRoom(ctx, accountID, roomID)
	if err != nil {
		return domain.SculptureInstance{}, err
	}
	if err := s.requireKnownSculpture(ctx, catalogID); err != nil {
		return domain.SculptureInstance{}, err
	}

	var placed domain.SculptureInstance
	err = s.uow.Run(ctx, func(ctx context.Context) error {
		room, err := s.repo.LockRoomForUpdate(ctx, roomID)
		if err != nil {
			return err
		}
		if room.MuseumID != museum.ID {
			return domain.ErrNotOwner
		}

		slotIndex, free := domain.LowestFreeSculptureSlot(room.Sculptures)
		if !free {
			return domain.ErrSculptureCapacityReached
		}

		placed = domain.SculptureInstance{
			SlotIndex: slotIndex,
			CatalogID: catalogID,
			CreatedAt: time.Now(),
		}
		return s.repo.InsertSculpture(ctx, roomID, placed)
	})
	if err != nil {
		return domain.SculptureInstance{}, err
	}
	return placed, nil
}

func (s *MuseumService) RemoveSculpture(
	ctx context.Context,
	accountID string,
	roomID domain.RoomID,
	slotIndex int,
) error {
	if !domain.IsValidSculptureSlotIndex(slotIndex) {
		return domain.ErrInvalidSculptureSlot
	}
	if _, _, err := s.requireOwnedRoom(ctx, accountID, roomID); err != nil {
		return err
	}
	return s.repo.DeleteSculpture(ctx, roomID, slotIndex)
}

func (s *MuseumService) requireKnownSculpture(ctx context.Context, catalogID string) error {
	if catalogID == "" {
		return domain.ErrUnknownSculpture
	}
	exists, err := s.catalog.SculptureExists(ctx, catalogID)
	if err != nil {
		return fmt.Errorf("museum: verify sculpture: %w", err)
	}
	if !exists {
		return domain.ErrUnknownSculpture
	}
	return nil
}

func hasDuplicates(ids []string) bool {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			return true
		}
		seen[id] = struct{}{}
	}
	return false
}

func slotsAreContiguous(slots []domain.PhotoSlotAssignment) bool {
	seen := make(map[int]struct{}, len(slots))
	for _, slot := range slots {
		if slot.SlotIndex < 0 || slot.SlotIndex >= len(slots) {
			return false
		}
		if _, dup := seen[slot.SlotIndex]; dup {
			return false
		}
		seen[slot.SlotIndex] = struct{}{}
	}
	return true
}

// MARK: - Invariant helpers

func (s *MuseumService) requireOwnedMuseum(ctx context.Context, accountID string) (domain.Museum, error) {
	museum, err := s.repo.FindMuseumByAccount(ctx, accountID)
	if err != nil {
		return domain.Museum{}, err
	}
	return museum, nil
}

func (s *MuseumService) requireOwnedRoom(
	ctx context.Context,
	accountID string,
	roomID domain.RoomID,
) (domain.Museum, domain.Room, error) {
	museum, err := s.requireOwnedMuseum(ctx, accountID)
	if err != nil {
		return domain.Museum{}, domain.Room{}, err
	}

	room, err := s.repo.FindRoom(ctx, roomID)
	if err != nil {
		return domain.Museum{}, domain.Room{}, err
	}
	if room.MuseumID != museum.ID {
		return domain.Museum{}, domain.Room{}, domain.ErrNotOwner
	}
	return museum, room, nil
}

func (s *MuseumService) requireKnownStyle(ctx context.Context, styleID string) error {
	exists, err := s.catalog.StyleExists(ctx, styleID)
	if err != nil {
		return fmt.Errorf("museum: verify style: %w", err)
	}
	if !exists {
		return domain.ErrUnknownStyle
	}
	return nil
}

func (s *MuseumService) requireVariantInStyle(ctx context.Context, variantID string, styleID string) error {
	variantStyle, found, err := s.catalog.VariantStyle(ctx, variantID)
	if err != nil {
		return fmt.Errorf("museum: verify variant: %w", err)
	}
	if !found {
		return domain.ErrUnknownVariant
	}
	if variantStyle != styleID {
		return domain.ErrVariantStyleMismatch
	}
	return nil
}
