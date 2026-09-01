package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"muse-backend/internal/sharing/domain"
)

type CollectionShareLinkService struct {
	links CollectionShareLinkRepository
	codes CodeGenerator
	rooms CollectionRoomReader
	clock Clock
	music VisitorMusicPolicy
}

func NewCollectionShareLinkService(
	links CollectionShareLinkRepository,
	codes CodeGenerator,
	rooms CollectionRoomReader,
	clock Clock,
) *CollectionShareLinkService {
	if clock == nil {
		clock = time.Now
	}
	return &CollectionShareLinkService{links: links, codes: codes, rooms: rooms, clock: clock}
}

func (s *CollectionShareLinkService) WithVisitorMusicPolicy(policy VisitorMusicPolicy) *CollectionShareLinkService {
	s.music = policy
	return s
}

// MARK: - Owner side

func (s *CollectionShareLinkService) EnsureLink(ctx context.Context, accountID, collectionRoomID string) (domain.CollectionShareLink, error) {
	room, err := s.rooms.OwnedCollectionRoom(ctx, accountID, collectionRoomID)
	if err != nil {
		return domain.CollectionShareLink{}, err
	}
	code, err := s.codes.NewCode()
	if err != nil {
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: mint code: %w", err)
	}
	return s.links.EnsureActive(ctx, room.ID, code, s.clock())
}

func (s *CollectionShareLinkService) CurrentLink(ctx context.Context, accountID, collectionRoomID string) (domain.CollectionShareLink, error) {
	room, err := s.rooms.OwnedCollectionRoom(ctx, accountID, collectionRoomID)
	if err != nil {
		return domain.CollectionShareLink{}, err
	}
	return s.links.FindActiveByRoom(ctx, room.ID)
}

func (s *CollectionShareLinkService) RegenerateLink(ctx context.Context, accountID, collectionRoomID string) (domain.CollectionShareLink, error) {
	room, err := s.rooms.OwnedCollectionRoom(ctx, accountID, collectionRoomID)
	if err != nil {
		return domain.CollectionShareLink{}, err
	}
	code, err := s.codes.NewCode()
	if err != nil {
		return domain.CollectionShareLink{}, fmt.Errorf("sharing: mint code: %w", err)
	}
	return s.links.Rotate(ctx, room.ID, code, s.clock())
}

func (s *CollectionShareLinkService) RevokeLink(ctx context.Context, accountID, collectionRoomID string) (bool, error) {
	room, err := s.rooms.OwnedCollectionRoom(ctx, accountID, collectionRoomID)
	if err != nil {
		return false, err
	}
	return s.links.Revoke(ctx, room.ID, s.clock())
}

// MARK: - Visitor side

func (s *CollectionShareLinkService) VisitorCollectionRoom(ctx context.Context, code domain.Code) (CollectionRoomContent, error) {
	link, err := s.resolveActive(ctx, code)
	if err != nil {
		return CollectionRoomContent{}, err
	}
	room, err := s.rooms.VisitorCollectionRoom(ctx, link.CollectionRoomID)
	if err != nil {
		if errors.Is(err, ErrContentNotVisible) {
			return CollectionRoomContent{}, domain.ErrLinkNotAvailable
		}
		return CollectionRoomContent{}, err
	}
	if !s.music.AudibleToVisitors {
		room.MusicTrackID = ""
	}
	return room, nil
}

func (s *CollectionShareLinkService) resolveActive(ctx context.Context, code domain.Code) (domain.CollectionShareLink, error) {
	if !domain.IsPlausibleCode(string(code)) {
		return domain.CollectionShareLink{}, domain.ErrLinkNotAvailable
	}
	link, err := s.links.FindByCode(ctx, code)
	if err != nil {
		if errors.Is(err, domain.ErrLinkNotAvailable) {
			return domain.CollectionShareLink{}, domain.ErrLinkNotAvailable
		}
		return domain.CollectionShareLink{}, err
	}
	if !link.IsActive() {
		return domain.CollectionShareLink{}, domain.ErrLinkNotAvailable
	}
	return link, nil
}
