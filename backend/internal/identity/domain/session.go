package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type SessionID string

type FamilyID string

type Session struct {
	ID        SessionID
	AccountID AccountID
	FamilyID  FamilyID
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

func (s Session) IsRevoked() bool {
	return s.RevokedAt != nil
}

func (s Session) IsExpired(now time.Time) bool {
	return now.After(s.ExpiresAt)
}

func (s Session) IsActive(now time.Time) bool {
	return !s.IsRevoked() && !s.IsExpired(now)
}

type AccessTokenClaims struct {
	AccountID AccountID
	SessionID SessionID
	ExpiresAt time.Time
}

type RefreshToken struct {
	SessionID SessionID
	FamilyID  FamilyID
	Digest    string
	IssuedAt  time.Time
	ExpiresAt time.Time
	RotatedAt *time.Time
}

func (r RefreshToken) IsExpired(now time.Time) bool {
	return now.After(r.ExpiresAt)
}

func (r RefreshToken) WasRotated() bool {
	return r.RotatedAt != nil
}

func DigestRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
