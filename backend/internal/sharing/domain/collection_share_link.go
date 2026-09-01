package domain

import (
	"errors"
	"time"
)

type CollectionShareLink struct {
	ID               string
	CollectionRoomID string
	Code             Code
	Status           Status
	CreatedAt        time.Time
	RevokedAt        *time.Time
}

func (l CollectionShareLink) IsActive() bool {
	return l.Status == StatusActive
}

var (
	ErrNoCollectionRoom = errors.New("sharing: no such collection room for this account")

	ErrNoActiveCollectionLink = errors.New("sharing: collection room has no active link")
)
