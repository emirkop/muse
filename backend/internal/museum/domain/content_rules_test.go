package domain

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestConfirmedCaps_AreTheNumbersTheVisionConfirms(t *testing.T) {
	if MaxPhotosPerRoom != 28 {
		t.Errorf("MaxPhotosPerRoom = %d; `01` §4.6 confirms 28", MaxPhotosPerRoom)
	}
	if MaxSculpturesPerRoom != 3 {
		t.Errorf("MaxSculpturesPerRoom = %d; `01` §4.8 confirms 3", MaxSculpturesPerRoom)
	}
}

func TestRoom_CapacityBoundariesAreExact(t *testing.T) {
	photoCases := map[int]bool{0: true, 1: true, 27: true, 28: false, 29: false}
	for count, expected := range photoCases {
		room := Room{PhotoSlots: make([]PhotoSlotAssignment, count)}
		if got := room.HasCapacityForPhoto(); got != expected {
			t.Errorf("%d photographs: HasCapacityForPhoto() = %v, want %v", count, got, expected)
		}
	}

	sculptureCases := map[int]bool{0: true, 2: true, 3: false, 4: false}
	for count, expected := range sculptureCases {
		room := Room{Sculptures: make([]SculptureInstance, count)}
		if got := room.HasCapacityForSculpture(); got != expected {
			t.Errorf("%d sculptures: HasCapacityForSculpture() = %v, want %v", count, got, expected)
		}
	}
}

func TestRoom_ThePhotoAndSculptureCapsAreIndependent(t *testing.T) {
	fullOfPhotos := Room{
		PhotoSlots: make([]PhotoSlotAssignment, MaxPhotosPerRoom),
		Sculptures: make([]SculptureInstance, 1),
	}
	if fullOfPhotos.HasCapacityForPhoto() {
		t.Error("a Room with 28 photographs must refuse another")
	}
	if !fullOfPhotos.HasCapacityForSculpture() {
		t.Error("a Room full of photographs may still take a sculpture")
	}

	fullOfSculptures := Room{
		PhotoSlots: make([]PhotoSlotAssignment, 5),
		Sculptures: make([]SculptureInstance, MaxSculpturesPerRoom),
	}
	if !fullOfSculptures.HasCapacityForPhoto() {
		t.Error("a Room with 3 sculptures may still take a photograph")
	}
	if fullOfSculptures.HasCapacityForSculpture() {
		t.Error("a Room with 3 sculptures must refuse another")
	}
}

func TestSlotIndexValidity_IsBoundedAtBothEnds(t *testing.T) {
	photoCases := map[int]bool{-1: false, 0: true, 27: true, 28: false, 1000: false}
	for index, expected := range photoCases {
		if got := IsValidPhotoSlotIndex(index); got != expected {
			t.Errorf("IsValidPhotoSlotIndex(%d) = %v, want %v", index, got, expected)
		}
	}
	sculptureCases := map[int]bool{-1: false, 0: true, 2: true, 3: false}
	for index, expected := range sculptureCases {
		if got := IsValidSculptureSlotIndex(index); got != expected {
			t.Errorf("IsValidSculptureSlotIndex(%d) = %v, want %v", index, got, expected)
		}
	}
}

func TestIsValidPrivacy_RefusesEverythingOutsideTheTwoStates(t *testing.T) {
	if !IsValidPrivacy(PrivacyPublic) || !IsValidPrivacy(PrivacyPrivate) {
		t.Fatal("the two confirmed states must be valid")
	}
	for _, value := range []Privacy{"", "PUBLIC", "Private", "public ", "unlisted", "friends"} {
		if IsValidPrivacy(value) {
			t.Errorf("privacy %q was accepted", value)
		}
	}
}

func TestRoom_HasMusicReadsTheReference(t *testing.T) {
	if (Room{}).HasMusic() {
		t.Error("a Room with no music must not report having it — no track is the normal state (`01` §4.8)")
	}
	if !(Room{MusicTrackID: "track_dev_a"}).HasMusic() {
		t.Error("a Room with an assigned track has music")
	}
	if (Room{MusicTrackID: ""}).HasMusic() {
		t.Error("the empty string is the no-music state")
	}
}

// MARK: - Captions

func TestNormalisedCaption_WhitespaceOnlyBecomesNoCaption(t *testing.T) {
	for _, input := range []string{"", " ", "   ", "\t", "\n", " \t\n "} {
		if got := NormalisedCaption(input); got != "" {
			t.Errorf("NormalisedCaption(%q) = %q, want the no-caption state", input, got)
		}
	}
}

func TestNormalisedCaption_TrimsAndTransformsNothingElse(t *testing.T) {
	cases := map[string]string{
		"  Grandad's watch  ": "Grandad's watch",
		"Grandad's watch":     "Grandad's watch",
		"MIXED Case Kept":     "MIXED Case Kept",
		"inner   spaces kept": "inner   spaces kept",
		"**not markdown**":    "**not markdown**",
		"line\nbreak kept":    "line\nbreak kept",
		"emoji 🕰 kept":        "emoji 🕰 kept",
		"<b>not html</b>":     "<b>not html</b>",
	}
	for input, expected := range cases {
		if got := NormalisedCaption(input); got != expected {
			t.Errorf("NormalisedCaption(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestValidateCaption_BoundIsBytesAndAppliedAfterTrimming(t *testing.T) {
	if MaxCaptionBytes != 500 {
		t.Errorf("MaxCaptionBytes = %d; the interim bound is 500", MaxCaptionBytes)
	}

	atLimit := strings.Repeat("a", MaxCaptionBytes)
	if err := ValidateCaption(atLimit); err != nil {
		t.Errorf("a caption exactly at the bound must be accepted: %v", err)
	}
	if err := ValidateCaption(atLimit + "a"); !errors.Is(err, ErrCaptionTooLong) {
		t.Errorf("one byte over → %v, want ErrCaptionTooLong", err)
	}
	if err := ValidateCaption("   " + atLimit + "   "); err != nil {
		t.Errorf("trimming happens before the bound is applied: %v", err)
	}

	threeByte := "☕"
	if utf8.RuneLen([]rune(threeByte)[0]) != 3 {
		t.Fatalf("the fixture is not a three-byte rune")
	}
	if err := ValidateCaption(strings.Repeat(threeByte, 166)); err != nil {
		t.Errorf("498 bytes must be accepted: %v", err)
	}
	if err := ValidateCaption(strings.Repeat(threeByte, 167)); !errors.Is(err, ErrCaptionTooLong) {
		t.Errorf("501 bytes → %v, want ErrCaptionTooLong", err)
	}
}

func TestValidateCaption_RefusesInvalidUTF8(t *testing.T) {
	if err := ValidateCaption("valid \xff\xfe bytes"); !errors.Is(err, ErrInvalidCaption) {
		t.Errorf("invalid UTF-8 → %v, want ErrInvalidCaption", err)
	}
	if err := ValidateCaption(""); err != nil {
		t.Errorf("clearing a caption must be valid: %v", err)
	}
}

// MARK: - The wall alternation helper

func TestOppositeWall_OnlyTheSideWallsAlternate(t *testing.T) {
	if oppositeWall(WallLeft) != WallRight {
		t.Error("left's opposite is right")
	}
	if oppositeWall(WallRight) != WallLeft {
		t.Error("right's opposite is left")
	}
	for _, wall := range []RoomWall{WallFocal, WallRear, RoomWall("")} {
		if got := oppositeWall(wall); got != wall {
			t.Errorf("oppositeWall(%q) = %q; only the side walls participate", wall, got)
		}
	}
}
