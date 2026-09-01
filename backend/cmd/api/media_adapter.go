package main

import (
	"context"
	"errors"

	mediaapp "muse-backend/internal/media/application"
	mediadomain "muse-backend/internal/media/domain"
	museumapp "muse-backend/internal/museum/application"
	museumdomain "muse-backend/internal/museum/domain"
)

type mediaForMuseum struct {
	media *mediaapp.MediaService
}

var _ museumapp.PhotoAssetCommitting = mediaForMuseum{}
var _ museumapp.PhotoDeliveryTicketing = mediaForMuseum{}

func (a mediaForMuseum) VerifyPhotoAssets(ctx context.Context, accountID string, assetIDs []string) error {
	return translateMediaError(a.media.VerifyPhotoAssets(ctx, accountID, assetIDs))
}

func (a mediaForMuseum) CommitPhotoAssets(ctx context.Context, assetIDs []string) error {
	return translateMediaError(a.media.CommitPhotoAssets(ctx, assetIDs))
}

func (a mediaForMuseum) ReleasePhotoAssets(ctx context.Context, assetIDs []string) error {
	return translateMediaError(a.media.ReleasePhotoAssets(ctx, assetIDs))
}

func (a mediaForMuseum) IssuePhotoDownloadTickets(ctx context.Context, accountID string, assetIDs []string) ([]museumapp.PhotoDownloadTicket, error) {
	tickets, err := a.media.IssuePhotoDownloadTickets(ctx, accountID, assetIDs)
	if err != nil {
		return nil, translateMediaError(err)
	}
	out := make([]museumapp.PhotoDownloadTicket, 0, len(tickets))
	for _, t := range tickets {
		out = append(out, museumapp.PhotoDownloadTicket{
			PhotoAssetID: string(t.AssetID),
			URL:          t.URL,
			ExpiresAt:    t.ExpiresAt,
			PixelWidth:   t.PixelWidth,
			PixelHeight:  t.PixelHeight,
		})
	}
	return out, nil
}

func translateMediaError(err error) error {
	if err == nil {
		return nil
	}

	var target error
	switch {
	case errors.Is(err, mediadomain.ErrAssetNotFound):
		target = museumdomain.ErrPhotoAssetNotFound
	case errors.Is(err, mediadomain.ErrAssetNotUploaded):
		target = museumdomain.ErrPhotoAssetNotUploaded
	case errors.Is(err, mediadomain.ErrAssetInvalid):
		target = museumdomain.ErrPhotoAssetInvalid
	case errors.Is(err, mediadomain.ErrAssetDiscarded):
		target = museumdomain.ErrPhotoAssetDiscarded
	case errors.Is(err, mediadomain.ErrAssetNotPending):
		target = museumdomain.ErrPhotoAssetNotFound
	default:
		return err
	}

	var assetErr *mediadomain.AssetError
	if errors.As(err, &assetErr) {
		return &museumdomain.PhotoAssetError{AssetID: string(assetErr.AssetID), Err: target}
	}
	return target
}
