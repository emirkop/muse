package application

import (
	"context"
	"fmt"
	"time"

	"muse-backend/internal/catalog/domain"
)

type MusicCatalogReading interface {
	FindMusicTrack(ctx context.Context, trackID string) (domain.MusicTrack, error)
}

type AudioURL struct {
	URL       string
	ExpiresAt time.Time
}

type AudioDelivering interface {
	PresignAudio(ctx context.Context, storageKey string, ttl time.Duration) (AudioURL, error)
}

type MusicDeliveryService struct {
	catalog      MusicCatalogReading
	audio        AudioDelivering
	ttl          time.Duration
	isProduction bool
}

func NewMusicDeliveryService(catalog MusicCatalogReading, audio AudioDelivering, ttl time.Duration, isProduction bool) *MusicDeliveryService {
	return &MusicDeliveryService{catalog: catalog, audio: audio, ttl: ttl, isProduction: isProduction}
}

func (s *MusicDeliveryService) AudioURL(ctx context.Context, trackID string) (AudioURL, error) {
	track, err := s.catalog.FindMusicTrack(ctx, trackID)
	if err != nil {
		return AudioURL{}, err
	}
	if s.isProduction && !track.IsLicensed() {
		return AudioURL{}, fmt.Errorf("%w: %s is %s", domain.ErrMusicTrackNotCleared, track.ID, track.Licensing)
	}
	if track.StorageKey == "" {
		return AudioURL{}, fmt.Errorf("%w: %s has no stored audio", domain.ErrMusicTrackNotCleared, track.ID)
	}
	return s.audio.PresignAudio(ctx, track.StorageKey, s.ttl)
}
