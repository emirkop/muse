package application

import (
	"context"
	"io"

	"muse-backend/internal/catalog/domain"
)

type BundleRepository interface {
	ResolveForApp(ctx context.Context, bundleID string, appAssetVersion int) (domain.AssetBundle, error)

	FindVersion(ctx context.Context, bundleID string, version int) (domain.AssetBundle, error)

	Register(ctx context.Context, bundle domain.AssetBundle) error

	RegisterTierCapacities(ctx context.Context, bundleID string, version int, capacities domain.TierCapacities) error

	DesignTierCountsNaming(ctx context.Context, bundleID string) ([]int, error)
}

type BundleObjectStore interface {
	Put(ctx context.Context, key, contentType string, body io.Reader, size int64, checksumSHA256 string) error

	Stat(ctx context.Context, key string) (StoredObject, error)

	PublicURL(key string) string
}

type StoredObject struct {
	ByteSize       int64
	ChecksumSHA256 string
}
