package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"muse-backend/internal/sharing/domain"
)

type ShareLinkService struct {
	links    ShareLinkRepository
	codes    CodeGenerator
	museums  MuseumReader
	content  MuseumContentReader
	profiles OwnerProfileReader
	clock    Clock
	music    VisitorMusicPolicy
}

func (s *ShareLinkService) WithVisitorMusicPolicy(policy VisitorMusicPolicy) *ShareLinkService {
	s.music = policy
	return s
}

func NewShareLinkService(
	links ShareLinkRepository,
	codes CodeGenerator,
	museums MuseumReader,
	content MuseumContentReader,
	profiles OwnerProfileReader,
	clock Clock,
) *ShareLinkService {
	if clock == nil {
		clock = time.Now
	}
	return &ShareLinkService{links: links, codes: codes, museums: museums, content: content, profiles: profiles, clock: clock}
}

// MARK: - Owner side

func (s *ShareLinkService) EnsureLink(ctx context.Context, accountID string) (domain.ShareLink, error) {
	museum, err := s.museums.OwnedMuseum(ctx, accountID)
	if err != nil {
		return domain.ShareLink{}, err
	}
	code, err := s.codes.NewCode()
	if err != nil {
		return domain.ShareLink{}, fmt.Errorf("sharing: mint code: %w", err)
	}
	return s.links.EnsureActive(ctx, museum.ID, code, s.clock())
}

func (s *ShareLinkService) CurrentLink(ctx context.Context, accountID string) (domain.ShareLink, error) {
	museum, err := s.museums.OwnedMuseum(ctx, accountID)
	if err != nil {
		return domain.ShareLink{}, err
	}
	return s.links.FindActiveByMuseum(ctx, museum.ID)
}

func (s *ShareLinkService) RegenerateLink(ctx context.Context, accountID string) (domain.ShareLink, error) {
	museum, err := s.museums.OwnedMuseum(ctx, accountID)
	if err != nil {
		return domain.ShareLink{}, err
	}
	code, err := s.codes.NewCode()
	if err != nil {
		return domain.ShareLink{}, fmt.Errorf("sharing: mint code: %w", err)
	}
	return s.links.Rotate(ctx, museum.ID, code, s.clock())
}

// MARK: - Visitor side

type Preview struct {
	Code    domain.Code
	StyleID string
	Owner   OwnerProfile
}

func (s *ShareLinkService) Preview(ctx context.Context, code domain.Code) (Preview, error) {
	_, museum, err := s.resolveActivePublic(ctx, code)
	if err != nil {
		return Preview{}, err
	}
	owner, err := s.profiles.PublicProfile(ctx, museum.OwnerAccountID)
	if err != nil {
		if errors.Is(err, ErrOwnerUnavailable) {
			return Preview{}, domain.ErrLinkNotAvailable
		}
		return Preview{}, err
	}
	return Preview{Code: code, StyleID: museum.StyleID, Owner: owner}, nil
}

func (s *ShareLinkService) VisitorMuseum(ctx context.Context, code domain.Code) (MuseumContent, error) {
	_, museum, err := s.resolveActivePublic(ctx, code)
	if err != nil {
		return MuseumContent{}, err
	}
	content, err := s.content.VisitorMuseum(ctx, museum.ID)
	if err != nil {
		return MuseumContent{}, foldNotVisible(err)
	}
	return content, nil
}

func (s *ShareLinkService) VisitorRoom(ctx context.Context, code domain.Code, roomID string) (RoomContent, error) {
	_, museum, err := s.resolveActivePublic(ctx, code)
	if err != nil {
		return RoomContent{}, err
	}
	room, err := s.content.VisitorRoom(ctx, museum.ID, roomID)
	if err != nil {
		return RoomContent{}, foldNotVisible(err)
	}
	if !s.music.AudibleToVisitors {
		room.MusicTrackID = ""
	}
	return room, nil
}

func (s *ShareLinkService) VisitorRoomPhotoTickets(ctx context.Context, code domain.Code, roomID string) ([]PhotoTicket, error) {
	_, museum, err := s.resolveActivePublic(ctx, code)
	if err != nil {
		return nil, err
	}
	tickets, err := s.content.VisitorRoomPhotoTickets(ctx, museum.ID, roomID)
	if err != nil {
		return nil, foldNotVisible(err)
	}
	return tickets, nil
}

func (s *ShareLinkService) resolveActivePublic(ctx context.Context, code domain.Code) (domain.ShareLink, Museum, error) {
	if !domain.IsPlausibleCode(string(code)) {
		return domain.ShareLink{}, Museum{}, domain.ErrLinkNotAvailable
	}
	link, err := s.links.FindByCode(ctx, code)
	if err != nil {
		if errors.Is(err, domain.ErrLinkNotAvailable) {
			return domain.ShareLink{}, Museum{}, domain.ErrLinkNotAvailable
		}
		return domain.ShareLink{}, Museum{}, err
	}
	if !link.IsActive() {
		return domain.ShareLink{}, Museum{}, domain.ErrLinkNotAvailable
	}
	museum, err := s.museums.MuseumByID(ctx, link.MuseumID)
	if err != nil {
		if errors.Is(err, domain.ErrNoMuseum) {
			return domain.ShareLink{}, Museum{}, domain.ErrLinkNotAvailable
		}
		return domain.ShareLink{}, Museum{}, err
	}
	if !museum.Public {
		return domain.ShareLink{}, Museum{}, domain.ErrLinkNotAvailable
	}
	return link, museum, nil
}

func foldNotVisible(err error) error {
	if errors.Is(err, ErrContentNotVisible) {
		return domain.ErrLinkNotAvailable
	}
	return err
}
