package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	museumdomain "muse-backend/internal/museum/domain"
)

func (s *stack) seedSculptureCatalog(ids ...string) []string {
	s.t.Helper()
	for _, id := range ids {
		if _, err := s.pool.Pool().Exec(context.Background(), `
			INSERT INTO sculptures (id, display_name, asset_bundle_id, asset_bundle_version)
			VALUES ($1, $1, 'bundle_test_' || $1, 1)
			ON CONFLICT (id) DO NOTHING`, id); err != nil {
			s.t.Fatalf("seed sculpture catalog %s: %v", id, err)
		}
	}
	return ids
}

type sculptureJSON struct {
	SlotIndex int    `json:"slot_index"`
	CatalogID string `json:"catalog_id"`
}

func (s *stack) addSculpture(roomID, catalogID, token string) (*http.Response, []sculptureJSON, map[string]any) {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/museum/me/rooms/"+roomID+"/sculptures",
		map[string]string{"catalog_id": catalogID}, token)
	sculptures, errBody := decodeSculptures(resp, raw, http.StatusCreated)
	return resp, sculptures, errBody
}

func (s *stack) removeSculpture(roomID string, slotIndex int, token string) (*http.Response, []sculptureJSON, map[string]any) {
	s.t.Helper()
	resp, raw := s.do(http.MethodDelete, fmt.Sprintf("/museum/me/rooms/%s/sculptures/%d", roomID, slotIndex), nil, token)
	sculptures, errBody := decodeSculptures(resp, raw, http.StatusOK)
	return resp, sculptures, errBody
}

func decodeSculptures(resp *http.Response, raw []byte, okStatus int) ([]sculptureJSON, map[string]any) {
	var ok struct {
		Sculptures []sculptureJSON `json:"sculptures"`
	}
	var errBody map[string]any
	if resp.StatusCode == okStatus {
		_ = json.Unmarshal(raw, &ok)
	} else {
		_ = json.Unmarshal(raw, &errBody)
	}
	return ok.Sculptures, errBody
}

func sculptureAt(sculptures []sculptureJSON, slotIndex int) string {
	for _, sculpture := range sculptures {
		if sculpture.SlotIndex == slotIndex {
			return sculpture.CatalogID
		}
	}
	return "<empty>"
}

func (s *stack) sculptureCount(roomID string) int {
	s.t.Helper()
	var n int
	_ = s.pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM room_sculptures WHERE room_id = $1`, roomID).Scan(&n)
	return n
}

// MARK: - The catalog is truthfully empty

func TestSculptureCatalogAPI_IsEmptyButReal(t *testing.T) {
	s := newStack(t)

	resp, raw := s.do(http.MethodGet, "/catalog/sculptures", nil, s.token)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("catalog must answer 200 even when empty, got %d %s", resp.StatusCode, raw)
	}
	var body struct {
		Sculptures []sculptureJSON `json:"sculptures"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Sculptures) != 0 {
		t.Errorf("the production catalog must be empty until real content exists; got %v", body.Sculptures)
	}
	if !json.Valid(raw) || !containsKey(raw, "sculptures") {
		t.Errorf("response must carry a sculptures key: %s", raw)
	}
	if unauth, _ := s.do(http.MethodGet, "/catalog/sculptures", nil, ""); unauth.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated catalog read must be 401, got %d", unauth.StatusCode)
	}
}

func containsKey(raw []byte, key string) bool {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return false
	}
	_, present := generic[key]
	return present
}

func TestSculptureAPI_WithTheRealEmptyCatalog_NothingCanBePlaced(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	before := s.snapshotOwnerState()

	resp, _, errBody := s.addSculpture(roomID, "sculpture_anything", s.token)

	if resp.StatusCode != http.StatusBadRequest || errBody["code"] != "unknown_sculpture" {
		t.Fatalf("expected 400 unknown_sculpture, got %d %v", resp.StatusCode, errBody)
	}
	if s.sculptureCount(roomID) != 0 {
		t.Error("nothing may be placed")
	}
	if s.snapshotOwnerState() != before {
		t.Error("a refused add must change nothing")
	}
}

// MARK: - Add at the lowest free slot

func TestSculptureAPI_AddsAtTheLowestFreeSlot(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	ids := s.seedSculptureCatalog("sculpture_a", "sculpture_b", "sculpture_c")

	for index, id := range ids {
		resp, sculptures, errBody := s.addSculpture(roomID, id, s.token)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s: %d %v", id, resp.StatusCode, errBody)
		}
		if len(sculptures) != index+1 {
			t.Fatalf("expected %d sculptures, got %v", index+1, sculptures)
		}
		if got := sculptureAt(sculptures, index); got != id {
			t.Errorf("slot %d holds %q, want %q", index, got, id)
		}
	}

	resp, raw := s.do(http.MethodGet, "/museum/me/rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get room: %d", resp.StatusCode)
	}
	var room struct {
		Sculptures []sculptureJSON `json:"sculptures"`
	}
	_ = json.Unmarshal(raw, &room)
	if len(room.Sculptures) != 3 {
		t.Fatalf("room payload sculptures = %v", room.Sculptures)
	}
	for index, id := range ids {
		if got := sculptureAt(room.Sculptures, index); got != id {
			t.Errorf("room payload slot %d holds %q, want %q", index, got, id)
		}
	}
}

func TestSculptureAPI_FourthIsRefused(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	ids := s.seedSculptureCatalog("sculpture_a", "sculpture_b", "sculpture_c", "sculpture_d")
	for _, id := range ids[:museumdomain.MaxSculpturesPerRoom] {
		if resp, _, errBody := s.addSculpture(roomID, id, s.token); resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed %s: %d %v", id, resp.StatusCode, errBody)
		}
	}
	before := s.snapshotOwnerState()

	resp, _, errBody := s.addSculpture(roomID, ids[3], s.token)

	if resp.StatusCode != http.StatusConflict || errBody["code"] != "sculpture_capacity_reached" {
		t.Fatalf("expected 409 sculpture_capacity_reached, got %d %v", resp.StatusCode, errBody)
	}
	if s.sculptureCount(roomID) != museumdomain.MaxSculpturesPerRoom {
		t.Errorf("room holds %d sculptures, want exactly %d", s.sculptureCount(roomID), museumdomain.MaxSculpturesPerRoom)
	}
	if s.snapshotOwnerState() != before {
		t.Error("a refused add must change nothing")
	}
}

func TestSculptureAPI_TheSameSculptureTwice_IsAllowed(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	id := s.seedSculptureCatalog("sculpture_a")[0]

	for slot := 0; slot < 2; slot++ {
		resp, sculptures, errBody := s.addSculpture(roomID, id, s.token)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("copy %d: %d %v", slot, resp.StatusCode, errBody)
		}
		if got := sculptureAt(sculptures, slot); got != id {
			t.Errorf("slot %d holds %q", slot, got)
		}
	}
}

// MARK: - Removal leaves the slot empty

func TestSculptureAPI_RemoveLeavesTheSlotEmpty_AndTheFreedSlotIsReused(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	ids := s.seedSculptureCatalog("sculpture_a", "sculpture_b", "sculpture_c", "sculpture_d")
	for _, id := range ids[:3] {
		if resp, _, _ := s.addSculpture(roomID, id, s.token); resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed %s", id)
		}
	}

	resp, sculptures, errBody := s.removeSculpture(roomID, 1, s.token)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove: %d %v", resp.StatusCode, errBody)
	}
	if len(sculptures) != 2 {
		t.Fatalf("expected 2 sculptures, got %v", sculptures)
	}
	if sculptureAt(sculptures, 0) != ids[0] || sculptureAt(sculptures, 2) != ids[2] {
		t.Errorf("surviving sculptures must not move: %v", sculptures)
	}
	if sculptureAt(sculptures, 1) != "<empty>" {
		t.Errorf("slot 1 must be empty: %v", sculptures)
	}

	resp, sculptures, errBody = s.addSculpture(roomID, ids[3], s.token)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add after remove: %d %v", resp.StatusCode, errBody)
	}
	if sculptureAt(sculptures, 1) != ids[3] {
		t.Errorf("the freed slot 1 must be reused: %v", sculptures)
	}
	if s.sculptureCount(roomID) != 3 {
		t.Errorf("room holds %d, want 3", s.sculptureCount(roomID))
	}
}

func TestSculptureAPI_Rejections_ChangeNothing(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	id := s.seedSculptureCatalog("sculpture_a")[0]
	if resp, _, _ := s.addSculpture(roomID, id, s.token); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	before := s.snapshotOwnerState()

	t.Run("remove an empty slot", func(t *testing.T) {
		resp, _, errBody := s.removeSculpture(roomID, 2, s.token)
		if resp.StatusCode != http.StatusNotFound || errBody["code"] != "sculpture_not_in_room" {
			t.Errorf("got %d %v, want 404 sculpture_not_in_room", resp.StatusCode, errBody)
		}
	})
	t.Run("remove a slot outside the cap", func(t *testing.T) {
		resp, _, errBody := s.removeSculpture(roomID, museumdomain.MaxSculpturesPerRoom, s.token)
		if resp.StatusCode != http.StatusBadRequest || errBody["code"] != "invalid_sculpture_slot" {
			t.Errorf("got %d %v, want 400 invalid_sculpture_slot", resp.StatusCode, errBody)
		}
	})
	t.Run("non-numeric slot", func(t *testing.T) {
		resp, raw := s.do(http.MethodDelete, "/museum/me/rooms/"+roomID+"/sculptures/middle", nil, s.token)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("got %d %s, want 400", resp.StatusCode, raw)
		}
	})
	t.Run("unknown catalog id", func(t *testing.T) {
		resp, _, errBody := s.addSculpture(roomID, "sculpture_invented", s.token)
		if resp.StatusCode != http.StatusBadRequest || errBody["code"] != "unknown_sculpture" {
			t.Errorf("got %d %v, want 400 unknown_sculpture", resp.StatusCode, errBody)
		}
	})
	t.Run("empty catalog id", func(t *testing.T) {
		resp, _, errBody := s.addSculpture(roomID, "", s.token)
		if resp.StatusCode != http.StatusBadRequest || errBody["code"] != "unknown_sculpture" {
			t.Errorf("got %d %v, want 400 unknown_sculpture", resp.StatusCode, errBody)
		}
	})
	t.Run("malformed body", func(t *testing.T) {
		resp, _ := s.do(http.MethodPost, "/museum/me/rooms/"+roomID+"/sculptures", "not json", s.token)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("got %d, want 400", resp.StatusCode)
		}
	})
	t.Run("unknown room", func(t *testing.T) {
		if resp, _, _ := s.addSculpture("00000000-0000-4000-8000-000000000000", id, s.token); resp.StatusCode != http.StatusNotFound {
			t.Errorf("got %d, want 404", resp.StatusCode)
		}
	})

	if s.snapshotOwnerState() != before {
		t.Error("no refusal may change stored state")
	}
}

// MARK: - Concurrency: the cap holds

func TestSculptureAPI_CapHoldsUnderConcurrentAdds(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	ids := s.seedSculptureCatalog("sculpture_a", "sculpture_b", "sculpture_c", "sculpture_d", "sculpture_e")

	var wg sync.WaitGroup
	statuses := make(chan int, len(ids))
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			resp, _, _ := s.addSculpture(roomID, id, s.token)
			statuses <- resp.StatusCode
		}(id)
	}
	wg.Wait()
	close(statuses)

	won, refused := 0, 0
	for code := range statuses {
		switch code {
		case http.StatusCreated:
			won++
		case http.StatusConflict:
			refused++
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if won != museumdomain.MaxSculpturesPerRoom || refused != len(ids)-museumdomain.MaxSculpturesPerRoom {
		t.Fatalf("won=%d refused=%d; want %d and %d", won, refused, museumdomain.MaxSculpturesPerRoom, len(ids)-museumdomain.MaxSculpturesPerRoom)
	}
	if s.sculptureCount(roomID) != museumdomain.MaxSculpturesPerRoom {
		t.Fatalf("room holds %d sculptures, want exactly %d", s.sculptureCount(roomID), museumdomain.MaxSculpturesPerRoom)
	}

	rows, err := s.pool.Pool().Query(context.Background(), `SELECT slot_index FROM room_sculptures WHERE room_id = $1 ORDER BY slot_index`, roomID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	expect := 0
	for rows.Next() {
		var slot int
		_ = rows.Scan(&slot)
		if slot != expect {
			t.Errorf("slot %d, want %d — concurrent adds must still take the lowest free slots", slot, expect)
		}
		expect++
	}
}

func TestSculptureAPI_ConcurrentRemovalsOfOneSlot_ExactlyOneWins(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	ids := s.seedSculptureCatalog("sculpture_a", "sculpture_b")
	for _, id := range ids {
		if resp, _, _ := s.addSculpture(roomID, id, s.token); resp.StatusCode != http.StatusCreated {
			t.Fatal("seed")
		}
	}

	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, _, _ := s.removeSculpture(roomID, 0, s.token)
			statuses <- resp.StatusCode
		}()
	}
	wg.Wait()
	close(statuses)

	wins, gone := 0, 0
	for code := range statuses {
		switch code {
		case http.StatusOK:
			wins++
		case http.StatusNotFound:
			gone++
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if wins != 1 || gone != 1 {
		t.Fatalf("wins=%d gone=%d; want exactly one of each", wins, gone)
	}
	if s.sculptureCount(roomID) != 1 {
		t.Errorf("room holds %d, want 1", s.sculptureCount(roomID))
	}
}

// MARK: - Sculptures are independent of photographs

func TestSculptureAPI_DoesNotTouchPhotographsOrAssets(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed photos")
	}
	if resp, _, _ := s.setCaption(roomID, a.asset, "kept", s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("caption")
	}
	id := s.seedSculptureCatalog("sculpture_a")[0]
	photoStateBefore := s.snapshotOwnerState()

	if resp, _, _ := s.addSculpture(roomID, id, s.token); resp.StatusCode != http.StatusCreated {
		t.Fatal("add sculpture")
	}
	if resp, _, _ := s.removeSculpture(roomID, 0, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("remove sculpture")
	}

	if after := s.snapshotOwnerState(); after != photoStateBefore {
		t.Errorf("sculpture operations changed photo/asset state:\nbefore %+v\nafter  %+v", photoStateBefore, after)
	}
	if s.assetState(a.asset) != "committed" || s.assetState(b.asset) != "committed" {
		t.Error("no asset may change state for a sculpture")
	}
	if tickets := s.ticketAssetIDs(roomID); !equalStrings(tickets, []string{a.asset, b.asset}) {
		t.Errorf("photo delivery changed: %v", tickets)
	}
}

// MARK: - Degraded mode

func TestSculptureAPI_WithoutObjectStorage_StillWorks(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	id := s.seedSculptureCatalog("sculpture_a")[0]
	degraded := s.degradedMuseumServer()
	defer degraded.Close()

	resp, raw := s.doAgainst(degraded.URL, http.MethodPost, "/museum/me/rooms/"+roomID+"/sculptures",
		map[string]string{"catalog_id": id}, s.token)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("sculptures must not require object storage, got %d %s", resp.StatusCode, raw)
	}
	if s.sculptureCount(roomID) != 1 {
		t.Error("the sculpture must be placed")
	}
}
