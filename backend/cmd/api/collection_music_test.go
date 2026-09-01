package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func (s *stack) assignCollectionMusic(roomID, trackID, token string) (*http.Response, []byte) {
	s.t.Helper()
	return s.do(http.MethodPut, "/collection-rooms/"+roomID+"/music", map[string]string{"music_track_id": trackID}, token)
}

func (s *stack) removeCollectionMusic(roomID, token string) (*http.Response, []byte) {
	s.t.Helper()
	return s.do(http.MethodDelete, "/collection-rooms/"+roomID+"/music", nil, token)
}

func (s *stack) collectionMusicOf(roomID string) string {
	s.t.Helper()
	r := s.get("/collection-rooms/"+roomID, s.token)
	if r.status != http.StatusOK {
		s.t.Fatalf("read collection room: %d %s", r.status, r.body)
	}
	return musicTrackOf(s.t, []byte(r.body))
}

func TestOwnerAssignsChangesAndRemovesCollectionMusic(t *testing.T) {
	s := newStack(t)
	room := s.createCollectionRoom(s.token, "Shared Watches")
	first := s.seedDevMusicTrack("track_dev_c1")
	second := s.seedDevMusicTrack("track_dev_c2")
	before := s.roomState(t, room)

	resp, raw := s.assignCollectionMusic(room, first, s.token)
	if resp.StatusCode != http.StatusOK || musicTrackOf(t, raw) != first {
		t.Fatalf("assign: %d %s", resp.StatusCode, raw)
	}
	if got := s.collectionMusicOf(room); got != first {
		t.Fatalf("the Room read must agree with the write: %q", got)
	}
	if list := s.get("/collection-rooms", s.token); !strings.Contains(list.body, `"music_track_id":"`+first+`"`) {
		t.Fatalf("list must carry the assignment: %s", list.body)
	}

	resp, raw = s.assignCollectionMusic(room, second, s.token)
	if resp.StatusCode != http.StatusOK || musicTrackOf(t, raw) != second {
		t.Fatalf("reassign: %d %s", resp.StatusCode, raw)
	}

	resp, raw = s.removeCollectionMusic(room, s.token)
	if resp.StatusCode != http.StatusOK || musicTrackOf(t, raw) != "" {
		t.Fatalf("remove: %d %s", resp.StatusCode, raw)
	}
	if strings.Contains(string(raw), "music_track_id") {
		t.Fatalf("no music is omitted, not empty: %s", raw)
	}
	if resp, raw := s.removeCollectionMusic(room, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("removing music from a Room with none must succeed: %d %s", resp.StatusCode, raw)
	}
	if after := s.roomState(t, room); after != before {
		t.Fatalf("a music change must touch music only: %s → %s", before, after)
	}
}

func TestCollectionMusicIsOwnerOnly(t *testing.T) {
	s := newStack(t)
	room := s.createCollectionRoom(s.token, "Shared Watches")
	track := s.seedDevMusicTrack("track_dev_c_owner")
	if resp, raw := s.assignCollectionMusic(room, track, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner assign: %d %s", resp.StatusCode, raw)
	}
	link := s.ensureCollectionShareLink(s.token, room)
	visitor := s.strangerToken()
	if r := s.visitCollectionRoom(link.code, visitor); r.status != http.StatusOK {
		t.Fatalf("precondition: the visitor's link must be live, got %d", r.status)
	}
	before := s.roomState(t, room)
	ownerBefore := s.snapshotOwnerState()

	for name, token := range map[string]string{
		"visitor holding a valid link": visitor,
		"stranger":                     s.strangerToken(),
		"unauthenticated":              "",
	} {
		want := http.StatusNotFound
		if token == "" {
			want = http.StatusUnauthorized
		}
		if resp, raw := s.assignCollectionMusic(room, track, token); resp.StatusCode != want {
			t.Errorf("%s assign → %d %s; want %d", name, resp.StatusCode, raw, want)
		}
		if resp, raw := s.removeCollectionMusic(room, token); resp.StatusCode != want {
			t.Errorf("%s remove → %d %s; want %d", name, resp.StatusCode, raw, want)
		}
	}
	if after := s.roomState(t, room); after != before {
		t.Fatalf("no non-owner attempt may change the Room: %s → %s", before, after)
	}
	if got := s.collectionMusicOf(room); got != track {
		t.Fatalf("the owner's track must survive every attempt, got %q", got)
	}
	if ownerAfter := s.snapshotOwnerState(); ownerAfter != ownerBefore {
		t.Fatalf("owner Museum state changed: %+v → %+v", ownerBefore, ownerAfter)
	}
	mustBeIndistinguishable(t, map[string]reply{
		"foreign room":     s.doAs(http.MethodDelete, "/collection-rooms/"+room+"/music", visitor),
		"nonexistent room": s.doAs(http.MethodDelete, "/collection-rooms/"+s.randomID()+"/music", visitor),
		"malformed id":     s.doAs(http.MethodDelete, "/collection-rooms/not-an-id/music", visitor),
	})
}

func TestInvalidTrackIsRejected(t *testing.T) {
	s := newStack(t)
	room := s.createCollectionRoom(s.token, "Shared Watches")
	before := s.roomState(t, room)

	for name, body := range map[string]any{
		"unknown track":      map[string]string{"music_track_id": "track_nope"},
		"empty track":        map[string]string{"music_track_id": ""},
		"missing field":      map[string]string{},
		"museum-style track": map[string]string{"music_track_id": "style_modern"},
	} {
		resp, raw := s.do(http.MethodPut, "/collection-rooms/"+room+"/music", body, s.token)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), `"code":"unknown_music_track"`) {
			t.Errorf("%s: %d %s", name, resp.StatusCode, raw)
		}
	}
	resp, _ := s.do(http.MethodPut, "/collection-rooms/"+room+"/music", "not json", s.token)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body: %d", resp.StatusCode)
	}
	if got := s.collectionMusicOf(room); got != "" {
		t.Fatalf("nothing may be stored: %q", got)
	}
	if after := s.roomState(t, room); after != before {
		t.Fatalf("a refused assignment changed the Room: %s → %s", before, after)
	}

	if _, err := s.pool.Pool().Exec(context.Background(),
		`UPDATE collection_rooms SET music_track_id = 'track_that_does_not_exist' WHERE id = $1`, room); err == nil {
		t.Fatal("a track outside the catalog must be refused by the foreign key")
	}
}

func TestCollectionAndMuseumMusicAreIndependent(t *testing.T) {
	s := newStack(t)
	museumRoom := s.createRoom()
	collectionRoom := s.createCollectionRoom(s.token, "Shared Watches")
	trackM := s.seedDevMusicTrack("track_dev_museum")
	trackC := s.seedDevMusicTrack("track_dev_collection")

	if resp, raw := s.assignMusic(museumRoom, trackM, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("museum assign: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := s.assignCollectionMusic(collectionRoom, trackC, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("collection assign: %d %s", resp.StatusCode, raw)
	}
	museumMusic := func() string { return musicTrackOf(t, []byte(s.get("/museum/me/rooms/"+museumRoom, s.token).body)) }
	if museumMusic() != trackM || s.collectionMusicOf(collectionRoom) != trackC {
		t.Fatalf("each surface must show its own: museum %q, collection %q", museumMusic(), s.collectionMusicOf(collectionRoom))
	}

	if resp, _ := s.assignCollectionMusic(collectionRoom, trackM, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("a Collection Room may use the same catalog track a Museum Room uses — the catalog is shared")
	}
	if museumMusic() != trackM {
		t.Fatal("a Collection assignment changed a Museum Room")
	}
	if resp, _ := s.removeMusic(museumRoom, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("museum remove")
	}
	if s.collectionMusicOf(collectionRoom) != trackM {
		t.Fatal("removing a Museum Room's music changed a Collection Room")
	}
	if resp, _ := s.removeCollectionMusic(collectionRoom, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("collection remove")
	}
	if resp, _ := s.assignMusic(museumRoom, trackC, s.token); resp.StatusCode != http.StatusOK || museumMusic() != trackC {
		t.Fatal("a Museum Room must be assignable independently after the Collection Room cleared")
	}

	var museumCol, collectionCol *string
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT (SELECT music_track_id FROM rooms WHERE id = $1), (SELECT music_track_id FROM collection_rooms WHERE id = $2)`,
		museumRoom, collectionRoom).Scan(&museumCol, &collectionCol); err != nil {
		t.Fatal(err)
	}
	if museumCol == nil || *museumCol != trackC || collectionCol != nil {
		t.Fatalf("rows: museum %v, collection %v", museumCol, collectionCol)
	}
	if resp, _ := s.assignMusic(collectionRoom, trackC, s.token); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("museum route with a collection id: %d", resp.StatusCode)
	}
	if resp, _ := s.assignCollectionMusic(museumRoom, trackC, s.token); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("collection route with a museum room id: %d", resp.StatusCode)
	}
	if resp, _ := s.do(http.MethodPatch, "/collection-rooms/"+collectionRoom, map[string]string{"name": "Renamed", "music_track_id": trackC}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("patch: %d", resp.StatusCode)
	}
	if got := s.collectionMusicOf(collectionRoom); got != "" {
		t.Fatalf("a PATCH must not be able to set music, got %q", got)
	}
}

func TestVisitorIsNotToldAboutCollectionMusic_WhileClearanceIsUnresolved(t *testing.T) {
	s := newStack(t)
	room := s.createCollectionRoom(s.token, "Shared Watches")
	track := s.seedDevMusicTrack("track_dev_c_visitor")
	if resp, raw := s.assignCollectionMusic(room, track, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner assign: %d %s", resp.StatusCode, raw)
	}
	link := s.ensureCollectionShareLink(s.token, room)
	visitor := s.strangerToken()

	if got := s.collectionMusicOf(room); got != track {
		t.Fatalf("owner must see the track, got %q", got)
	}
	visitorRoom := s.visitCollectionRoom(link.code, visitor)
	if visitorRoom.status != http.StatusOK {
		t.Fatalf("visitor room: %d %s", visitorRoom.status, visitorRoom.body)
	}
	if strings.Contains(visitorRoom.body, "music_track_id") || strings.Contains(visitorRoom.body, track) {
		t.Fatalf("visitor payload must carry no track reference while clearance is unresolved: %s", visitorRoom.body)
	}
	var decoded struct {
		CollectionRoomID string `json:"collection_room_id"`
		Name             string `json:"name"`
	}
	if err := json.Unmarshal([]byte(visitorRoom.body), &decoded); err != nil || decoded.CollectionRoomID != room || decoded.Name != "Shared Watches" {
		t.Fatalf("visitor payload: %s (%v)", visitorRoom.body, err)
	}
	if ownerViaLink := s.visitCollectionRoom(link.code, s.token); strings.Contains(ownerViaLink.body, track) {
		t.Fatalf("the visitor surface must be gated for every caller: %s", ownerViaLink.body)
	}
	museumRoom := s.createRoom()
	if resp, _ := s.assignMusic(museumRoom, track, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("museum assign")
	}
	s.setMuseumPrivacy(s.token, "public")
	s.setRoomPrivacy(s.token, museumRoom, "public")
	museumCode := s.ensureShareLink(s.token).code
	if r := s.get("/share-links/"+museumCode+"/rooms/"+museumRoom, visitor); r.status != http.StatusOK || strings.Contains(r.body, track) {
		t.Fatalf("museum visitor surface: %d %s", r.status, r.body)
	}
}
