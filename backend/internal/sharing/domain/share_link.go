package domain

import "time"

type Code string

type Status string

const (
	StatusActive  Status = "active"
	StatusRevoked Status = "revoked"
)

type ShareLink struct {
	ID        string
	MuseumID  string
	Code      Code
	Status    Status
	CreatedAt time.Time
	RevokedAt *time.Time
}

func (l ShareLink) IsActive() bool {
	return l.Status == StatusActive
}

const codeLength = 22

func IsPlausibleCode(code string) bool {
	if len(code) != codeLength {
		return false
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
