package domain

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

type EmailAddress string

func (e EmailAddress) String() string { return string(e) }

const (
	PasswordMinimumLength = 10
	PasswordMaximumLength = 128
	EmailMaximumLength    = 254
)

var (
	ErrInvalidEmail = errors.New("identity: email address is not valid")
	ErrWeakPassword = errors.New("identity: password does not meet the minimum policy")

	ErrCredentialNotFound = errors.New("identity: no password credential for this address")
	ErrInvalidPassword    = errors.New("identity: password does not match")

	ErrEmailAlreadyRegistered = errors.New("identity: a password credential already exists for this address")

	ErrVerificationTokenInvalid = errors.New("identity: verification token is not valid")
	ErrResetTokenInvalid        = errors.New("identity: password reset token is not valid")

	ErrTooManyAttempts = errors.New("identity: too many attempts")
)

func NormaliseEmail(raw string) (EmailAddress, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > EmailMaximumLength {
		return "", ErrInvalidEmail
	}
	lowered := strings.ToLower(trimmed)

	at := strings.IndexByte(lowered, '@')
	if at <= 0 || at != strings.LastIndexByte(lowered, '@') {
		return "", ErrInvalidEmail
	}
	local, domain := lowered[:at], lowered[at+1:]
	if local == "" || domain == "" {
		return "", ErrInvalidEmail
	}
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", ErrInvalidEmail
	}
	for _, r := range lowered {
		if unicode.IsSpace(r) || r == ',' || r == ';' {
			return "", ErrInvalidEmail
		}
	}
	return EmailAddress(lowered), nil
}

func ValidatePassword(raw string) error {
	length := len([]rune(raw))
	if length < PasswordMinimumLength || length > PasswordMaximumLength {
		return ErrWeakPassword
	}
	return nil
}

type PasswordCredential struct {
	AccountID AccountID
	Email     EmailAddress
	Hash      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type PendingSignup struct {
	ID           string
	Email        EmailAddress
	PasswordHash string
	TokenDigest  string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	ConsumedAt   *time.Time
}

func (p PendingSignup) IsUsable(now time.Time) bool {
	return p.ConsumedAt == nil && now.Before(p.ExpiresAt)
}

type PasswordReset struct {
	ID          string
	AccountID   AccountID
	TokenDigest string
	ExpiresAt   time.Time
	CreatedAt   time.Time
	ConsumedAt  *time.Time
}

func (p PasswordReset) IsUsable(now time.Time) bool {
	return p.ConsumedAt == nil && now.Before(p.ExpiresAt)
}

func DigestOpaqueToken(raw string) string {
	return DigestRefreshToken(raw)
}
