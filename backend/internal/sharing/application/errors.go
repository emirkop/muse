package application

import "errors"

var (
	ErrContentNotVisible = errors.New("sharing: content not visible to visitors")

	ErrOwnerUnavailable = errors.New("sharing: owner profile unavailable")

	ErrPhotosUnavailable = errors.New("sharing: photo delivery unavailable")
)
