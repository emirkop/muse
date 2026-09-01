package domain

import "errors"

var (
	ErrMusicTrackNotFound = errors.New("catalog: music track not found")

	ErrMusicTrackNotCleared = errors.New("catalog: music track is not cleared for this deployment")

	ErrDesignCategoryRequired = errors.New("catalog: a collection category is required to list applicable designs")

	ErrDesignUnknownCategory = errors.New("catalog: collection category is not in the catalog")

	ErrSearchCategoryRequired = errors.New("catalog: a collection category is required to search models")

	ErrSearchUnknownCategory = errors.New("catalog: collection category is not in the catalog")

	ErrPresentationAssetIDsRequired = errors.New("catalog: at least one model id is required")

	ErrPresentationAssetTooManyIDs = errors.New("catalog: too many model ids in one request")
)
