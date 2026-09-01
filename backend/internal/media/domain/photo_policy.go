package domain

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	PhotoContentType        = "image/jpeg"
	MaxPhotoBytes     int64 = 10 << 20
	MaxPhotoLongEdge        = 3072
	MinPhotoShortEdge       = 320
)

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

type PhotoDeclaration struct {
	ClientUploadID string
	ContentType    string
	ByteSize       int64
	PixelWidth     int
	PixelHeight    int
	ChecksumSHA256 string
}

func (d PhotoDeclaration) Validate() error {
	if strings.TrimSpace(d.ClientUploadID) == "" || len(d.ClientUploadID) > 128 {
		return ErrInvalidClientUploadID
	}
	if d.ContentType != PhotoContentType {
		return ErrUnsupportedContentType
	}
	if d.ByteSize <= 0 || d.ByteSize > MaxPhotoBytes {
		return ErrPhotoTooLarge
	}
	if err := validatePhotoDimensions(d.PixelWidth, d.PixelHeight); err != nil {
		return err
	}
	if !sha256Hex.MatchString(d.ChecksumSHA256) {
		return ErrInvalidChecksum
	}
	return nil
}

func validatePhotoDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("%w: dimensions must be positive", ErrPhotoDimensions)
	}
	long, short := width, height
	if height > width {
		long, short = height, width
	}
	if long > MaxPhotoLongEdge {
		return fmt.Errorf("%w: long edge %d exceeds %d", ErrPhotoDimensions, long, MaxPhotoLongEdge)
	}
	if short < MinPhotoShortEdge {
		return fmt.Errorf("%w: short edge %d is below %d", ErrPhotoDimensions, short, MinPhotoShortEdge)
	}
	return nil
}
