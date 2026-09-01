package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"time"

	_ "image/jpeg"

	"muse-backend/internal/media/domain"
	"muse-backend/internal/platform/observability"
)

const (
	headerProbeBytes         int64 = 64 << 10
	headerProbeFallbackBytes int64 = 1 << 20
)

type MediaService struct {
	assets      AssetRepository
	storage     ObjectStorage
	uploadTTL   time.Duration
	downloadTTL time.Duration
	now         func() time.Time
	logger      *slog.Logger
}

func NewMediaService(
	assets AssetRepository,
	storage ObjectStorage,
	uploadTTL, downloadTTL time.Duration,
	logger *slog.Logger,
) *MediaService {
	return &MediaService{
		assets:      assets,
		storage:     storage,
		uploadTTL:   uploadTTL,
		downloadTTL: downloadTTL,
		now:         time.Now,
		logger:      logger,
	}
}

type PhotoUploadTicket struct {
	Asset   domain.Asset
	Upload  *UploadTicket
	Created bool
}

func (s *MediaService) InitiatePhotoUpload(
	ctx context.Context,
	accountID string,
	decl domain.PhotoDeclaration,
) (PhotoUploadTicket, error) {
	if err := decl.Validate(); err != nil {
		return PhotoUploadTicket{}, err
	}

	candidate := domain.Asset{
		AccountID:      accountID,
		Category:       domain.CategoryRoomPhoto,
		ContentType:    decl.ContentType,
		ByteSize:       decl.ByteSize,
		PixelWidth:     decl.PixelWidth,
		PixelHeight:    decl.PixelHeight,
		ChecksumSHA256: decl.ChecksumSHA256,
		State:          domain.StatePending,
		ClientUploadID: decl.ClientUploadID,
		CreatedAt:      s.now(),
	}

	asset, created, err := s.assets.CreatePending(ctx, candidate)
	if err != nil {
		return PhotoUploadTicket{}, fmt.Errorf("media: create pending asset: %w", err)
	}

	if !created {
		if !sameDeclaration(asset, decl) {
			return PhotoUploadTicket{}, domain.ErrDeclarationMismatch
		}
		switch asset.State {
		case domain.StateCommitted:
			return PhotoUploadTicket{Asset: asset, Created: false}, nil
		case domain.StateReleased:
			return PhotoUploadTicket{}, domain.ErrAssetDiscarded
		case domain.StateDiscarded:
			return PhotoUploadTicket{}, domain.ErrAssetDiscarded
		}
	}

	ticket, err := s.storage.PresignUpload(ctx, PresignUploadRequest{
		Key:            asset.StorageKey,
		ContentType:    asset.ContentType,
		ByteSize:       asset.ByteSize,
		ChecksumSHA256: asset.ChecksumSHA256,
		TTL:            s.uploadTTL,
	})
	if err != nil {
		return PhotoUploadTicket{}, fmt.Errorf("media: presign upload: %w", err)
	}
	return PhotoUploadTicket{Asset: asset, Upload: &ticket, Created: created}, nil
}

func sameDeclaration(asset domain.Asset, decl domain.PhotoDeclaration) bool {
	return asset.ContentType == decl.ContentType &&
		asset.ByteSize == decl.ByteSize &&
		asset.PixelWidth == decl.PixelWidth &&
		asset.PixelHeight == decl.PixelHeight &&
		asset.ChecksumSHA256 == decl.ChecksumSHA256
}

func (s *MediaService) VerifyPhotoAssets(ctx context.Context, accountID string, ids []string) error {
	assets, err := s.loadOwned(ctx, accountID, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		asset := assets[domain.AssetID(id)]
		switch asset.State {
		case domain.StateCommitted:
			continue
		case domain.StateReleased, domain.StateDiscarded:
			return &domain.AssetError{AssetID: asset.ID, Err: domain.ErrAssetDiscarded}
		}
		if err := s.verifyStoredObject(ctx, asset); err != nil {
			return &domain.AssetError{AssetID: asset.ID, Err: err}
		}
	}
	return nil
}

func (s *MediaService) ReleasePhotoAssets(ctx context.Context, ids []string) error {
	assetIDs := make([]domain.AssetID, 0, len(ids))
	for _, id := range ids {
		assetIDs = append(assetIDs, domain.AssetID(id))
	}
	changed, err := s.assets.MarkReleased(ctx, assetIDs, s.now())
	if err != nil {
		return fmt.Errorf("media: release assets: %w", err)
	}
	if changed != int64(len(assetIDs)) {
		return fmt.Errorf("%w: expected %d committed assets, %d changed state", domain.ErrAssetNotCommitted, len(assetIDs), changed)
	}
	return nil
}

func (s *MediaService) CommitPhotoAssets(ctx context.Context, ids []string) error {
	assetIDs := make([]domain.AssetID, 0, len(ids))
	for _, id := range ids {
		assetIDs = append(assetIDs, domain.AssetID(id))
	}
	changed, err := s.assets.MarkCommitted(ctx, assetIDs, s.now())
	if err != nil {
		return fmt.Errorf("media: commit assets: %w", err)
	}
	if changed != int64(len(assetIDs)) {
		return fmt.Errorf("%w: expected %d pending assets, %d changed state", domain.ErrAssetNotPending, len(assetIDs), changed)
	}
	return nil
}

type PhotoDownloadTicket struct {
	AssetID     domain.AssetID
	URL         string
	ExpiresAt   time.Time
	PixelWidth  int
	PixelHeight int
}

func (s *MediaService) IssuePhotoDownloadTickets(
	ctx context.Context,
	accountID string,
	ids []string,
) ([]PhotoDownloadTicket, error) {
	assets, err := s.loadOwned(ctx, accountID, ids)
	if err != nil {
		return nil, err
	}

	tickets := make([]PhotoDownloadTicket, 0, len(ids))
	for _, id := range ids {
		asset := assets[domain.AssetID(id)]
		if asset.State != domain.StateCommitted {
			return nil, &domain.AssetError{AssetID: asset.ID, Err: domain.ErrAssetNotFound}
		}
		download, err := s.storage.PresignDownload(ctx, asset.StorageKey, s.downloadTTL)
		if err != nil {
			return nil, fmt.Errorf("media: presign download: %w", err)
		}
		tickets = append(tickets, PhotoDownloadTicket{
			AssetID:     asset.ID,
			URL:         download.URL,
			ExpiresAt:   download.ExpiresAt,
			PixelWidth:  asset.PixelWidth,
			PixelHeight: asset.PixelHeight,
		})
	}
	return tickets, nil
}

func (s *MediaService) ReclaimAbandonedUploads(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	cutoff := s.now().Add(-olderThan)
	stale, err := s.assets.ListPendingOlderThan(ctx, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("media: list abandoned uploads: %w", err)
	}
	return s.reclaim(ctx, stale, "abandoned"), nil
}

func (s *MediaService) ReclaimReleasedAssets(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	cutoff := s.now().Add(-olderThan)
	released, err := s.assets.ListReleasedOlderThan(ctx, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("media: list released assets: %w", err)
	}
	return s.reclaim(ctx, released, "released"), nil
}

func (s *MediaService) reclaim(ctx context.Context, assets []domain.Asset, kind string) int {
	reclaimed := 0
	for _, asset := range assets {
		if err := s.storage.Delete(ctx, asset.StorageKey); err != nil {
			observability.Log(ctx, s.logger, observability.Event{
				Name:     observability.EventMediaReclaimFailed,
				Category: observability.CategoryMedia,
				Outcome:  observability.OutcomeFailed,
				Reason:   "delete_object:" + kind,
				Err:      err,
			})
			continue
		}
		if err := s.assets.MarkDiscarded(ctx, asset.ID, s.now()); err != nil {
			observability.Log(ctx, s.logger, observability.Event{
				Name:     observability.EventMediaReclaimFailed,
				Category: observability.CategoryMedia,
				Outcome:  observability.OutcomeFailed,
				Reason:   "tombstone:" + kind,
				Err:      err,
			})
			continue
		}
		reclaimed++
	}
	return reclaimed
}

// MARK: - Internals

func (s *MediaService) loadOwned(ctx context.Context, accountID string, ids []string) (map[domain.AssetID]domain.Asset, error) {
	assetIDs := make([]domain.AssetID, 0, len(ids))
	for _, id := range ids {
		assetIDs = append(assetIDs, domain.AssetID(id))
	}
	found, err := s.assets.FindOwnedByIDs(ctx, accountID, assetIDs)
	if err != nil {
		return nil, fmt.Errorf("media: load assets: %w", err)
	}
	byID := make(map[domain.AssetID]domain.Asset, len(found))
	for _, asset := range found {
		byID[asset.ID] = asset
	}
	for _, id := range assetIDs {
		if _, ok := byID[id]; !ok {
			return nil, &domain.AssetError{AssetID: id, Err: domain.ErrAssetNotFound}
		}
	}
	return byID, nil
}

func (s *MediaService) verifyStoredObject(ctx context.Context, asset domain.Asset) error {
	stat, err := s.storage.Stat(ctx, asset.StorageKey)
	if errors.Is(err, ErrObjectNotFound) {
		return domain.ErrAssetNotUploaded
	}
	if err != nil {
		return fmt.Errorf("media: stat object: %w", err)
	}

	config, format, err := s.probeImage(ctx, asset.StorageKey)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrAssetInvalid, err)
	}

	checksum := stat.ChecksumSHA256
	if checksum == "" {
		checksum, err = s.hashObject(ctx, asset.StorageKey)
		if err != nil {
			return fmt.Errorf("media: hash object: %w", err)
		}
	}

	return asset.VerifyStored(domain.StoredObject{
		ByteSize:       stat.ByteSize,
		ContentType:    stat.ContentType,
		Format:         format,
		PixelWidth:     config.Width,
		PixelHeight:    config.Height,
		ChecksumSHA256: checksum,
	})
}

func (s *MediaService) probeImage(ctx context.Context, key string) (image.Config, string, error) {
	head, err := s.storage.ReadRange(ctx, key, 0, headerProbeBytes)
	if err != nil {
		return image.Config{}, "", fmt.Errorf("read header: %w", err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(head))
	if err == nil {
		return config, format, nil
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return image.Config{}, "", fmt.Errorf("decode header: %w", err)
	}

	head, err = s.storage.ReadRange(ctx, key, 0, headerProbeFallbackBytes)
	if err != nil {
		return image.Config{}, "", fmt.Errorf("read header: %w", err)
	}
	config, format, err = image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		return image.Config{}, "", fmt.Errorf("decode header: %w", err)
	}
	return config, format, nil
}

func (s *MediaService) hashObject(ctx context.Context, key string) (string, error) {
	body, err := s.storage.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer body.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, body); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
