package main

import (
	"context"
	"errors"

	collectionapp "muse-backend/internal/collection/application"
	collectiondomain "muse-backend/internal/collection/domain"
	identityapp "muse-backend/internal/identity/application"
	identitydomain "muse-backend/internal/identity/domain"
	museumapp "muse-backend/internal/museum/application"
	museumdomain "muse-backend/internal/museum/domain"
	sharingapp "muse-backend/internal/sharing/application"
	sharingdomain "muse-backend/internal/sharing/domain"
)

type museumForSharing struct {
	museums *museumapp.MuseumService
}

var _ sharingapp.MuseumReader = museumForSharing{}
var _ sharingapp.MuseumContentReader = museumForSharing{}

func (a museumForSharing) OwnedMuseum(ctx context.Context, accountID string) (sharingapp.Museum, error) {
	museum, err := a.museums.FindMuseum(ctx, accountID)
	if err != nil {
		return sharingapp.Museum{}, translateMuseumLookupError(err)
	}
	return toSharingMuseum(museum), nil
}

func (a museumForSharing) MuseumByID(ctx context.Context, museumID string) (sharingapp.Museum, error) {
	museum, err := a.museums.MuseumByID(ctx, museumdomain.MuseumID(museumID))
	if err != nil {
		return sharingapp.Museum{}, translateMuseumLookupError(err)
	}
	return toSharingMuseum(museum), nil
}

func (a museumForSharing) VisitorMuseum(ctx context.Context, museumID string) (sharingapp.MuseumContent, error) {
	museum, rooms, err := a.museums.VisitorMuseum(ctx, museumdomain.MuseumID(museumID))
	if err != nil {
		return sharingapp.MuseumContent{}, translateVisibilityError(err)
	}
	summaries := make([]sharingapp.RoomSummary, 0, len(rooms))
	for _, room := range rooms {
		summaries = append(summaries, sharingapp.RoomSummary{ID: string(room.ID), Name: room.Name, VariantID: room.VariantID})
	}
	return sharingapp.MuseumContent{ID: string(museum.ID), StyleID: museum.StyleID, Rooms: summaries}, nil
}

func (a museumForSharing) VisitorRoom(ctx context.Context, museumID, roomID string) (sharingapp.RoomContent, error) {
	room, err := a.museums.VisitorRoom(ctx, museumdomain.MuseumID(museumID), museumdomain.RoomID(roomID))
	if err != nil {
		return sharingapp.RoomContent{}, translateVisibilityError(err)
	}
	slots := make([]sharingapp.PhotoSlot, 0, len(room.PhotoSlots))
	for _, slot := range room.PhotoSlots {
		slots = append(slots, sharingapp.PhotoSlot{SlotIndex: slot.SlotIndex, PhotoAssetID: slot.PhotoAssetID, Caption: slot.Caption})
	}
	sculptures := make([]sharingapp.Sculpture, 0, len(room.Sculptures))
	for _, sculpture := range room.Sculptures {
		sculptures = append(sculptures, sharingapp.Sculpture{SlotIndex: sculpture.SlotIndex, CatalogID: sculpture.CatalogID})
	}
	return sharingapp.RoomContent{
		ID: string(room.ID), Name: room.Name, VariantID: room.VariantID,
		MusicTrackID: room.MusicTrackID,
		PhotoSlots:   slots, Sculptures: sculptures,
	}, nil
}

func (a museumForSharing) VisitorRoomPhotoTickets(ctx context.Context, museumID, roomID string) ([]sharingapp.PhotoTicket, error) {
	tickets, err := a.museums.VisitorPhotoDownloadTickets(ctx, museumdomain.MuseumID(museumID), museumdomain.RoomID(roomID))
	if err != nil {
		return nil, translateVisibilityError(err)
	}
	out := make([]sharingapp.PhotoTicket, 0, len(tickets))
	for _, t := range tickets {
		out = append(out, sharingapp.PhotoTicket{
			PhotoAssetID: t.PhotoAssetID,
			URL:          t.URL,
			ExpiresAt:    t.ExpiresAt,
			PixelWidth:   t.PixelWidth,
			PixelHeight:  t.PixelHeight,
		})
	}
	return out, nil
}

func toSharingMuseum(m museumdomain.Museum) sharingapp.Museum {
	return sharingapp.Museum{
		ID:             string(m.ID),
		OwnerAccountID: m.AccountID,
		StyleID:        m.StyleID,
		Public:         museumdomain.VisitorCanSeeMuseum(m),
	}
}

func translateMuseumLookupError(err error) error {
	if errors.Is(err, museumdomain.ErrMuseumNotFound) {
		return sharingdomain.ErrNoMuseum
	}
	return err
}

func translateVisibilityError(err error) error {
	switch {
	case errors.Is(err, museumdomain.ErrNotVisible):
		return sharingapp.ErrContentNotVisible
	case errors.Is(err, museumdomain.ErrPhotosUnavailable):
		return sharingapp.ErrPhotosUnavailable
	default:
		return err
	}
}

type identityForSharing struct {
	accounts *identityapp.AccountService
}

var _ sharingapp.OwnerProfileReader = identityForSharing{}

func (a identityForSharing) PublicProfile(ctx context.Context, accountID string) (sharingapp.OwnerProfile, error) {
	account, err := a.accounts.FindByID(ctx, identitydomain.AccountID(accountID))
	if err != nil {
		if errors.Is(err, identitydomain.ErrAccountNotFound) || errors.Is(err, identitydomain.ErrAccountDeactivated) {
			return sharingapp.OwnerProfile{}, sharingapp.ErrOwnerUnavailable
		}
		return sharingapp.OwnerProfile{}, err
	}
	if account.IsDeleted() {
		return sharingapp.OwnerProfile{}, sharingapp.ErrOwnerUnavailable
	}
	return sharingapp.OwnerProfile{AvatarID: string(account.AvatarID)}, nil
}

type collectionForSharing struct {
	rooms *collectionapp.CollectionRoomService
}

var _ sharingapp.CollectionRoomReader = collectionForSharing{}

func (a collectionForSharing) OwnedCollectionRoom(ctx context.Context, accountID, collectionRoomID string) (sharingapp.CollectionRoomRef, error) {
	room, err := a.rooms.Find(ctx, accountID, collectiondomain.CollectionRoomID(collectionRoomID))
	if err != nil {
		if errors.Is(err, collectiondomain.ErrCollectionRoomNotFound) || errors.Is(err, collectiondomain.ErrNotOwner) {
			return sharingapp.CollectionRoomRef{}, sharingdomain.ErrNoCollectionRoom
		}
		return sharingapp.CollectionRoomRef{}, err
	}
	return sharingapp.CollectionRoomRef{ID: string(room.ID), OwnerAccountID: room.AccountID}, nil
}

func (a collectionForSharing) VisitorCollectionRoom(ctx context.Context, collectionRoomID string) (sharingapp.CollectionRoomContent, error) {
	room, err := a.rooms.VisitorRoom(ctx, collectiondomain.CollectionRoomID(collectionRoomID))
	if err != nil {
		if errors.Is(err, collectiondomain.ErrCollectionRoomNotFound) {
			return sharingapp.CollectionRoomContent{}, sharingapp.ErrContentNotVisible
		}
		return sharingapp.CollectionRoomContent{}, err
	}
	items := make([]sharingapp.CollectionItemRef, 0, len(room.Items))
	for _, item := range room.Items {
		items = append(items, sharingapp.CollectionItemRef{
			ID: string(item.ID), SlotIndex: item.SlotIndex, CatalogModelID: item.CatalogModelID,
		})
	}
	return sharingapp.CollectionRoomContent{
		ID:           string(room.ID),
		Name:         room.Name,
		CategoryID:   room.CategoryID,
		DesignID:     room.DesignID,
		CurrentTier:  int(room.CurrentTier),
		MusicTrackID: room.MusicTrackID,
		Items:        items,
	}, nil
}
