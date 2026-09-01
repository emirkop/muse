package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"muse-backend/internal/identity/domain"
)

func TestNormaliseEmail_TrimsAndLowercases(t *testing.T) {
	got, err := domain.NormaliseEmail("  Emir.Test@Example.COM \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "emir.test@example.com" {
		t.Fatalf("got %q, want %q", got, "emir.test@example.com")
	}
}

func TestNormaliseEmail_PreservesDotsAndPlusTags(t *testing.T) {
	got, err := domain.NormaliseEmail("a.b+muse@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "a.b+muse@example.com" {
		t.Fatalf("normalisation must not rewrite the local part: got %q", got)
	}
}

func TestNormaliseEmail_RejectsUnusableAddresses(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"whitespace only": "   ",
		"no at":           "nobody.example.com",
		"two ats":         "a@b@example.com",
		"empty local":     "@example.com",
		"empty domain":    "someone@",
		"domain no dot":   "someone@localhost",
		"leading dot":     "someone@.example.com",
		"trailing dot":    "someone@example.com.",
		"internal space":  "some one@example.com",
		"comma":           "a,b@example.com",
		"semicolon":       "a;b@example.com",
		"over max length": strings.Repeat("a", 250) + "@example.com",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := domain.NormaliseEmail(input); !errors.Is(err, domain.ErrInvalidEmail) {
				t.Fatalf("expected ErrInvalidEmail for %q, got %v", input, err)
			}
		})
	}
}

func TestValidatePassword_LengthBounds(t *testing.T) {
	tooShort := strings.Repeat("a", domain.PasswordMinimumLength-1)
	atMinimum := strings.Repeat("a", domain.PasswordMinimumLength)
	atMaximum := strings.Repeat("a", domain.PasswordMaximumLength)
	tooLong := strings.Repeat("a", domain.PasswordMaximumLength+1)

	if err := domain.ValidatePassword(tooShort); !errors.Is(err, domain.ErrWeakPassword) {
		t.Fatalf("a password one character under the minimum must be refused, got %v", err)
	}
	if err := domain.ValidatePassword(atMinimum); err != nil {
		t.Fatalf("the minimum length must be accepted, got %v", err)
	}
	if err := domain.ValidatePassword(atMaximum); err != nil {
		t.Fatalf("the maximum length must be accepted, got %v", err)
	}
	if err := domain.ValidatePassword(tooLong); !errors.Is(err, domain.ErrWeakPassword) {
		t.Fatalf("an over-long password must be refused (unbounded input is a cheap way to make the server do expensive work), got %v", err)
	}
}

func TestPasswordMinimum_IsAtOrAboveGuidanceFloor(t *testing.T) {
	const guidanceFloor = 8
	if domain.PasswordMinimumLength < guidanceFloor {
		t.Fatalf("PasswordMinimumLength is %d, below the %d-character floor current guidance sets",
			domain.PasswordMinimumLength, guidanceFloor)
	}
}

func TestValidatePassword_CountsRunesNotBytes(t *testing.T) {
	passphrase := strings.Repeat("ş", domain.PasswordMinimumLength)
	if len(passphrase) <= domain.PasswordMinimumLength {
		t.Fatalf("test precondition: expected the passphrase to be longer in bytes than in runes")
	}
	if err := domain.ValidatePassword(passphrase); err != nil {
		t.Fatalf("a 10-character non-Latin passphrase must be accepted, got %v", err)
	}
}

func TestValidatePassword_ImposesNoCompositionRules(t *testing.T) {
	for _, password := range []string{
		"aaaaaaaaaaaa",
		"correct horse battery staple",
		"0123456789",
		"~~~~~~~~~~~~",
	} {
		if err := domain.ValidatePassword(password); err != nil {
			t.Fatalf("policy is length-only; %q must be accepted, got %v", password, err)
		}
	}
}

func TestPendingSignup_IsUsable(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	consumed := now.Add(-time.Minute)

	live := domain.PendingSignup{ExpiresAt: now.Add(time.Hour)}
	expired := domain.PendingSignup{ExpiresAt: now.Add(-time.Second)}
	used := domain.PendingSignup{ExpiresAt: now.Add(time.Hour), ConsumedAt: &consumed}

	if !live.IsUsable(now) {
		t.Fatal("an unconsumed, unexpired signup must be usable")
	}
	if expired.IsUsable(now) {
		t.Fatal("an expired signup must not be usable")
	}
	if used.IsUsable(now) {
		t.Fatal("a consumed signup must not be usable — verification links are single-use")
	}
}

func TestPasswordReset_IsUsable(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	consumed := now.Add(-time.Minute)

	if !(domain.PasswordReset{ExpiresAt: now.Add(time.Hour)}).IsUsable(now) {
		t.Fatal("an unconsumed, unexpired reset must be usable")
	}
	if (domain.PasswordReset{ExpiresAt: now.Add(-time.Second)}).IsUsable(now) {
		t.Fatal("an expired reset must not be usable")
	}
	if (domain.PasswordReset{ExpiresAt: now.Add(time.Hour), ConsumedAt: &consumed}).IsUsable(now) {
		t.Fatal("a consumed reset must not be usable — reset links are single-use")
	}
}

func TestDigestOpaqueToken_IsOneWayAndStable(t *testing.T) {
	token := "a-verification-token"

	first := domain.DigestOpaqueToken(token)
	second := domain.DigestOpaqueToken(token)

	if first != second {
		t.Fatal("digesting must be deterministic, or a stored digest could never be matched")
	}
	if first == token || strings.Contains(first, token) {
		t.Fatal("the digest must not contain or equal the token")
	}
	if first == domain.DigestOpaqueToken(token+"x") {
		t.Fatal("distinct tokens must digest differently")
	}
}
