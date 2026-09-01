package application

import "context"

type EntitlementProviding interface {
	MayAddCollectionItem(ctx context.Context, accountID string, collectionRoomID string) (bool, error)
}
