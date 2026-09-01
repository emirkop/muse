package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"muse-backend/internal/catalog/application"
	"muse-backend/internal/catalog/domain"
)

type fakeMusicCatalog struct {
	tracks map[string]domain.MusicTrack
}

func (f fakeMusicCatalog) FindMusicTrack(_ context.Context, trackID string) (domain.MusicTrack, error) {
	if track, ok := f.tracks[trackID]; ok {
		return track, nil
	}
	return domain.MusicTrack{}, domain.ErrMusicTrackNotFound
}

type fakeAudio struct{ presigns int }

func (f *fakeAudio) PresignAudio(_ context.Context, storageKey string, ttl time.Duration) (application.AudioURL, error) {
	f.presigns++
	return application.AudioURL{
		URL:       "https://cdn.example/" + storageKey + "?sig=x",
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

func catalogWith(tracks ...domain.MusicTrack) fakeMusicCatalog {
	byID := map[string]domain.MusicTrack{}
	for _, t := range tracks {
		byID[string(t.ID)] = t
	}
	return fakeMusicCatalog{tracks: byID}
}

var (
	devTrack = domain.MusicTrack{
		ID: "track_dev_tone", DisplayName: "Dev Test Tone (not for production)",
		Licensing: domain.LicensingDevTest, StorageKey: "music/dev/tone.mp3", ContentType: "audio/mpeg",
	}
	licensedTrack = domain.MusicTrack{
		ID: "track_licensed", DisplayName: "A Licensed Track", Attribution: "Rights Holder",
		Licensing: domain.LicensingLicensed, StorageKey: "music/licensed/track.mp3", ContentType: "audio/mpeg",
	}
)

func TestAudioURL_DevTestTrack_IsRefusedInProduction(t *testing.T) {
	audio := &fakeAudio{}
	service := application.NewMusicDeliveryService(catalogWith(devTrack), audio, time.Minute, true)

	_, err := service.AudioURL(context.Background(), "track_dev_tone")

	if !errors.Is(err, domain.ErrMusicTrackNotCleared) {
		t.Fatalf("got %v, want ErrMusicTrackNotCleared", err)
	}
	if audio.presigns != 0 {
		t.Fatal("a refused track must not be presigned at all")
	}
}

func TestAudioURL_DevTestTrack_IsServedOutsideProduction(t *testing.T) {
	audio := &fakeAudio{}
	service := application.NewMusicDeliveryService(catalogWith(devTrack), audio, time.Minute, false)

	url, err := service.AudioURL(context.Background(), "track_dev_tone")

	if err != nil {
		t.Fatalf("dev audio must be usable in development: %v", err)
	}
	if url.URL == "" || url.ExpiresAt.IsZero() {
		t.Fatalf("expected a short-lived URL, got %+v", url)
	}
	if audio.presigns != 1 {
		t.Fatalf("expected one presign, got %d", audio.presigns)
	}
}

func TestAudioURL_LicensedTrack_IsServedInProduction(t *testing.T) {
	service := application.NewMusicDeliveryService(catalogWith(licensedTrack), &fakeAudio{}, time.Minute, true)

	url, err := service.AudioURL(context.Background(), "track_licensed")

	if err != nil {
		t.Fatalf("a licensed track must be servable in production: %v", err)
	}
	if url.URL == "" {
		t.Fatal("expected a URL")
	}
}

func TestAudioURL_UnknownTrack_IsNotFound(t *testing.T) {
	service := application.NewMusicDeliveryService(catalogWith(), &fakeAudio{}, time.Minute, true)

	_, err := service.AudioURL(context.Background(), "track_anything")

	if !errors.Is(err, domain.ErrMusicTrackNotFound) {
		t.Fatalf("got %v, want ErrMusicTrackNotFound", err)
	}
}

func TestAudioURL_TrackWithoutStoredAudio_IsRefused(t *testing.T) {
	empty := domain.MusicTrack{ID: "track_metadata_only", Licensing: domain.LicensingLicensed}
	service := application.NewMusicDeliveryService(catalogWith(empty), &fakeAudio{}, time.Minute, false)

	_, err := service.AudioURL(context.Background(), "track_metadata_only")

	if !errors.Is(err, domain.ErrMusicTrackNotCleared) {
		t.Fatalf("got %v, want ErrMusicTrackNotCleared", err)
	}
	if errors.Is(err, domain.ErrMusicTrackNotFound) {
		t.Fatal("an existing entry must not be reported as missing")
	}
}

func TestSeedMusicTracks_IsEmpty(t *testing.T) {
	if tracks := domain.SeedMusicTracks(); len(tracks) != 0 {
		t.Fatalf("the curated catalog must ship empty until a licence is confirmed, got %d entries", len(tracks))
	}
}

func TestMusicLicensing_OnlyConfirmedLicensingCounts(t *testing.T) {
	if devTrack.IsLicensed() {
		t.Fatal("dev_test audio is not licensed content")
	}
	if !licensedTrack.IsLicensed() {
		t.Fatal("a licensed track is licensed")
	}
	if (domain.MusicTrack{}).IsLicensed() {
		t.Fatal("a zero-value track must never read as licensed")
	}
}
