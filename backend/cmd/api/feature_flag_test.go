package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"muse-backend/internal/platform/featureflag"
	platformhttp "muse-backend/internal/platform/http"
	sharingapp "muse-backend/internal/sharing/application"
	sharinginfra "muse-backend/internal/sharing/infrastructure"
	sharingiface "muse-backend/internal/sharing/interfaces"
)

func TestFlagOff_VisitorIsToldNothingAboutMusic(t *testing.T) {
	s := newStack(t)
	flags := mustFlags(t, "production")

	if visitorMusicPolicy(flags).AudibleToVisitors {
		t.Fatal("with nothing set, the production derivation must gate visitor music OFF")
	}

	room, track, code, visitor := s.sharedCollectionRoomWithMusic(t)
	body := s.visitCollectionRoomAgainst(s.server.URL, code, visitor)
	if strings.Contains(body, "music_track_id") || strings.Contains(body, track) {
		t.Fatalf("flag off: the visitor payload must carry no track reference: %s", body)
	}
	if got := s.collectionMusicOf(room); got != track {
		t.Fatalf("the owner's own assignment must be unaffected, got %q", got)
	}
}

func TestFlagOn_VisitorIsServedTheTrackReference(t *testing.T) {
	s := newStack(t)
	room, track, code, visitor := s.sharedCollectionRoomWithMusic(t)

	if body := s.visitCollectionRoomAgainst(s.server.URL, code, visitor); strings.Contains(body, track) {
		t.Fatalf("precondition: the default surface should withhold the track: %s", body)
	}

	cleared := s.collectionSharingSurfaceWith(t, mustFlags(t, "production",
		"MUSE_FLAG_VISITOR_AUDIBLE_ROOM_MUSIC=enabled"))
	body := s.visitCollectionRoomAgainst(cleared, code, visitor)
	if !strings.Contains(body, `"music_track_id":"`+track+`"`) {
		t.Fatalf("flag on: the visitor payload should carry the track reference: %s", body)
	}
	if !strings.Contains(body, `"collection_room_id":"`+room+`"`) {
		t.Fatalf("flag on: the rest of the visitor payload must be intact: %s", body)
	}
	if strings.Contains(body, "account_id") || strings.Contains(body, "privacy") {
		t.Fatalf("flag on: the payload must not have grown other fields: %s", body)
	}
}

func TestFlagOn_ChangesNoAuthorizationOutcome(t *testing.T) {
	s := newStack(t)
	_, _, code, visitor := s.sharedCollectionRoomWithMusic(t)
	cleared := s.collectionSharingSurfaceWith(t, mustFlags(t, "production",
		"MUSE_FLAG_VISITOR_AUDIBLE_ROOM_MUSIC=enabled"))

	resp, raw := s.doAgainst(cleared, http.MethodGet,
		"/collection-share-links/aaaaaaaaaaaaaaaaaaaaaa/collection-room", nil, visitor)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown code with the flag on → %d %s, want 404", resp.StatusCode, raw)
	}
	resp, raw = s.doAgainst(cleared, http.MethodGet,
		"/collection-share-links/"+code+"/collection-room", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated with the flag on → %d %s, want 401", resp.StatusCode, raw)
	}
}

// MARK: - Harness

func mustFlags(t *testing.T, environment string, environ ...string) *featureflag.Provider {
	t.Helper()
	flags, err := featureflag.NewProvider(environment, environ)
	if err != nil {
		t.Fatalf("build flags %v: %v", environ, err)
	}
	return flags
}

func (s *stack) sharedCollectionRoomWithMusic(t *testing.T) (room, track, code, visitor string) {
	t.Helper()
	room = s.createCollectionRoom(s.token, "Flagged Watches")
	track = s.seedDevMusicTrack("track_dev_flag_89")
	if resp, raw := s.assignCollectionMusic(room, track, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner assign music: %d %s", resp.StatusCode, raw)
	}
	return room, track, s.ensureCollectionShareLink(s.token, room).code, s.strangerToken()
}

func (s *stack) collectionSharingSurfaceWith(t *testing.T, flags featureflag.FeatureFlagProviding) string {
	t.Helper()
	router := platformhttp.NewRouter()
	service := sharingapp.NewCollectionShareLinkService(
		sharinginfra.NewPostgresCollectionShareLinkRepository(s.pool.Pool()),
		sharinginfra.RandomCodeGenerator{},
		collectionForSharing{rooms: s.collection},
		nil,
	).WithVisitorMusicPolicy(visitorMusicPolicy(flags))
	sharingiface.NewCollectionHandlers(service, s.authenticator, sharingiface.Config{
		ShareLinkBaseURL: testShareLinkBase,
		AppStoreURL:      testAppStoreURL,
	}, s.logger).RegisterRoutes(router)

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server.URL
}

func (s *stack) visitCollectionRoomAgainst(baseURL, code, token string) string {
	s.t.Helper()
	resp, raw := s.doAgainst(baseURL, http.MethodGet,
		"/collection-share-links/"+code+"/collection-room", nil, token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("visitor collection room: %d %s", resp.StatusCode, raw)
	}
	return string(raw)
}
