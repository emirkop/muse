package domain

import (
	"fmt"
	"time"
)

type Asset struct {
	ID             AssetID
	AccountID      string
	Category       AssetCategory
	StorageKey     string
	ContentType    string
	ByteSize       int64
	PixelWidth     int
	PixelHeight    int
	ChecksumSHA256 string
	State          AssetState
	ClientUploadID string
	CreatedAt      time.Time
	CommittedAt    *time.Time
	ReleasedAt     *time.Time
	DiscardedAt    *time.Time
}

type AssetID string

type AssetCategory string

const CategoryRoomPhoto AssetCategory = "room_photo"

type AssetState string

const (
	StatePending   AssetState = "pending"
	StateCommitted AssetState = "committed"
	StateReleased  AssetState = "released"
	StateDiscarded AssetState = "discarded"
)

func PhotoStorageKey(accountID string, id AssetID) string {
	return fmt.Sprintf("photos/%s/%s", accountID, id)
}

type StoredObject struct {
	ByteSize       int64
	ContentType    string
	Format         string
	PixelWidth     int
	PixelHeight    int
	ChecksumSHA256 string
}

func (a Asset) VerifyStored(obj StoredObject) error {
	switch {
	case obj.ByteSize != a.ByteSize:
		return invalid("stored size %d does not match declared %d", obj.ByteSize, a.ByteSize)
	case obj.ContentType != "" && obj.ContentType != a.ContentType:
		return invalid("stored content type %q does not match declared %q", obj.ContentType, a.ContentType)
	case obj.Format != "jpeg":
		return invalid("stored bytes are %q, not a JPEG", obj.Format)
	case obj.PixelWidth != a.PixelWidth || obj.PixelHeight != a.PixelHeight:
		return invalid("stored dimensions %dx%d do not match declared %dx%d",
			obj.PixelWidth, obj.PixelHeight, a.PixelWidth, a.PixelHeight)
	case obj.ChecksumSHA256 != a.ChecksumSHA256:
		return invalid("stored checksum does not match declared checksum")
	}
	if err := validatePhotoDimensions(obj.PixelWidth, obj.PixelHeight); err != nil {
		return invalid("%v", err)
	}
	if obj.ByteSize > MaxPhotoBytes {
		return invalid("stored size %d exceeds the %d-byte limit", obj.ByteSize, MaxPhotoBytes)
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrAssetInvalid, fmt.Sprintf(format, args...))
}
