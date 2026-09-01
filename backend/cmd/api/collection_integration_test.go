package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type collectionRoomJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CategoryID  string `json:"category_id"`
	DesignID    string `json:"design_id"`
	CurrentTier int    `json:"current_tier"`
	Items       []struct {
		ID             string `json:"id"`
		SlotIndex      int    `json:"slot_index"`
		CatalogModelID string `json:"catalog_model_id"`
	} `json:"items"`
}

type collectionRoomListJSON struct {
	CollectionRooms []collectionRoomJSON `json:"collection_rooms"`
}

const seededCategory = "category_watches"

func (s *stack) createCollectionRoom(token, name string) string {
	s.t.Helper()
	return s.createCollectionRoomInCategory(token, name, seededCategory)
}

func (s *stack) createCollectionRoomInCategory(token, name, categoryID string) string {
	s.t.Helper()
	resp, body := s.do(http.MethodPost, "/collection-rooms",
		map[string]string{"name": name, "category_id": categoryID}, token)
	if resp.StatusCode != http.StatusCreated {
		s.t.Fatalf("create collection room: %d %s", resp.StatusCode, body)
	}
	var room collectionRoomJSON
	if err := json.Unmarshal(body, &room); err != nil {
		s.t.Fatalf("decode collection room: %v (%s)", err, body)
	}
	return room.ID
}

func decodeCollectionRoom(t *testing.T, body []byte) collectionRoomJSON {
	t.Helper()
	var room collectionRoomJSON
	if err := json.Unmarshal(body, &room); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	return room
}

func TestCollection_CreateReadUpdateDelete(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodPost, "/collection-rooms", map[string]string{
		"name": "Watches", "category_id": seededCategory, "design_id": devFixtureDesign,
	}, s.token)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	created := decodeCollectionRoom(t, body)
	if created.ID == "" {
		t.Fatal("no id returned")
	}
	if created.Name != "Watches" || created.CategoryID != seededCategory || created.DesignID != devFixtureDesign {
		t.Fatalf("create echoed %+v", created)
	}
	if created.CurrentTier != 1 {
		t.Fatalf("current_tier = %d, want 1 (the base tier)", created.CurrentTier)
	}
	if created.Items == nil {
		t.Fatal("items must serialise as [] for a new Room, never null")
	}
	if len(created.Items) != 0 {
		t.Fatalf("a new Room has %d items", len(created.Items))
	}

	resp, body = s.do(http.MethodGet, "/collection-rooms/"+created.ID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %d %s", resp.StatusCode, body)
	}
	if got := decodeCollectionRoom(t, body); got.ID != created.ID || got.Name != "Watches" {
		t.Fatalf("get returned %+v", got)
	}

	resp, body = s.do(http.MethodPatch, "/collection-rooms/"+created.ID,
		map[string]string{"name": "My Watches"}, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch: %d %s", resp.StatusCode, body)
	}
	patched := decodeCollectionRoom(t, body)
	if patched.Name != "My Watches" {
		t.Fatalf("name = %q", patched.Name)
	}
	if patched.CategoryID != seededCategory || patched.DesignID != devFixtureDesign {
		t.Fatalf("a name-only patch disturbed the references: %+v", patched)
	}
	if patched.CurrentTier != created.CurrentTier {
		t.Fatalf("current_tier moved during a rename: %d → %d", created.CurrentTier, patched.CurrentTier)
	}

	if resp, body = s.do(http.MethodDelete, "/collection-rooms/"+created.ID, nil, s.token); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", resp.StatusCode, body)
	}
	if resp, _ = s.do(http.MethodGet, "/collection-rooms/"+created.ID, nil, s.token); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete: %d, want 404", resp.StatusCode)
	}
	if resp, _ = s.do(http.MethodDelete, "/collection-rooms/"+created.ID, nil, s.token); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("repeat delete: %d, want 404", resp.StatusCode)
	}
}

func TestCollection_UnlimitedPerAccount_AndListIsOwnerScoped(t *testing.T) {
	s := newStack(t)

	const count = 30
	for index := 0; index < count; index++ {
		resp, body := s.do(http.MethodPost, "/collection-rooms",
			map[string]string{"name": "Room", "category_id": seededCategory}, s.token)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create #%d: %d %s — no per-account count limit may exist at this phase", index+1, resp.StatusCode, body)
		}
	}

	resp, body := s.do(http.MethodGet, "/collection-rooms", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", resp.StatusCode, body)
	}
	var list collectionRoomListJSON
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.CollectionRooms) != count {
		t.Fatalf("list = %d rooms, want %d", len(list.CollectionRooms), count)
	}

	stranger := s.strangerToken()
	resp, body = s.do(http.MethodGet, "/collection-rooms", nil, stranger)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stranger list: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"collection_rooms":[]`) {
		t.Fatalf("a fresh account's list should be an empty array, got %s", body)
	}
}

func TestCollection_StrangerCanNeitherReadNorMutate(t *testing.T) {
	s := newStack(t)
	roomID := s.createCollectionRoom(s.token, "Private Watches")
	stranger := s.strangerToken()
	before := s.snapshotOwnerState()

	attempts := []struct {
		name, method, path string
		body               any
	}{
		{"read", http.MethodGet, "/collection-rooms/" + roomID, nil},
		{"rename", http.MethodPatch, "/collection-rooms/" + roomID, map[string]string{"name": "Stolen"}},
		{"recategorise", http.MethodPatch, "/collection-rooms/" + roomID, map[string]string{"category_id": "category_coins"}},
		{"redesign", http.MethodPatch, "/collection-rooms/" + roomID, map[string]string{"design_id": devFixtureDesign}},
		{"delete", http.MethodDelete, "/collection-rooms/" + roomID, nil},
	}

	bogus := "00000000-0000-4000-8000-000000000000"
	for _, attempt := range attempts {
		resp, body := s.do(attempt.method, attempt.path, attempt.body, stranger)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s as a stranger → %d %s; want 404", attempt.name, resp.StatusCode, body)
		}
		absentPath := strings.Replace(attempt.path, roomID, bogus, 1)
		absentResp, absentBody := s.do(attempt.method, absentPath, attempt.body, stranger)
		if absentResp.StatusCode != resp.StatusCode || string(absentBody) != string(body) {
			t.Fatalf("%s: a foreign Collection Room (%d %s) must be indistinguishable from a nonexistent one (%d %s)",
				attempt.name, resp.StatusCode, body, absentResp.StatusCode, absentBody)
		}
	}

	for _, attempt := range attempts {
		resp, body := s.do(attempt.method, attempt.path, attempt.body, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s unauthenticated → %d %s; want 401", attempt.name, resp.StatusCode, body)
		}
	}

	resp, body := s.do(http.MethodGet, "/collection-rooms", nil, stranger)
	if resp.StatusCode != http.StatusOK || strings.Contains(string(body), roomID) {
		t.Fatalf("stranger's list leaked the owner's room: %d %s", resp.StatusCode, body)
	}

	if after := s.snapshotOwnerState(); after != before {
		t.Fatalf("the owner's state moved:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestCollection_MalformedIDsAreNotFound(t *testing.T) {
	s := newStack(t)
	s.createCollectionRoom(s.token, "Watches")

	for _, id := range []string{
		"not-a-uuid",
		"1",
		"..",
		"%27%3B+DROP+TABLE+collection_rooms%3B--",
		strings.Repeat("a", 300),
	} {
		resp, body := s.do(http.MethodGet, "/collection-rooms/"+id, nil, s.token)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET /collection-rooms/%s → %d %s; want 404", id, resp.StatusCode, body)
		}
	}

	if resp, body := s.do(http.MethodGet, "/collection-rooms", nil, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("list after malformed-id probes: %d %s", resp.StatusCode, body)
	}
}

func TestCollection_ValidatesInput(t *testing.T) {
	s := newStack(t)
	roomID := s.createCollectionRoom(s.token, "Watches")
	long := strings.Repeat("x", 400)

	cases := []struct {
		name     string
		body     any
		wantCode string
	}{
		{"blank name", map[string]string{"name": "   "}, "name_required"},
		{"missing name", map[string]string{}, "name_required"},
		{"over-long name", map[string]string{"name": long}, "name_too_long"},
		{"over-long category", map[string]string{"name": "ok", "category_id": long}, "invalid_category"},
		{"over-long design", map[string]string{"name": "ok", "design_id": long, "category_id": seededCategory}, "invalid_design"},
		{"unknown design", map[string]string{"name": "ok", "category_id": seededCategory, "design_id": "design_invented"}, "design_not_applicable"},
		{"missing category", map[string]string{"name": "ok"}, "category_required"},
		{"blank category", map[string]string{"name": "ok", "category_id": ""}, "category_required"},
		{"unknown category", map[string]string{"name": "ok", "category_id": "category_stamps"}, "unknown_category"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := s.do(http.MethodPost, "/collection-rooms", tc.body, s.token)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("→ %d %s; want 400", resp.StatusCode, body)
			}
			if !strings.Contains(string(body), tc.wantCode) {
				t.Fatalf("body %s does not carry code %q", body, tc.wantCode)
			}
		})
	}

	resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID, map[string]string{}, s.token)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "empty_patch") {
		t.Fatalf("empty patch → %d %s; want 400 empty_patch", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodPost, "/collection-rooms",
		map[string]string{"name": "Anything", "category_id": "no-such-category-exists"}, s.token)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "unknown_category") {
		t.Fatalf("invented category → %d %s; want 400 unknown_category now that a registry exists", resp.StatusCode, body)
	}

	for _, patch := range []map[string]string{{"category_id": "category_stamps"}, {"category_id": ""}} {
		resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID, patch, s.token)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("patch %v → %d %s; want 400", patch, resp.StatusCode, body)
		}
	}
}

func TestCollection_AndMuseumAreMutuallyUnaffected(t *testing.T) {
	s := newStack(t)

	museumRoom := s.createRoom()
	collectionRoom := s.createCollectionRoom(s.token, "Watches")

	museumBefore := s.snapshotOwnerState()

	if resp, body := s.do(http.MethodPatch, "/collection-rooms/"+collectionRoom,
		map[string]string{"name": "Renamed", "category_id": "category_coins", "design_id": devFixtureDesign}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("patch collection room: %d %s", resp.StatusCode, body)
	}
	if resp, body := s.do(http.MethodDelete, "/collection-rooms/"+collectionRoom, nil, s.token); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete collection room: %d %s", resp.StatusCode, body)
	}

	after := s.snapshotOwnerState()
	if after.Museum != museumBefore.Museum || after.Rooms != museumBefore.Rooms ||
		after.Slots != museumBefore.Slots || after.Assets != museumBefore.Assets {
		t.Fatalf("Collection changes touched the Museum:\nbefore %+v\nafter  %+v", museumBefore, after)
	}

	survivor := s.createCollectionRoom(s.token, "Coins")
	if resp, body := s.do(http.MethodDelete, "/museum/me/rooms/"+museumRoom, nil, s.token); resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete museum room: %d %s", resp.StatusCode, body)
	}
	if resp, body := s.do(http.MethodGet, "/collection-rooms/"+survivor, nil, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("collection room after a Museum deletion: %d %s", resp.StatusCode, body)
	}
}

func TestCollection_ConcurrentCreatesAllSucceed(t *testing.T) {
	s := newStack(t)

	const attempts = 8
	var wg sync.WaitGroup
	statuses := make([]int, attempts)
	for index := 0; index < attempts; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			resp, _ := s.do(http.MethodPost, "/collection-rooms",
				map[string]string{"name": "Concurrent", "category_id": seededCategory}, s.token)
			statuses[slot] = resp.StatusCode
		}(index)
	}
	wg.Wait()

	for index, status := range statuses {
		if status != http.StatusCreated {
			t.Fatalf("concurrent create #%d → %d, want 201", index, status)
		}
	}

	var count int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM collection_rooms WHERE account_id = $1`, s.accountID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != attempts {
		t.Fatalf("%d rooms exist after %d concurrent creates", count, attempts)
	}
}

func TestCollection_ItemWriteSurfaceAcceptsOnlyValidatedModels(t *testing.T) {
	s := newStack(t)
	roomID := s.createCollectionRoom(s.token, "Watches")

	resp, body := s.addItem(roomID, "anything")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST items with an invented model → %d %s, want 400", resp.StatusCode, body)
	}

	resp, _ = s.do(http.MethodPost, "/collection-rooms/"+roomID+"/collection-items",
		map[string]string{"catalog_model_id": syntheticModel}, s.token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST /collection-items → %d; there is exactly one item write path", resp.StatusCode)
	}
}
