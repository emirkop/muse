package application

import (
	"context"
	"errors"
	"io"
	"time"

	"muse-backend/internal/media/domain"
)

type AssetRepository interface {
	CreatePending(ctx context.Context, asset domain.Asset) (result domain.Asset, created bool, err error)

	FindOwnedByIDs(ctx context.Context, accountID string, ids []domain.AssetID) ([]domain.Asset, error)

	MarkCommitted(ctx context.Context, ids []domain.AssetID, at time.Time) (int64, error)

	MarkReleased(ctx context.Context, ids []domain.AssetID, at time.Time) (int64, error)

	MarkDiscarded(ctx context.Context, id domain.AssetID, at time.Time) error

	ListPendingOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]domain.Asset, error)

	ListReleasedOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]domain.Asset, error)
}

type ObjectStorage interface {
	PresignUpload(ctx context.Context, req PresignUploadRequest) (UploadTicket, error)

	Stat(ctx context.Context, key string) (ObjectStat, error)

	ReadRange(ctx context.Context, key string, offset, length int64) ([]byte, error)

	Open(ctx context.Context, key string) (io.ReadCloser, error)

	PresignDownload(ctx context.Context, key string, ttl time.Duration) (DownloadTicket, error)

	Delete(ctx context.Context, key string) error
}

var ErrObjectNotFound = errors.New("media: no object at key")

type PresignUploadRequest struct {
	Key            string
	ContentType    string
	ByteSize       int64
	ChecksumSHA256 string
	TTL            time.Duration
}

type UploadTicket struct {
	URL       string
	Method    string
	Headers   map[string]string
	ExpiresAt time.Time
}

type ObjectStat struct {
	ByteSize       int64
	ContentType    string
	ChecksumSHA256 string
}

type DownloadTicket struct {
	URL       string
	ExpiresAt time.Time
}
