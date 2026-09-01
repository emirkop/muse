package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	identitydomain "muse-backend/internal/identity/domain"
	museumdomain "muse-backend/internal/museum/domain"
)

type snapshot struct {
	Museum          string
	Rooms           string
	Slots           string
	Assets          string
	CollectionRooms string
	CollectionItems string
}

func (s *stack) snapshotOwnerState() snapshot {
	s.t.Helper()
	ctx := context.Background()
	q := func(sql string, args ...any) string {
		var out string
		if err := s.pool.Pool().QueryRow(ctx, sql, args...).Scan(&out); err != nil {
			s.t.Fatalf("snapshot: %v", err)
		}
		return out
	}
	return snapshot{
		Museum:          q(`SELECT coalesce(string_agg(id::text || '|' || style_id || '|' || privacy, ';' ORDER BY id), '') FROM museums WHERE account_id = $1`, s.accountID),
		Rooms:           q(`SELECT coalesce(string_agg(r.id::text || '|' || r.name || '|' || r.variant_id || '|' || r.privacy, ';' ORDER BY r.id), '') FROM rooms r JOIN museums m ON m.id = r.museum_id WHERE m.account_id = $1`, s.accountID),
		Slots:           q(`SELECT coalesce(string_agg(ps.id::text || '|' || ps.slot_index || '|' || ps.photo_asset_id::text || '|' || ps.caption, ';' ORDER BY ps.id), '') FROM room_photo_slots ps JOIN rooms r ON r.id = ps.room_id JOIN museums m ON m.id = r.museum_id WHERE m.account_id = $1`, s.accountID),
		Assets:          q(`SELECT coalesce(string_agg(id::text || '|' || state || '|' || storage_key, ';' ORDER BY id), '') FROM assets WHERE account_id = $1`, s.accountID),
		CollectionRooms: q(`SELECT coalesce(string_agg(id::text || '|' || name || '|' || coalesce(category_id, '-') || '|' || coalesce(design_id, '-') || '|' || current_tier, ';' ORDER BY id), '') FROM collection_rooms WHERE account_id = $1`, s.accountID),
		CollectionItems: q(`SELECT coalesce(string_agg(i.id::text || '|' || i.slot_index || '|' || i.catalog_model_id, ';' ORDER BY i.id), '') FROM collection_items i JOIN collection_rooms c ON c.id = i.collection_room_id WHERE c.account_id = $1`, s.accountID),
	}
}

func (s *stack) strangerToken() string {
	s.t.Helper()
	var strangerID string
	if err := s.pool.Pool().QueryRow(context.Background(), `INSERT INTO accounts (display_name) VALUES ('stranger') RETURNING id`).Scan(&strangerID); err != nil {
		s.t.Fatal(err)
	}
	token, _, err := s.signer.Sign(identitydomain.AccountID(strangerID), identitydomain.SessionID("stranger-sess"))
	if err != nil {
		s.t.Fatal(err)
	}
	return token
}

func (s *stack) reorder(roomID string, order []string, token string) (*http.Response, []slotJSON, map[string]any) {
	s.t.Helper()
	resp, raw := s.do(http.MethodPut, "/museum/me/rooms/"+roomID+"/photo-order", map[string]any{"photo_asset_ids": order}, token)
	var ok struct {
		PhotoSlots []slotJSON `json:"photo_slots"`
	}
	var errBody map[string]any
	if resp.StatusCode == http.StatusOK {
		_ = json.Unmarshal(raw, &ok)
	} else {
		_ = json.Unmarshal(raw, &errBody)
	}
	return resp, ok.PhotoSlots, errBody
}

func orderFrom(slots []slotJSON) []string {
	sorted := append([]slotJSON(nil), slots...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].SlotIndex < sorted[j].SlotIndex })
	out := make([]string, 0, len(sorted))
	for i, s := range sorted {
		if s.SlotIndex != i {
			return nil
		}
		out = append(out, s.PhotoAssetID)
	}
	return out
}

// MARK: -

func TestSecurityGate1_NonOwnerCannotMutateAnything(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed assign")
	}
	pending := s.uploaded(newPhoto(t, 800, 600, "pending"))
	sculptureID := s.seedSculptureCatalog("sculpture_gate")[0]
	if resp, _, _ := s.addSculpture(roomID, sculptureID, s.token); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed sculpture")
	}

	before := s.snapshotOwnerState()

	type attempt struct {
		name, method, path                   string
		body                                 any
		wantWithoutMuseum, wantWithOwnMuseum int
	}
	attempts := []attempt{
		{"change style", http.MethodPatch, "/museum/me/style", map[string]string{"style_id": "style_gothic"}, http.StatusNotFound, http.StatusOK},
		{"change privacy", http.MethodPatch, "/museum/me/privacy", map[string]string{"privacy": "public"}, http.StatusNotFound, http.StatusOK},
		{"create room", http.MethodPost, "/museum/me/rooms", map[string]string{"name": "X", "variant_id": "style_gothic_variant_Hall"}, http.StatusNotFound, http.StatusCreated},
		{"update owner's room", http.MethodPatch, "/museum/me/rooms/" + roomID, map[string]string{"name": "Hijacked", "variant_id": "style_modern_variant_Hall", "privacy": "public"}, http.StatusNotFound, http.StatusNotFound},
		{"delete owner's room", http.MethodDelete, "/museum/me/rooms/" + roomID, nil, http.StatusNotFound, http.StatusNotFound},
		{"assign into owner's room", http.MethodPost, "/museum/me/rooms/" + roomID + "/photos", map[string]any{"asset_ids": []string{pending.asset}}, http.StatusNotFound, http.StatusNotFound},
		{"reorder owner's room", http.MethodPut, "/museum/me/rooms/" + roomID + "/photo-order", map[string]any{"photo_asset_ids": []string{b.asset, a.asset}}, http.StatusNotFound, http.StatusNotFound},
		{"read owner's photo urls", http.MethodGet, "/museum/me/rooms/" + roomID + "/photo-urls", nil, http.StatusNotFound, http.StatusNotFound},
		{"caption owner's photo", http.MethodPut, "/museum/me/rooms/" + roomID + "/photos/" + a.asset + "/caption", map[string]string{"caption": "hijacked"}, http.StatusNotFound, http.StatusNotFound},
		{"replace owner's photo", http.MethodPost, "/museum/me/rooms/" + roomID + "/photos/" + a.asset + "/replacement", map[string]string{"asset_id": pending.asset}, http.StatusNotFound, http.StatusNotFound},
		{"delete owner's photo", http.MethodDelete, "/museum/me/rooms/" + roomID + "/photos/" + a.asset, nil, http.StatusNotFound, http.StatusNotFound},
		{"add sculpture to owner's room", http.MethodPost, "/museum/me/rooms/" + roomID + "/sculptures", map[string]string{"catalog_id": sculptureID}, http.StatusNotFound, http.StatusNotFound},
		{"remove owner's sculpture", http.MethodDelete, "/museum/me/rooms/" + roomID + "/sculptures/0", nil, http.StatusNotFound, http.StatusNotFound},
	}

	strangerWithoutMuseum := s.strangerToken()
	for _, at := range attempts {
		t.Run("stranger without museum: "+at.name, func(t *testing.T) {
			resp, raw := s.do(at.method, at.path, at.body, strangerWithoutMuseum)
			if resp.StatusCode != at.wantWithoutMuseum {
				t.Errorf("got %d, want %d: %s", resp.StatusCode, at.wantWithoutMuseum, raw)
			}
			if after := s.snapshotOwnerState(); after != before {
				t.Errorf("owner state changed after a stranger's %s:\nbefore %+v\nafter  %+v", at.name, before, after)
			}
		})
	}

	strangerWithMuseum := s.strangerToken()
	if resp, _ := s.do(http.MethodPost, "/museum", map[string]string{"style_id": "style_modern"}, strangerWithMuseum); resp.StatusCode != http.StatusCreated {
		t.Fatalf("stranger creating their own museum: %d", resp.StatusCode)
	}
	if after := s.snapshotOwnerState(); after != before {
		t.Fatal("a stranger creating their own Museum must not touch the owner's state")
	}
	for _, at := range attempts {
		t.Run("stranger with own museum: "+at.name, func(t *testing.T) {
			resp, raw := s.do(at.method, at.path, at.body, strangerWithMuseum)
			if resp.StatusCode != at.wantWithOwnMuseum {
				t.Errorf("got %d, want %d: %s", resp.StatusCode, at.wantWithOwnMuseum, raw)
			}
			if after := s.snapshotOwnerState(); after != before {
				t.Errorf("owner state changed after a museum-owning stranger's %s:\nbefore %+v\nafter  %+v", at.name, before, after)
			}
		})
	}

	for _, at := range attempts {
		t.Run("unauthenticated: "+at.name, func(t *testing.T) {
			resp, _ := s.do(at.method, at.path, at.body, "")
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("got %d, want 401", resp.StatusCode)
			}
			if after := s.snapshotOwnerState(); after != before {
				t.Errorf("owner state changed after an unauthenticated %s", at.name)
			}
		})
	}
}

func TestSecurityGate1_RoomPayloadCarriesNoEditAffordanceData(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()

	resp, raw := s.do(http.MethodGet, "/museum/me/rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get room: %d", resp.StatusCode)
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)

	allowed := map[string]bool{"id": true, "name": true, "variant_id": true, "privacy": true, "photo_slots": true, "sculptures": true}
	for key := range payload {
		if !allowed[key] {
			t.Errorf("unexpected key %q in Room payload — edit affordance data must never be sent", key)
		}
	}
	for _, forbidden := range []string{"can_edit", "is_owner", "editable", "permissions", "owner_id", "account_id"} {
		if _, present := payload[forbidden]; present {
			t.Errorf("Room payload must not carry %q", forbidden)
		}
	}
}

// MARK: - Reorder contract ( / ), whole stack

func TestReorderAPI_OwnerSwapsTwoPhotos(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 600, 800, "b"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	before := s.snapshotOwnerState()

	resp, slots, errBody := s.reorder(roomID, []string{b.asset, a.asset}, s.token)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reorder: %d %v", resp.StatusCode, errBody)
	}
	got := orderFrom(slots)
	if len(got) != 2 || got[0] != b.asset || got[1] != a.asset {
		t.Fatalf("order = %v, want [%s %s]", got, b.asset, a.asset)
	}
	after := s.snapshotOwnerState()
	if after.Assets != before.Assets {
		t.Error("reordering must not touch asset rows (no upload, no state change)")
	}
	if after.Slots == before.Slots {
		t.Error("reordering must change slot indices")
	}
}

func TestReorderAPI_ArbitraryPermutationOf28_AndCrossWallMove(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	var all []string
	for i := 0; i < museumdomain.MaxPhotosPerRoom; i++ {
		all = append(all, s.uploaded(newPhoto(t, 640, 480, fmt.Sprintf("p-%d", i))).asset)
	}
	if resp, _, _ := s.assign(roomID, all); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed 28")
	}

	crossWall := append([]string(nil), all...)
	crossWall[0], crossWall[27] = crossWall[27], crossWall[0]
	resp, slots, errBody := s.reorder(roomID, crossWall, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cross-wall reorder: %d %v", resp.StatusCode, errBody)
	}
	if got := orderFrom(slots); got == nil || got[0] != all[27] || got[27] != all[0] {
		t.Fatalf("cross-wall move not applied: %v", got)
	}

	want := append([]string(nil), all...)
	rand.New(rand.NewSource(38)).Shuffle(len(want), func(i, j int) { want[i], want[j] = want[j], want[i] })
	resp, slots, errBody = s.reorder(roomID, want, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("permutation: %d %v", resp.StatusCode, errBody)
	}
	got := orderFrom(slots)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d = %s, want %s", i, got[i], want[i])
		}
	}
	if s.slotCount(roomID) != museumdomain.MaxPhotosPerRoom {
		t.Error("row count must be unchanged")
	}
}

func TestReorderAPI_SinglePhoto_IsAccepted(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "only"))
	if resp, _, _ := s.assign(roomID, []string{a.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	resp, slots, errBody := s.reorder(roomID, []string{a.asset}, s.token)
	if resp.StatusCode != http.StatusOK || len(slots) != 1 {
		t.Fatalf("single-photo identity order must succeed: %d %v", resp.StatusCode, errBody)
	}
}

func TestReorderAPI_RejectsMalformedAndMismatchedOrders(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	c := s.uploaded(newPhoto(t, 800, 600, "c"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	before := s.snapshotOwnerState()

	cases := []struct {
		name     string
		order    []string
		want     int
		wantCode string
	}{
		{"empty", []string{}, http.StatusBadRequest, "invalid_order"},
		{"duplicate", []string{a.asset, a.asset}, http.StatusBadRequest, "invalid_order"},
		{"missing", []string{a.asset}, http.StatusConflict, "order_mismatch"},
		{"foreign", []string{a.asset, c.asset}, http.StatusConflict, "order_mismatch"},
		{"too many", []string{a.asset, b.asset, c.asset}, http.StatusConflict, "order_mismatch"},
		{"unknown id", []string{a.asset, "00000000-0000-4000-8000-000000000000"}, http.StatusConflict, "order_mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, _, errBody := s.reorder(roomID, tc.order, s.token)
			if resp.StatusCode != tc.want || errBody["code"] != tc.wantCode {
				t.Errorf("got %d %v, want %d %s", resp.StatusCode, errBody, tc.want, tc.wantCode)
			}
			if s.snapshotOwnerState() != before {
				t.Error("a refused reorder must change nothing")
			}
		})
	}
	if resp, _ := s.do(http.MethodPut, "/museum/me/rooms/"+roomID+"/photo-order", "not json", s.token); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body: got %d, want 400", resp.StatusCode)
	}
	if resp, _, _ := s.reorder("00000000-0000-4000-8000-000000000000", []string{a.asset}, s.token); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown room: got %d, want 404", resp.StatusCode)
	}
}

func TestReorderAPI_ConcurrentReorders_CannotCorruptOrdering(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	var all []string
	for i := 0; i < 12; i++ {
		all = append(all, s.uploaded(newPhoto(t, 640, 480, fmt.Sprintf("c-%d", i))).asset)
	}
	if resp, _, _ := s.assign(roomID, all); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}

	const racers = 10
	permutations := make([][]string, racers)
	for i := range permutations {
		p := append([]string(nil), all...)
		rand.New(rand.NewSource(int64(100+i))).Shuffle(len(p), func(a, b int) { p[a], p[b] = p[b], p[a] })
		permutations[i] = p
	}

	var wg sync.WaitGroup
	statuses := make(chan int, racers)
	for _, p := range permutations {
		wg.Add(1)
		go func(order []string) {
			defer wg.Done()
			resp, _, _ := s.reorder(roomID, order, s.token)
			statuses <- resp.StatusCode
		}(p)
	}
	wg.Wait()
	close(statuses)
	for code := range statuses {
		if code != http.StatusOK {
			t.Errorf("a concurrent reorder failed with %d; serialization must make each succeed", code)
		}
	}

	rows, err := s.pool.Pool().Query(context.Background(), `SELECT slot_index, photo_asset_id FROM room_photo_slots WHERE room_id = $1 ORDER BY slot_index`, roomID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	final := make([]string, 0, 12)
	expectIdx := 0
	for rows.Next() {
		var idx int
		var asset string
		_ = rows.Scan(&idx, &asset)
		if idx != expectIdx {
			t.Fatalf("index %d, want %d — ordering corrupted", idx, expectIdx)
		}
		expectIdx++
		final = append(final, asset)
	}
	if len(final) != 12 {
		t.Fatalf("expected 12 slots, got %d", len(final))
	}
	matched := false
	for _, p := range permutations {
		if equalStrings(p, final) {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("final order %v is not any submitted permutation", final)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// MARK: - Caption contract, whole stack

func (s *stack) setCaption(roomID, photoAssetID, caption, token string) (*http.Response, []slotJSON, map[string]any) {
	s.t.Helper()
	resp, raw := s.do(http.MethodPut, "/museum/me/rooms/"+roomID+"/photos/"+photoAssetID+"/caption",
		map[string]string{"caption": caption}, token)
	var ok struct {
		PhotoSlots []slotJSON `json:"photo_slots"`
	}
	var errBody map[string]any
	if resp.StatusCode == http.StatusOK {
		_ = json.Unmarshal(raw, &ok)
	} else {
		_ = json.Unmarshal(raw, &errBody)
	}
	return resp, ok.PhotoSlots, errBody
}

func captionFor(slots []slotJSON, assetID string) string {
	for _, slot := range slots {
		if slot.PhotoAssetID == assetID {
			return slot.Caption
		}
	}
	return "<absent>"
}

func TestCaptionAPI_AddUpdateAndClear_RoundTrips(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}

	_, slots, _ := s.reorder(roomID, []string{a.asset, b.asset}, s.token)
	if captionFor(slots, a.asset) != "" {
		t.Errorf("photographs must have no caption by default, got %q", captionFor(slots, a.asset))
	}

	resp, slots, errBody := s.setCaption(roomID, a.asset, "Trabzon, 1998", s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add: %d %v", resp.StatusCode, errBody)
	}
	if captionFor(slots, a.asset) != "Trabzon, 1998" {
		t.Errorf("add: caption = %q", captionFor(slots, a.asset))
	}
	if captionFor(slots, b.asset) != "" {
		t.Errorf("add: a neighbour gained a caption: %q", captionFor(slots, b.asset))
	}

	resp, slots, _ = s.setCaption(roomID, a.asset, "Trabzon, 1999", s.token)
	if resp.StatusCode != http.StatusOK || captionFor(slots, a.asset) != "Trabzon, 1999" {
		t.Fatalf("update: %d %q", resp.StatusCode, captionFor(slots, a.asset))
	}

	resp, slots, _ = s.setCaption(roomID, a.asset, "", s.token)
	if resp.StatusCode != http.StatusOK || captionFor(slots, a.asset) != "" {
		t.Fatalf("clear: %d %q", resp.StatusCode, captionFor(slots, a.asset))
	}

	resp, raw := s.do(http.MethodGet, "/museum/me/rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get room: %d", resp.StatusCode)
	}
	var room struct {
		PhotoSlots []slotJSON `json:"photo_slots"`
	}
	_ = json.Unmarshal(raw, &room)
	if captionFor(room.PhotoSlots, a.asset) != "" {
		t.Error("cleared caption did not persist")
	}
}

func TestCaptionAPI_IsIdempotent_AndDoesNotTouchOrderingOrAssets(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	var all []string
	for i := 0; i < 5; i++ {
		all = append(all, s.uploaded(newPhoto(t, 640, 480, fmt.Sprintf("p-%d", i))).asset)
	}
	if resp, _, _ := s.assign(roomID, all); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	if resp, _, _ := s.setCaption(roomID, all[2], "middle", s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("first caption")
	}
	before := s.snapshotOwnerState()

	for _, text := range []string{"middle", "  middle  "} {
		resp, slots, _ := s.setCaption(roomID, all[2], text, s.token)
		if resp.StatusCode != http.StatusOK || captionFor(slots, all[2]) != "middle" {
			t.Fatalf("%q: %d %q", text, resp.StatusCode, captionFor(slots, all[2]))
		}
	}
	if after := s.snapshotOwnerState(); after != before {
		t.Error("an unchanged caption must not alter any stored state")
	}

	if resp, slots, _ := s.setCaption(roomID, all[0], "focal", s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("caption")
	} else if got := orderFrom(slots); got == nil || !equalStrings(got, all) {
		t.Errorf("ordering changed: %v want %v", got, all)
	}
	after := s.snapshotOwnerState()
	if after.Assets != before.Assets {
		t.Error("a caption edit must not touch asset rows (no upload, no state change)")
	}
}

func TestCaptionAPI_SurvivesReorder_AttachedToItsPhotograph(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	var all []string
	for i := 0; i < 4; i++ {
		all = append(all, s.uploaded(newPhoto(t, 640, 480, fmt.Sprintf("r-%d", i))).asset)
	}
	if resp, _, _ := s.assign(roomID, all); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	if resp, _, _ := s.setCaption(roomID, all[0], "the first one", s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("caption")
	}

	reordered := []string{all[1], all[2], all[3], all[0]}
	resp, slots, errBody := s.reorder(roomID, reordered, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reorder: %d %v", resp.StatusCode, errBody)
	}

	if captionFor(slots, all[0]) != "the first one" {
		t.Errorf("the caption did not follow its photograph: %q", captionFor(slots, all[0]))
	}
	if got := orderFrom(slots); !equalStrings(got, reordered) {
		t.Errorf("order = %v", got)
	}
	resp, slots, _ = s.setCaption(roomID, all[0], "still mine", s.token)
	if resp.StatusCode != http.StatusOK || captionFor(slots, all[0]) != "still mine" {
		t.Fatalf("edit after reorder: %d %q", resp.StatusCode, captionFor(slots, all[0]))
	}
	if captionFor(slots, all[1]) != "" {
		t.Errorf("the photograph now at slot 0 was wrongly mutated: %q", captionFor(slots, all[1]))
	}
}

func TestCaptionAPI_RejectsForeignPhotoTooLongAndMalformed(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	orphan := s.uploaded(newPhoto(t, 800, 600, "orphan"))
	if resp, _, _ := s.assign(roomID, []string{a.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	before := s.snapshotOwnerState()

	cases := []struct {
		name, assetID, caption string
		want                   int
		wantCode               string
	}{
		{"foreign photo", orphan.asset, "x", http.StatusNotFound, "photo_not_in_room"},
		{"unknown photo", "00000000-0000-4000-8000-000000000000", "x", http.StatusNotFound, "photo_not_in_room"},
		{"too long", a.asset, strings.Repeat("x", museumdomain.MaxCaptionBytes+1), http.StatusBadRequest, "caption_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, _, errBody := s.setCaption(roomID, tc.assetID, tc.caption, s.token)
			if resp.StatusCode != tc.want || errBody["code"] != tc.wantCode {
				t.Errorf("got %d %v, want %d %s", resp.StatusCode, errBody, tc.want, tc.wantCode)
			}
			if s.snapshotOwnerState() != before {
				t.Error("a refused caption must change nothing")
			}
		})
	}

	if resp, _, errBody := s.setCaption(roomID, a.asset, strings.Repeat("x", museumdomain.MaxCaptionBytes), s.token); resp.StatusCode != http.StatusOK {
		t.Errorf("a caption at the bound must be accepted: %d %v", resp.StatusCode, errBody)
	}

	if resp, _ := s.do(http.MethodPut, "/museum/me/rooms/"+roomID+"/photos/"+a.asset+"/caption", "not json", s.token); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body: got %d, want 400", resp.StatusCode)
	}
	if resp, _, _ := s.setCaption("00000000-0000-4000-8000-000000000000", a.asset, "x", s.token); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown room: got %d, want 404", resp.StatusCode)
	}
}
