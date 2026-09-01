package domain

import "time"

type Account struct {
	ID          AccountID
	DisplayName string
	AvatarID    AvatarID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

func (a Account) IsDeleted() bool {
	return a.DeletedAt != nil
}

type LinkedIdentity struct {
	ID        string
	AccountID AccountID
	Provider  Provider
	Subject   string
	CreatedAt time.Time
}
