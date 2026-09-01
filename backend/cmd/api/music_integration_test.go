package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func (s *stack) seedDevMusicTrack(id string) string {
	s.t.Helper()
	_, err := s.pool.Pool().Exec(s.t.Context(), `
		INSERT INTO music_tracks (id, display_name, attribution, licensing, storage_key, content_type, duration_seconds)
		VALUES ($1, $2, 'Muse (test audio)', 'dev_test', $3, 'audio/mpeg', 12)
		ON CONFLICT (id) DO NOTHING`,
		id, "DEV TEST TONE — not licensed content", "music/dev/"+id+".mp3")
	if err != nil {
		s.t.Fatalf("seed dev music track: %v", err)
	}
	return id
}

func (s *stack) assignMusic(roomID, trackID, token string) (*http.Response, []byte) {
	s.t.Helper()
	return s.do(http.MethodPut, "/museum/me/rooms/"+roomID+"/music", map[string]string{"music_track_id": trackID}, token)
}

func (s *stack) removeMusic(roomID, token string) (*http.Response, []byte) {
	s.t.Helper()
	return s.do(http.MethodDelete, "/museum/me/rooms/"+roomID+"/music", nil, token)
}

func musicTrackOf(t *testing.T, body []byte) string {
	t.Helper()
	var room struct {
		MusicTrackID string `json:"music_track_id"`
	}
	if err := json.Unmarshal(body, &room); err != nil {
		t.Fatalf("decode room: %v (%s)", err, body)
	}
	return room.MusicTrackID
}

func TestProductionCatalogIsEmpty_AndEveryAssignmentIsRefused(t *testing.T) {
	s := newStack(t)
	room := s.createRoom()

	list := s.get("/catalog/music", s.token)
	if list.status != http.StatusOK {
		t.Fatalf("catalog list: %d %s", list.status, list.body)
	}
	if !strings.Contains(list.body, `"tracks":[]`) {
		t.Fatalf("the curated catalog must ship empty as an empty list, got %s", list.body)
	}

	resp, raw := s.assignMusic(room, "track_anything", s.token)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "unknown music track") {
		t.Fatalf("expected 400 unknown music track, got %d %s", resp.StatusCode, raw)
	}
	if r := s.get("/museum/me/rooms/"+room, s.token); strings.Contains(r.body, "music_track_id") {
		t.Fatalf("a Room with no music must omit the field entirely: %s", r.body)
	}
}

func TestOwnerAssignsAndRemovesMusic(t *testing.T) {
	s := newStack(t)
	room := s.createRoom()
	first := s.seedDevMusicTrack("track_dev_one")
	second := s.seedDevMusicTrack("track_dev_two")

	resp, raw := s.assignMusic(room, first, s.token)
	if resp.StatusCode != http.StatusOK || musicTrackOf(t, raw) != first {
		t.Fatalf("assign: %d %s", resp.StatusCode, raw)
	}
	if got := musicTrackOf(t, []byte(s.get("/museum/me/rooms/"+room, s.token).body)); got != first {
		t.Fatalf("expected %s, got %s", first, got)
	}

	resp, raw = s.assignMusic(room, second, s.token)
	if resp.StatusCode != http.StatusOK || musicTrackOf(t, raw) != second {
		t.Fatalf("reassign: %d %s", resp.StatusCode, raw)
	}

	resp, raw = s.removeMusic(room, s.token)
	if resp.StatusCode != http.StatusOK || musicTrackOf(t, raw) != "" {
		t.Fatalf("remove: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := s.removeMusic(room, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("removing music from a Room with none must succeed: %d %s", resp.StatusCode, raw)
	}
	after := s.get("/museum/me/rooms/"+room, s.token)
	if !strings.Contains(after.body, `"name":"Trabzon"`) || !strings.Contains(after.body, `"variant_id"`) {
		t.Fatalf("a music change must not disturb the Room: %s", after.body)
	}
}

func TestRoomMusicIsARealForeignKey(t *testing.T) {
	s := newStack(t)
	room := s.createRoom()

	_, err := s.pool.Pool().Exec(t.Context(),
		`UPDATE rooms SET music_track_id = 'not_in_the_catalog' WHERE id = $1`, room)

	if err == nil {
		t.Fatal("the foreign key must reject a track id with no catalog row")
	}
}

func TestMusicAssignmentIsOwnerOnly(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)
	track := s.seedDevMusicTrack("track_dev_gate")
	if resp, raw := s.assignMusic(f.publicRoom, track, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner assign: %d %s", resp.StatusCode, raw)
	}
	before := s.snapshotOwnerState()

	for name, token := range map[string]string{
		"stranger without museum":  f.stranger,
		"stranger with own museum": f.strangerWithMuseum,
		"unauthenticated":          "",
	} {
		t.Run(name, func(t *testing.T) {
			wantAssign := http.StatusNotFound
			if token == "" {
				wantAssign = http.StatusUnauthorized
			}
			if resp, raw := s.assignMusic(f.publicRoom, track, token); resp.StatusCode != wantAssign {
				t.Errorf("assign → %d %s; want %d", resp.StatusCode, raw, wantAssign)
			}
			if resp, raw := s.removeMusic(f.publicRoom, token); resp.StatusCode != wantAssign {
				t.Errorf("remove → %d %s; want %d", resp.StatusCode, raw, wantAssign)
			}
		})
	}
	if after := s.snapshotOwnerState(); after != before {
		t.Fatalf("no non-owner attempt may change the owner's state:\nbefore %+v\nafter  %+v", before, after)
	}
	if got := musicTrackOf(t, []byte(s.get("/museum/me/rooms/"+f.publicRoom, s.token).body)); got != track {
		t.Fatalf("the owner's track must still be assigned, got %q", got)
	}
}

func TestVisitorIsNotToldAboutMusic_WhileClearanceIsUnresolved(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)
	s.setMuseumPrivacy(s.token, "public")
	code := s.ensureShareLink(s.token).code
	track := s.seedDevMusicTrack("track_dev_visitor")
	if resp, raw := s.assignMusic(f.publicRoom, track, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner assign: %d %s", resp.StatusCode, raw)
	}

	if got := musicTrackOf(t, []byte(s.get("/museum/me/rooms/"+f.publicRoom, s.token).body)); got != track {
		t.Fatalf("owner must see the track, got %q", got)
	}

	visitorRoom := s.get("/share-links/"+code+"/rooms/"+f.publicRoom, f.stranger)
	if visitorRoom.status != http.StatusOK {
		t.Fatalf("visitor room: %d %s", visitorRoom.status, visitorRoom.body)
	}
	if strings.Contains(visitorRoom.body, "music_track_id") || strings.Contains(visitorRoom.body, track) {
		t.Fatalf("visitor payload must carry no track reference while clearance is unresolved: %s", visitorRoom.body)
	}
	ownerViaLink := s.get("/share-links/"+code+"/rooms/"+f.publicRoom, s.token)
	if strings.Contains(ownerViaLink.body, track) {
		t.Fatalf("the visitor surface must be gated for every caller: %s", ownerViaLink.body)
	}
}

func TestAudioURLIsShortLivedAndUncacheable(t *testing.T) {
	s := newStack(t)
	track := s.seedDevMusicTrack("track_dev_audio")

	resp, raw := s.do(http.MethodGet, "/catalog/music/"+track+"/audio-url", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audio url: %d %s", resp.StatusCode, raw)
	}
	var decoded struct {
		URL       string `json:"url"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.URL == "" || decoded.ExpiresAt == "" {
		t.Fatalf("expected a URL and an expiry: %s", raw)
	}
	if !strings.Contains(decoded.URL, "sig=") {
		t.Fatalf("the URL must be signed, not a bare object path: %s", decoded.URL)
	}
	if resp.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("audio URLs must not be cacheable: %q", resp.Header.Get("Cache-Control"))
	}
	if resp, _ := s.do(http.MethodGet, "/catalog/music/track_nope/audio-url", nil, s.token); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown track: expected 404, got %d", resp.StatusCode)
	}
	if resp, _ := s.do(http.MethodGet, "/catalog/music/"+track+"/audio-url", nil, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestCatalogReportsLicensingState(t *testing.T) {
	s := newStack(t)
	s.seedDevMusicTrack("track_dev_listed")

	list := s.get("/catalog/music", s.token)

	var decoded struct {
		Tracks []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Attribution string `json:"attribution"`
			Licensing   string `json:"licensing"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal([]byte(list.body), &decoded); err != nil {
		t.Fatal(err)
	}
	var track struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Attribution string `json:"attribution"`
		Licensing   string `json:"licensing"`
	}
	for _, candidate := range decoded.Tracks {
		if candidate.ID == "track_dev_listed" {
			track = candidate
		}
	}
	if track.ID == "" {
		t.Fatalf("the seeded track must be listed, got %s", list.body)
	}
	if track.Licensing != "dev_test" {
		t.Fatalf("licensing state must be reported: %+v", track)
	}
	if track.Attribution == "" {
		t.Fatalf("attribution must be carried so a client can display credit: %+v", track)
	}
	if strings.Contains(list.body, "storage_key") || strings.Contains(list.body, "music/dev/") {
		t.Fatalf("the catalog must never expose storage keys: %s", list.body)
	}
}
