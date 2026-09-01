package domain

import "errors"

var (
	ErrLinkNotAvailable = errors.New("sharing: link not available")

	ErrNoMuseum = errors.New("sharing: account has no museum")

	ErrNoActiveLink = errors.New("sharing: no active link")
)
