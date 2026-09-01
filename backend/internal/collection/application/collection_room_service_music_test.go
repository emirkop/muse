package application_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/collection/application"
	"muse-backend/internal/collection/domain"
)

type fakeMusic struct {
	known map[string]bool
	err   error
	asked []string
}

func (f *fakeMusic) MusicTrackExists(_ context.Context, trackID string) (bool, error) {
	f.asked = append(f.asked, trackID)
	if f.err != nil {
		return false, f.err
	}
	return f.known[trackID], nil
}

func newMusicService(known ...string) (*application.CollectionRoomService, *fakeRepo, *fakeMusic) {
	service, repo, _, _ := newServiceWithDependencies()
	music := &fakeMusic{known: map[string]bool{}}
	for _, id := range known {
		music.known[id] = true
	}
	return service.WithMusicCatalog(music), repo, music
}

func TestAssignMusic_OwnerAssignsACatalogTrack(t *testing.T) {
	service, repo, music := newMusicService("track_dev_a")
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	updated, err := service.AssignMusic(ctx, "owner", room.ID, "track_dev_a")
	if err != nil {
		t.Fatal(err)
	}
	if updated.MusicTrackID != "track_dev_a" {
		t.Fatalf("music: %q", updated.MusicTrackID)
	}
	if music.asked == nil || music.asked[0] != "track_dev_a" {
		t.Fatalf("the catalog must be consulted for the track: %v", music.asked)
	}
	if len(repo.musicSets) != 1 || repo.musicSets[0] == nil || *repo.musicSets[0] != "track_dev_a" {
		t.Fatalf("exactly one music write: %v", repo.musicSets)
	}
	if updated.Name != room.Name || updated.CategoryID != room.CategoryID || updated.DesignID != room.DesignID ||
		updated.CurrentTier != room.CurrentTier || len(updated.Items) != len(room.Items) {
		t.Fatalf("a music change must touch music only: %+v vs %+v", updated, room)
	}
}

func TestAssignMusic_OwnerChangesAndRemoves(t *testing.T) {
	service, repo, _ := newMusicService("track_dev_a", "track_dev_b")
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	if _, err := service.AssignMusic(ctx, "owner", room.ID, "track_dev_a"); err != nil {
		t.Fatal(err)
	}
	changed, err := service.AssignMusic(ctx, "owner", room.ID, "track_dev_b")
	if err != nil || changed.MusicTrackID != "track_dev_b" {
		t.Fatalf("change: %+v %v", changed, err)
	}
	removed, err := service.RemoveMusic(ctx, "owner", room.ID)
	if err != nil || removed.MusicTrackID != "" {
		t.Fatalf("remove: %+v %v", removed, err)
	}
	again, err := service.RemoveMusic(ctx, "owner", room.ID)
	if err != nil || again.MusicTrackID != "" {
		t.Fatalf("removing music from a Room with none must succeed: %+v %v", again, err)
	}
	if len(repo.musicSets) != 4 || repo.musicSets[2] != nil || repo.musicSets[3] != nil {
		t.Fatalf("writes: %v", repo.musicSets)
	}
}

func TestAssignMusic_ForeignAccountIsRefusedBeforeAnyWrite(t *testing.T) {
	service, repo, music := newMusicService("track_dev_a")
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	if _, err := service.AssignMusic(ctx, "owner", room.ID, "track_dev_a"); err != nil {
		t.Fatal(err)
	}
	writesBefore, asksBefore := len(repo.musicSets), len(music.asked)

	if _, err := service.AssignMusic(ctx, "stranger", room.ID, "track_dev_a"); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("stranger assign: %v", err)
	}
	if _, err := service.RemoveMusic(ctx, "stranger", room.ID); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("stranger remove: %v", err)
	}
	if _, err := service.AssignMusic(ctx, "owner", "no-such-room", "track_dev_a"); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("nonexistent room: %v", err)
	}
	if len(repo.musicSets) != writesBefore {
		t.Fatalf("a stranger's attempt reached the repository: %v", repo.musicSets)
	}
	if len(music.asked) != asksBefore {
		t.Fatal("ownership must be checked BEFORE the catalog — a stranger must not learn which tracks exist")
	}
	still, _ := repo.Find(ctx, room.ID)
	if still.MusicTrackID != "track_dev_a" {
		t.Fatalf("owner's music changed: %q", still.MusicTrackID)
	}
}

func TestAssignMusic_InvalidTrackIsRejected(t *testing.T) {
	service, repo, _ := newMusicService("track_dev_a")
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	for name, trackID := range map[string]string{"unknown": "track_nope", "empty": ""} {
		if _, err := service.AssignMusic(ctx, "owner", room.ID, trackID); !errors.Is(err, domain.ErrUnknownMusicTrack) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(repo.musicSets) != 0 {
		t.Fatalf("a refused assignment must write nothing: %v", repo.musicSets)
	}

	unwired, _, _, _ := newServiceWithDependencies()
	room2 := roomWithDesign(t, unwired, "design_universal")
	if _, err := unwired.AssignMusic(ctx, "owner", room2.ID, "track_dev_a"); !errors.Is(err, domain.ErrUnknownMusicTrack) {
		t.Fatalf("without a catalog the service must fail closed, got %v", err)
	}
}

func TestAssignMusic_CatalogFailureIsSurfaced(t *testing.T) {
	service, repo, music := newMusicService("track_dev_a")
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	music.err = errors.New("catalog down")

	_, err := service.AssignMusic(ctx, "owner", room.ID, "track_dev_a")
	if err == nil || errors.Is(err, domain.ErrUnknownMusicTrack) {
		t.Fatalf("expected the catalog error to surface, got %v", err)
	}
	if len(repo.musicSets) != 0 {
		t.Fatal("nothing may be written when the catalog could not answer")
	}
}

func TestAssignMusic_IsIndependentOfTheGeneralPatch(t *testing.T) {
	service, repo, _ := newMusicService("track_dev_a")
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	if _, err := service.AssignMusic(ctx, "owner", room.ID, "track_dev_a"); err != nil {
		t.Fatal(err)
	}

	renamed := "Renamed Watches"
	if _, err := service.Update(ctx, "owner", room.ID, domain.CollectionRoomPatch{Name: &renamed}); err != nil {
		t.Fatal(err)
	}
	after, _ := repo.Find(ctx, room.ID)
	if after.MusicTrackID != "track_dev_a" || after.Name != renamed {
		t.Fatalf("a rename must leave music alone (and rename): %+v", after)
	}
	if len(repo.updates) != 1 || repo.updates[0].Name == nil {
		t.Fatalf("patches: %+v", repo.updates)
	}
}
