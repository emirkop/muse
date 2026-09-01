package domain

import (
	"testing"
	"time"
)

func TestIsValidAvatarID_IsTheFiveConfirmedAvatarsAndNothingElse(t *testing.T) {
	if len(AvailableAvatarIDs) != 5 {
		t.Fatalf("%d avatars available; `01` §4.2 confirms five", len(AvailableAvatarIDs))
	}
	for _, id := range AvailableAvatarIDs {
		if !IsValidAvatarID(id) {
			t.Errorf("%q is in the catalog but was rejected", id)
		}
	}
	for _, id := range []AvatarID{"", "avatar_6", "AVATAR_1", "avatar_1 ", "../avatar_1", "0"} {
		if IsValidAvatarID(id) {
			t.Errorf("%q was accepted as an avatar id", id)
		}
	}
}

func TestAccount_IsDeletedTracksTheSoftDeleteTimestamp(t *testing.T) {
	if (Account{}).IsDeleted() {
		t.Error("an account with no deletion timestamp is not deleted")
	}
	deletedAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if !(Account{DeletedAt: &deletedAt}).IsDeleted() {
		t.Error("an account with a deletion timestamp is deleted")
	}
	var zero time.Time
	if !(Account{DeletedAt: &zero}).IsDeleted() {
		t.Error("a non-nil pointer means deleted, whatever the timestamp")
	}
}

func TestRefreshToken_ExpiryBoundaryIsStrict(t *testing.T) {
	expiry := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	token := RefreshToken{ExpiresAt: expiry}

	if token.IsExpired(expiry.Add(-time.Nanosecond)) {
		t.Error("a token one nanosecond before expiry is live")
	}
	if token.IsExpired(expiry) {
		t.Error("a token exactly at its expiry is not yet expired")
	}
	if !token.IsExpired(expiry.Add(time.Nanosecond)) {
		t.Error("a token one nanosecond after expiry is expired")
	}
	if !(RefreshToken{}).IsExpired(expiry) {
		t.Error("a token with no expiry must not be treated as live")
	}
}
