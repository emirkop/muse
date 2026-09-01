package application_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/museum/domain"
)

func TestAssignRoomMusic_WithAnEmptyCatalog_IsRefused(t *testing.T) {
	h, _ := seedOwner(t)

	err := h.service.AssignRoomMusic(context.Background(), "owner", h.private.ID, "track_anything")

	if !errors.Is(err, domain.ErrUnknownMusicTrack) {
		t.Fatalf("got %v, want ErrUnknownMusicTrack — the production catalog is empty", err)
	}
	room, _ := h.service.FindRoom(context.Background(), "owner", h.private.ID)
	if room.HasMusic() {
		t.Fatal("a refused assignment must leave the Room without music")
	}
}

func TestAssignRoomMusic_WithACatalogTrack_AssignsTheReference(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	h.catalog.musicTracks = map[string]bool{"track_dev_tone": true}

	if err := h.service.AssignRoomMusic(ctx, "owner", h.private.ID, "track_dev_tone"); err != nil {
		t.Fatalf("assign: %v", err)
	}

	room, _ := h.service.FindRoom(ctx, "owner", h.private.ID)
	if room.MusicTrackID != "track_dev_tone" || !room.HasMusic() {
		t.Fatalf("expected the track reference, got %q", room.MusicTrackID)
	}
}

func TestAssignRoomMusic_TouchesNothingElseAboutTheRoom(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	h.catalog.musicTracks = map[string]bool{"track_a": true, "track_b": true}
	before, _ := h.service.FindRoom(ctx, "owner", h.private.ID)

	if err := h.service.AssignRoomMusic(ctx, "owner", h.private.ID, "track_a"); err != nil {
		t.Fatal(err)
	}
	if err := h.service.AssignRoomMusic(ctx, "owner", h.private.ID, "track_b"); err != nil {
		t.Fatal(err)
	}
	after, _ := h.service.FindRoom(ctx, "owner", h.private.ID)

	if after.MusicTrackID != "track_b" {
		t.Fatalf("reassignment must replace, got %q", after.MusicTrackID)
	}
	if after.Name != before.Name || after.VariantID != before.VariantID || after.Privacy != before.Privacy {
		t.Fatalf("name/variant/privacy must be untouched:\nbefore %+v\nafter  %+v", before, after)
	}
	if len(after.PhotoSlots) != len(before.PhotoSlots) || len(after.Sculptures) != len(before.Sculptures) {
		t.Fatal("photographs and sculptures must be untouched by a music change")
	}
}

func TestRemoveRoomMusic_ClearsIt_AndIsIdempotent(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	h.catalog.musicTracks = map[string]bool{"track_a": true}
	if err := h.service.AssignRoomMusic(ctx, "owner", h.private.ID, "track_a"); err != nil {
		t.Fatal(err)
	}

	if err := h.service.RemoveRoomMusic(ctx, "owner", h.private.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	room, _ := h.service.FindRoom(ctx, "owner", h.private.ID)
	if room.HasMusic() {
		t.Fatalf("expected no music, got %q", room.MusicTrackID)
	}

	if err := h.service.RemoveRoomMusic(ctx, "owner", h.private.ID); err != nil {
		t.Fatalf("removing music from a Room with none must succeed, got %v", err)
	}
}

func TestRoomMusic_IsOwnerOnly(t *testing.T) {
	h, _ := seedOwner(t)
	ctx := context.Background()
	h.catalog.musicTracks = map[string]bool{"track_a": true}
	if _, err := h.service.CreateMuseum(ctx, "intruder-with-museum", "style_modern"); err != nil {
		t.Fatal(err)
	}
	writes := h.repo.roomWrites

	for caller, want := range map[string]error{
		"intruder-without-museum": domain.ErrMuseumNotFound,
		"intruder-with-museum":    domain.ErrNotOwner,
	} {
		if err := h.service.AssignRoomMusic(ctx, caller, h.private.ID, "track_a"); !errors.Is(err, want) {
			t.Errorf("%s assign: got %v, want %v", caller, err, want)
		}
		if err := h.service.RemoveRoomMusic(ctx, caller, h.private.ID); !errors.Is(err, want) {
			t.Errorf("%s remove: got %v, want %v", caller, err, want)
		}
	}
	if h.repo.roomWrites != writes {
		t.Fatal("no non-owner attempt may write")
	}
	if err := h.service.AssignRoomMusic(ctx, "intruder-with-museum", h.private.ID, "track_does_not_exist"); !errors.Is(err, domain.ErrNotOwner) {
		t.Fatalf("ownership must be decided first, got %v", err)
	}
}

func TestAssignRoomMusic_EmptyTrackID_IsRefused(t *testing.T) {
	h, _ := seedOwner(t)

	if err := h.service.AssignRoomMusic(context.Background(), "owner", h.private.ID, ""); !errors.Is(err, domain.ErrUnknownMusicTrack) {
		t.Fatalf("got %v, want ErrUnknownMusicTrack", err)
	}
}
