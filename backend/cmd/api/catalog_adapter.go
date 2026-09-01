package main

import (
	"context"
	"time"

	catalogapp "muse-backend/internal/catalog/application"
	mediaapp "muse-backend/internal/media/application"
)

type catalogAudio struct {
	storage mediaapp.ObjectStorage
}

var _ catalogapp.AudioDelivering = catalogAudio{}

func (a catalogAudio) PresignAudio(ctx context.Context, storageKey string, ttl time.Duration) (catalogapp.AudioURL, error) {
	download, err := a.storage.PresignDownload(ctx, storageKey, ttl)
	if err != nil {
		return catalogapp.AudioURL{}, err
	}
	return catalogapp.AudioURL{URL: download.URL, ExpiresAt: download.ExpiresAt}, nil
}
