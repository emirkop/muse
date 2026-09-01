package application

import (
	"context"
	"time"

	"muse-backend/internal/sharing/domain"
)

type CollectionShareLinkRepository interface {
	FindActiveByRoom(ctx context.Context, collectionRoomID string) (domain.CollectionShareLink, error)
	FindByCode(ctx context.Context, code domain.Code) (domain.CollectionShareLink, error)
	EnsureActive(ctx context.Context, collectionRoomID string, code domain.Code, now time.Time) (domain.CollectionShareLink, error)
	Rotate(ctx context.Context, collectionRoomID string, code domain.Code, now time.Time) (domain.CollectionShareLink, error)
	Revoke(ctx context.Context, collectionRoomID string, now time.Time) (revoked bool, err error)
}

type CollectionRoomRef struct {
	ID             string
	OwnerAccountID string
}

type CollectionRoomContent struct {
	ID           string
	Name         string
	CategoryID   string
	DesignID     string
	CurrentTier  int
	MusicTrackID string
	Items        []CollectionItemRef
}

type CollectionItemRef struct {
	ID             string
	SlotIndex      int
	CatalogModelID string
}

type CollectionRoomReader interface {
	OwnedCollectionRoom(ctx context.Context, accountID, collectionRoomID string) (CollectionRoomRef, error)
	VisitorCollectionRoom(ctx context.Context, collectionRoomID string) (CollectionRoomContent, error)
}
