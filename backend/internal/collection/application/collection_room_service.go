package application

import (
	"context"
	"fmt"

	"muse-backend/internal/collection/domain"
)

type CollectionRoomService struct {
	rooms      CollectionRoomRepository
	categories CollectionCategoryReading
	designs    CollectionDesignReading
	models     CollectionModelReading
	uow        UnitOfWork
	music      MusicTrackReading
	capacity   ItemCapacityAuthority
}

func NewCollectionRoomService(
	rooms CollectionRoomRepository,
	categories CollectionCategoryReading,
	designs CollectionDesignReading,
	models CollectionModelReading,
) *CollectionRoomService {
	return &CollectionRoomService{
		rooms:      rooms,
		categories: categories,
		designs:    designs,
		models:     models,
	}
}

func (s *CollectionRoomService) WithUnitOfWork(uow UnitOfWork) *CollectionRoomService {
	s.uow = uow
	return s
}

func (s *CollectionRoomService) WithMusicCatalog(music MusicTrackReading) *CollectionRoomService {
	s.music = music
	return s
}

type CreateInput struct {
	Name       string
	CategoryID string
	DesignID   string
}

func (s *CollectionRoomService) Create(
	ctx context.Context,
	accountID string,
	input CreateInput,
) (domain.CollectionRoom, error) {
	if err := domain.ValidateName(input.Name); err != nil {
		return domain.CollectionRoom{}, err
	}
	if err := domain.ValidateDesignReference(input.DesignID); err != nil {
		return domain.CollectionRoom{}, err
	}
	if input.CategoryID == "" {
		return domain.CollectionRoom{}, domain.ErrCategoryRequired
	}
	if err := s.requireKnownCategory(ctx, input.CategoryID); err != nil {
		return domain.CollectionRoom{}, err
	}
	if input.DesignID != "" {
		if err := s.requireApplicableDesign(ctx, input.DesignID, input.CategoryID); err != nil {
			return domain.CollectionRoom{}, err
		}
	}

	room, err := s.rooms.Create(ctx, domain.CollectionRoom{
		AccountID:   accountID,
		Name:        input.Name,
		CategoryID:  input.CategoryID,
		DesignID:    input.DesignID,
		CurrentTier: domain.BaseTier,
	})
	if err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("create collection room: %w", err)
	}
	return room, nil
}

func (s *CollectionRoomService) List(ctx context.Context, accountID string) ([]domain.CollectionRoom, error) {
	rooms, err := s.rooms.ListForAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("list collection rooms: %w", err)
	}
	return rooms, nil
}

func (s *CollectionRoomService) Find(
	ctx context.Context,
	accountID string,
	id domain.CollectionRoomID,
) (domain.CollectionRoom, error) {
	return s.ownedRoom(ctx, accountID, id)
}

func (s *CollectionRoomService) VisitorRoom(
	ctx context.Context,
	id domain.CollectionRoomID,
) (domain.CollectionRoom, error) {
	return s.rooms.Find(ctx, id)
}

func (s *CollectionRoomService) AssignMusic(
	ctx context.Context,
	accountID string,
	id domain.CollectionRoomID,
	trackID string,
) (domain.CollectionRoom, error) {
	if trackID == "" {
		return domain.CollectionRoom{}, domain.ErrUnknownMusicTrack
	}
	if _, err := s.ownedRoom(ctx, accountID, id); err != nil {
		return domain.CollectionRoom{}, err
	}
	if s.music == nil {
		return domain.CollectionRoom{}, domain.ErrUnknownMusicTrack
	}
	exists, err := s.music.MusicTrackExists(ctx, trackID)
	if err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("look up music track: %w", err)
	}
	if !exists {
		return domain.CollectionRoom{}, domain.ErrUnknownMusicTrack
	}
	if err := s.rooms.SetMusic(ctx, id, &trackID); err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("assign collection room music: %w", err)
	}
	return s.rooms.Find(ctx, id)
}

func (s *CollectionRoomService) RemoveMusic(
	ctx context.Context,
	accountID string,
	id domain.CollectionRoomID,
) (domain.CollectionRoom, error) {
	if _, err := s.ownedRoom(ctx, accountID, id); err != nil {
		return domain.CollectionRoom{}, err
	}
	if err := s.rooms.SetMusic(ctx, id, nil); err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("remove collection room music: %w", err)
	}
	return s.rooms.Find(ctx, id)
}

func (s *CollectionRoomService) WithEntitlements(capacity ItemCapacityAuthority) *CollectionRoomService {
	s.capacity = capacity
	return s
}

func (s *CollectionRoomService) CountItemsForAccount(ctx context.Context, accountID string) (int, error) {
	return s.rooms.CountItemsForAccount(ctx, accountID)
}

func (s *CollectionRoomService) Update(
	ctx context.Context,
	accountID string,
	id domain.CollectionRoomID,
	patch domain.CollectionRoomPatch,
) (domain.CollectionRoom, error) {
	if patch.IsEmpty() {
		return domain.CollectionRoom{}, domain.ErrEmptyPatch
	}
	if patch.Name != nil {
		if err := domain.ValidateName(*patch.Name); err != nil {
			return domain.CollectionRoom{}, err
		}
	}
	if patch.DesignID != nil {
		if err := domain.ValidateDesignReference(*patch.DesignID); err != nil {
			return domain.CollectionRoom{}, err
		}
	}
	if patch.CategoryID != nil {
		if *patch.CategoryID == "" {
			return domain.CollectionRoom{}, domain.ErrCategoryRequired
		}
		if err := s.requireKnownCategory(ctx, *patch.CategoryID); err != nil {
			return domain.CollectionRoom{}, err
		}
	}

	existing, err := s.ownedRoom(ctx, accountID, id)
	if err != nil {
		return domain.CollectionRoom{}, err
	}

	effectiveCategory := existing.CategoryID
	if patch.CategoryID != nil {
		effectiveCategory = *patch.CategoryID
	}
	effectiveDesign := existing.DesignID
	if patch.DesignID != nil {
		effectiveDesign = *patch.DesignID
	}
	if effectiveDesign != "" {
		if err := s.requireApplicableDesign(ctx, effectiveDesign, effectiveCategory); err != nil {
			return domain.CollectionRoom{}, err
		}
	}
	if err := s.rooms.Update(ctx, id, patch); err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("update collection room: %w", err)
	}
	return s.ownedRoom(ctx, accountID, id)
}

func (s *CollectionRoomService) Delete(
	ctx context.Context,
	accountID string,
	id domain.CollectionRoomID,
) error {
	if _, err := s.ownedRoom(ctx, accountID, id); err != nil {
		return err
	}
	if err := s.rooms.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete collection room: %w", err)
	}
	return nil
}

func (s *CollectionRoomService) requireKnownCategory(ctx context.Context, categoryID string) error {
	if err := domain.ValidateCategoryReference(categoryID); err != nil {
		return err
	}
	known, err := s.categories.CollectionCategoryExists(ctx, categoryID)
	if err != nil {
		return fmt.Errorf("look up collection category: %w", err)
	}
	if !known {
		return domain.ErrUnknownCategory
	}
	return nil
}

func (s *CollectionRoomService) RatchetTier(
	ctx context.Context,
	accountID string,
	id domain.CollectionRoomID,
	requested domain.Tier,
) (domain.CollectionRoom, error) {
	room, err := s.ownedRoom(ctx, accountID, id)
	if err != nil {
		return domain.CollectionRoom{}, err
	}

	if room.DesignID == "" {
		return domain.CollectionRoom{}, domain.ErrDesignRequiredForTier
	}

	authoredTiers, found, err := s.designs.DesignTierBound(ctx, room.DesignID)
	if err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("look up design tier bound: %w", err)
	}
	if !found {
		return domain.CollectionRoom{}, domain.ErrTierNotAuthored
	}
	if err := domain.ValidateTierRequest(requested, authoredTiers); err != nil {
		return domain.CollectionRoom{}, err
	}

	if _, err := s.rooms.RatchetTier(ctx, id, requested); err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("ratchet tier: %w", err)
	}
	return s.ownedRoom(ctx, accountID, id)
}

func (s *CollectionRoomService) requireApplicableDesign(
	ctx context.Context,
	designID string,
	categoryID string,
) error {
	if err := domain.ValidateDesignReference(designID); err != nil {
		return err
	}
	applicable, err := s.designs.IsDesignApplicable(ctx, designID, categoryID)
	if err != nil {
		return fmt.Errorf("look up collection design: %w", err)
	}
	if !applicable {
		return domain.ErrDesignNotApplicable
	}
	return nil
}

func (s *CollectionRoomService) ownedRoom(
	ctx context.Context,
	accountID string,
	id domain.CollectionRoomID,
) (domain.CollectionRoom, error) {
	room, err := s.rooms.Find(ctx, id)
	if err != nil {
		return domain.CollectionRoom{}, err
	}
	if room.AccountID != accountID {
		return domain.CollectionRoom{}, domain.ErrCollectionRoomNotFound
	}
	return room, nil
}
