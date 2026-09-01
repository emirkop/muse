package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	catalogapp "muse-backend/internal/catalog/application"
	catalogdomain "muse-backend/internal/catalog/domain"
)

// MARK: - Harness

func (s *stack) addItem(roomID, modelID string) (*http.Response, []byte) {
	s.t.Helper()
	return s.do(http.MethodPost, "/collection-rooms/"+roomID+"/items",
		map[string]string{"catalog_model_id": modelID}, s.token)
}

func (s *stack) mustAddItem(roomID, modelID string) collectionRoomJSON {
	s.t.Helper()
	resp, body := s.addItem(roomID, modelID)
	if resp.StatusCode != http.StatusCreated {
		s.t.Fatalf("add item: %d %s", resp.StatusCode, body)
	}
	return decodeCollectionRoom(s.t, body)
}

func (s *stack) placeItem(roomID, itemID string, slotIndex int) (*http.Response, []byte) {
	s.t.Helper()
	return s.do(http.MethodPut,
		fmt.Sprintf("/collection-rooms/%s/items/%s/slot", roomID, itemID),
		map[string]any{"slot_index": slotIndex}, s.token)
}

func (s *stack) mustPlaceItem(roomID, itemID string, slotIndex int) collectionRoomJSON {
	s.t.Helper()
	resp, body := s.placeItem(roomID, itemID, slotIndex)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("place item at %d: %d %s", slotIndex, resp.StatusCode, body)
	}
	return decodeCollectionRoom(s.t, body)
}

func (s *stack) readItems(roomID string) (bySlot map[int]string, slotOf map[string]int) {
	s.t.Helper()
	resp, body := s.do(http.MethodGet, "/collection-rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("read room: %d %s", resp.StatusCode, body)
	}
	room := decodeCollectionRoom(s.t, body)
	bySlot = map[int]string{}
	slotOf = map[string]int{}
	for _, item := range room.Items {
		bySlot[item.SlotIndex] = item.ID
		slotOf[item.ID] = item.SlotIndex
	}
	return bySlot, slotOf
}

func (s *stack) itemsInOrder(roomID string) []string {
	s.t.Helper()
	resp, body := s.do(http.MethodGet, "/collection-rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("read room: %d %s", resp.StatusCode, body)
	}
	ids := []string{}
	for _, item := range decodeCollectionRoom(s.t, body).Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func (s *stack) fillToItems(roomID string, count int) collectionRoomJSON {
	s.t.Helper()
	var room collectionRoomJSON
	for index := 1; index <= count; index++ {
		if tier := requiredTierFor(s.t, index); tier > s.currentTier(roomID) {
			if resp, body := s.ratchet(roomID, tier); resp.StatusCode != http.StatusOK {
				s.t.Fatalf("ratchet to %d: %d %s", tier, resp.StatusCode, body)
			}
		}
		room = s.mustAddItem(roomID, syntheticModel)
	}
	return room
}

func committedDesignFixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "..", "assets", "dev_fixtures", "bundles", "dev_fixture_collection_design", "v1")
}

func (s *stack) publishCommittedDesignFixture(t *testing.T) catalogapp.PublishResult {
	t.Helper()
	dir := committedDesignFixtureDir(t)
	raw, err := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if err != nil {
		t.Fatalf("read committed bundle.json: %v", err)
	}
	var spec struct {
		BundleID      string `json:"bundle_id"`
		Version       int    `json:"version"`
		Kind          string `json:"kind"`
		Format        string `json:"format"`
		MinAppVersion int    `json:"min_app_version"`
		Files         []struct {
			AssetID     string `json:"asset_id"`
			Role        string `json:"role"`
			Path        string `json:"path"`
			ContentType string `json:"content_type"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse committed bundle.json: %v", err)
	}
	request := catalogapp.PublishRequest{
		BundleID: spec.BundleID, Version: spec.Version, Kind: catalogdomain.BundleKind(spec.Kind),
		Format: spec.Format, MinAppVersion: spec.MinAppVersion,
	}
	for _, file := range spec.Files {
		body, err := os.ReadFile(filepath.Join(dir, file.Path))
		if err != nil {
			t.Fatalf("read committed %s: %v", file.Path, err)
		}
		request.Files = append(request.Files, newPublishSource(file.AssetID, catalogdomain.AssetRole(file.Role), file.ContentType, string(body)))
	}
	result, err := s.publisher.Publish(context.Background(), request)
	if err != nil {
		t.Fatalf("publish the committed fixture Design: %v", err)
	}
	return result
}

func (s *stack) roomWithPublishedDesign(t *testing.T, name string) string {
	t.Helper()
	s.publishCommittedDesignFixture(t)
	return s.roomWithDesign(name)
}

func (s *stack) derivedCapacities(t *testing.T, bundleID string, version int) []int {
	t.Helper()
	rows, err := s.pool.Pool().Query(context.Background(),
		`SELECT cumulative_capacity FROM asset_bundle_tier_capacities WHERE bundle_id = $1 AND version = $2 ORDER BY tier`,
		bundleID, version)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var capacities []int
	for rows.Next() {
		var capacity int
		if err := rows.Scan(&capacity); err != nil {
			t.Fatal(err)
		}
		capacities = append(capacities, capacity)
	}
	return capacities
}

func (s *stack) roomState(t *testing.T, roomID string) string {
	t.Helper()
	var state string
	err := s.pool.Pool().QueryRow(context.Background(), `
		SELECT r.current_tier::text || '|' || coalesce((
			SELECT string_agg(i.id::text || '@' || i.slot_index || ':' || i.catalog_model_id, ';' ORDER BY i.slot_index)
			FROM collection_items i WHERE i.collection_room_id = r.id), '')
		FROM collection_rooms r WHERE r.id = $1
	`, roomID).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// MARK: - VERIFY 1 — a selected Model creates a real item

func TestSelectedCatalogModelCreatesARealItem(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")

	room := s.mustAddItem(roomID, syntheticModel)
	if len(room.Items) != 1 {
		t.Fatalf("room holds %d items after one add, want 1", len(room.Items))
	}
	item := room.Items[0]
	if item.CatalogModelID != syntheticModel {
		t.Fatalf("item references %q, want %q", item.CatalogModelID, syntheticModel)
	}
	if item.SlotIndex != 0 {
		t.Fatalf("first item landed at slot %d, want 0", item.SlotIndex)
	}
	if item.ID == "" {
		t.Fatal("item has no id")
	}

	bySlot, _ := s.readItems(roomID)
	if bySlot[0] != item.ID {
		t.Fatalf("slot 0 holds %q on re-read, want %q", bySlot[0], item.ID)
	}

	if tier := s.currentTier(roomID); tier != 1 {
		t.Fatalf("tier is %d after one item, want 1 — placement must not expand on its own", tier)
	}
}

func TestTheSameModelMayBePlacedTwice(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")

	s.mustAddItem(roomID, syntheticModel)
	room := s.mustAddItem(roomID, syntheticModel)

	if len(room.Items) != 2 {
		t.Fatalf("room holds %d items, want 2", len(room.Items))
	}
	if room.Items[0].ID == room.Items[1].ID {
		t.Fatal("two placements produced one item")
	}
	if room.Items[0].SlotIndex != 0 || room.Items[1].SlotIndex != 1 {
		t.Fatalf("slots are %d and %d, want 0 and 1", room.Items[0].SlotIndex, room.Items[1].SlotIndex)
	}
}

// MARK: - VERIFY 2 — an unknown Model is rejected

func TestUnknownOrForeignModelIsRejected(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")

	for _, testCase := range []struct {
		name    string
		modelID string
	}{
		{"nonexistent", "dev-fixture:model-does-not-exist"},
		{"empty", ""},
		{"real but wrong category", "dev-fixture:model-racer"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resp, body := s.addItem(roomID, testCase.modelID)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("add %q → %d %s, want 400", testCase.modelID, resp.StatusCode, body)
			}
		})
	}

	bySlot, _ := s.readItems(roomID)
	if len(bySlot) != 0 {
		t.Fatalf("room holds %d items after only refusals", len(bySlot))
	}

	_, err := s.pool.Pool().Exec(context.Background(), `
		INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id)
		VALUES ($1, 99, 'dev-fixture:model-does-not-exist')
	`, roomID)
	if err == nil {
		t.Fatal("an unknown catalog_model_id was accepted by the database")
	}
}

// MARK: - VERIFY 3 — the first available reached slot is assigned

func TestPlacementTakesTheLowestFreeSlot(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")

	first := s.mustAddItem(roomID, syntheticModel).Items[0].ID
	s.mustAddItem(roomID, syntheticModel)
	s.mustAddItem(roomID, syntheticModel)

	_, slotOf := s.readItems(roomID)
	if slotOf[first] != 0 {
		t.Fatalf("first item is at slot %d, want 0", slotOf[first])
	}

	s.mustPlaceItem(roomID, first, 3)
	bySlot, _ := s.readItems(roomID)
	if _, occupied := bySlot[0]; occupied {
		t.Fatal("slot 0 was refilled by a cascade — rule 4 forbids one")
	}

	room := s.mustAddItem(roomID, syntheticModel)
	_, slotOf = s.readItems(roomID)
	placed := ""
	for _, item := range room.Items {
		if item.SlotIndex == 0 {
			placed = item.ID
		}
	}
	if placed == "" {
		t.Fatalf("the next item did not take the free slot 0; slots are %v", slotOf)
	}
	if len(room.Items) != 4 {
		t.Fatalf("room holds %d items, want 4", len(room.Items))
	}
}

// MARK: - VERIFY 4 — crossing a capacity uses the ratchet first

func TestCrossingCapacityRatchetsBeforeTheNewSlotIsUsed(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")

	s.fillToItems(roomID, 4)
	if tier := s.currentTier(roomID); tier != 1 {
		t.Fatalf("tier is %d at 4 items, want 1 — the fixture's tier 1 holds exactly four", tier)
	}
	before := s.roomState(t, roomID)

	if required := requiredTierFor(t, 5); required != 2 {
		t.Fatalf("required tier for 5 items is %d, want 2", required)
	}
	resp, body := s.addItem(roomID, syntheticModel)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"tier_capacity_reached"`) {
		t.Fatalf("add into a full tier → %d %s, want 400 tier_capacity_reached", resp.StatusCode, body)
	}
	if s.roomState(t, roomID) != before {
		t.Fatal("a refused placement changed the Room")
	}
	if tier := s.currentTier(roomID); tier != 1 {
		t.Fatal("a refused placement moved the tier")
	}

	if resp, body := s.ratchet(roomID, 2); resp.StatusCode != http.StatusOK {
		t.Fatalf("ratchet: %d %s", resp.StatusCode, body)
	}
	if tier := s.currentTier(roomID); tier != 2 {
		t.Fatalf("tier is %d after ratcheting, want 2", tier)
	}

	room := s.mustAddItem(roomID, syntheticModel)
	if len(room.Items) != 5 {
		t.Fatalf("room holds %d items, want 5", len(room.Items))
	}
	bySlot, _ := s.readItems(roomID)
	if bySlot[4] == "" {
		t.Fatalf("slot 4 is empty after ratcheting; slots are %v", bySlot)
	}
}

// MARK: - VERIFY 5 — an occupied target swaps, atomically

func TestOccupiedTargetSwapsTheTwoItems(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.fillToItems(roomID, 3)

	bySlot, _ := s.readItems(roomID)
	a, b, c := bySlot[0], bySlot[1], bySlot[2]

	room := s.mustPlaceItem(roomID, a, 2)

	_, slotOf := s.readItems(roomID)
	if slotOf[a] != 2 || slotOf[c] != 0 {
		t.Fatalf("after swapping 0↔2: a=%d c=%d, want 2 and 0", slotOf[a], slotOf[c])
	}
	if slotOf[b] != 1 {
		t.Fatalf("the uninvolved item moved to slot %d, want 1", slotOf[b])
	}
	if len(room.Items) != 3 {
		t.Fatalf("room holds %d items after a swap, want 3", len(room.Items))
	}

	if slotOf[a] == slotOf[c] {
		t.Fatal("two items share a slot")
	}

	before := s.itemsInOrder(roomID)
	if resp, body := s.placeItem(roomID, a, 2); resp.StatusCode != http.StatusOK {
		t.Fatalf("re-placing at the same slot: %d %s", resp.StatusCode, body)
	}
	if after := s.itemsInOrder(roomID); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("a no-op placement changed the order: %v → %v", before, after)
	}
}

// MARK: - VERIFY 6 — an empty target moves, without shifting anything

func TestEmptyTargetMovesWithoutDisturbingOtherItems(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.fillToItems(roomID, 3)

	bySlot, _ := s.readItems(roomID)
	a, b, c := bySlot[0], bySlot[1], bySlot[2]

	s.mustPlaceItem(roomID, b, 3)

	after, slotOf := s.readItems(roomID)
	if slotOf[b] != 3 {
		t.Fatalf("moved item is at slot %d, want 3", slotOf[b])
	}
	if slotOf[a] != 0 || slotOf[c] != 2 {
		t.Fatalf("unrelated items moved: a=%d c=%d, want 0 and 2", slotOf[a], slotOf[c])
	}
	if _, occupied := after[1]; occupied {
		t.Fatal("slot 1 was backfilled — a move must leave the hole it made")
	}
	if len(after) != 3 {
		t.Fatalf("%d items after a move, want 3", len(after))
	}
}

// MARK: - VERIFY 7 — cross-tier swap works between reached tiers

func TestCrossTierSwapWorksBetweenReachedTiers(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")

	s.fillToItems(roomID, 6)
	if tier := s.currentTier(roomID); tier != 2 {
		t.Fatalf("tier is %d at 6 items, want 2", tier)
	}

	bySlot, _ := s.readItems(roomID)
	tierOneItem := bySlot[1]
	tierTwoItem := bySlot[5]

	s.mustPlaceItem(roomID, tierOneItem, 5)

	_, slotOf := s.readItems(roomID)
	if slotOf[tierOneItem] != 5 || slotOf[tierTwoItem] != 1 {
		t.Fatalf("cross-tier swap did not exchange: %d and %d, want 5 and 1",
			slotOf[tierOneItem], slotOf[tierTwoItem])
	}
	if tier := s.currentTier(roomID); tier != 2 {
		t.Fatalf("tier is %d after a cross-tier swap, want 2", tier)
	}
}

// MARK: - VERIFY 8 — a future-tier drop cannot expand the Room, and is refused

func TestUnauthoredSlotIsRejectedAndNothingChanges(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.fillToItems(roomID, 2)

	bySlot, _ := s.readItems(roomID)
	item := bySlot[0]
	before := s.roomState(t, roomID)

	for _, slot := range []int{18, 100, 4096} {
		resp, body := s.placeItem(roomID, item, slot)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"slot_not_available"`) {
			t.Fatalf("placement at unauthored slot %d → %d %s, want 400 slot_not_available", slot, resp.StatusCode, body)
		}
	}
	if resp, body := s.placeItem(roomID, item, -1); resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"invalid_slot_index"`) {
		t.Fatalf("negative slot → %d %s, want 400 invalid_slot_index", resp.StatusCode, body)
	}

	if after := s.roomState(t, roomID); after != before {
		t.Fatalf("a rejected drop changed the Room:\nbefore %s\nafter  %s", before, after)
	}
	if tier := s.currentTier(roomID); tier != 1 {
		t.Fatalf("tier moved to %d — no placement path may expand a Room", tier)
	}
}

func TestFutureTierEmptySlotIsRejectedServerSide(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.fillToItems(roomID, 3)

	bySlot, _ := s.readItems(roomID)
	item := bySlot[1]
	before := s.roomState(t, roomID)

	for _, futureSlot := range []int{4, 7, 9, 10, 17} {
		resp, body := s.placeItem(roomID, item, futureSlot)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"slot_not_available"`) {
			t.Fatalf("future-tier slot %d → %d %s, want 400 slot_not_available", futureSlot, resp.StatusCode, body)
		}
	}
	if after := s.roomState(t, roomID); after != before {
		t.Fatalf("a rejected future-tier drop changed the Room:\nbefore %s\nafter  %s", before, after)
	}
	if tier := s.currentTier(roomID); tier != 1 {
		t.Fatalf("tier is %d after rejected drops, want 1", tier)
	}

	s.mustPlaceItem(roomID, item, 3)
	_, slotOf := s.readItems(roomID)
	if slotOf[item] != 3 {
		t.Fatalf("legal move landed at %d, want 3", slotOf[item])
	}

	if resp, body := s.ratchet(roomID, 2); resp.StatusCode != http.StatusOK {
		t.Fatalf("ratchet: %d %s", resp.StatusCode, body)
	}
	s.mustPlaceItem(roomID, item, 7)
	_, slotOf = s.readItems(roomID)
	if slotOf[item] != 7 {
		t.Fatalf("after ratcheting, the move landed at %d, want 7", slotOf[item])
	}
}

func TestUnpublishedDesignFailsClosed(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithDesign("Watches")

	resp, body := s.addItem(roomID, syntheticModel)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"design_layout_unavailable"`) {
		t.Fatalf("add with nothing published → %d %s, want 400 design_layout_unavailable", resp.StatusCode, body)
	}
	s.insertSyntheticItems(roomID, 0, 2)
	bySlot, _ := s.readItems(roomID)
	before := s.roomState(t, roomID)
	resp, body = s.placeItem(roomID, bySlot[0], 3)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"design_layout_unavailable"`) {
		t.Fatalf("place with nothing published → %d %s, want 400 design_layout_unavailable", resp.StatusCode, body)
	}
	if s.roomState(t, roomID) != before {
		t.Fatal("a fail-closed refusal changed the Room")
	}

	s.publishCommittedDesignFixture(t)
	s.mustPlaceItem(roomID, bySlot[0], 3)
}

func TestARoomWithNoDesignAcceptsNoItems(t *testing.T) {
	s := newStack(t)
	roomID := s.createCollectionRoom(s.token, "Undesigned")

	resp, body := s.addItem(roomID, syntheticModel)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"design_required"`) {
		t.Fatalf("add without a Design → %d %s, want 400 design_required", resp.StatusCode, body)
	}
}

// MARK: - VERIFY 9 — reordering never changes current_tier

func TestReorderingNeverRaisesOrLowersTheTier(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.fillToItems(roomID, 12)

	before := s.currentTier(roomID)
	if before != 3 {
		t.Fatalf("tier is %d at 12 items, want 3", before)
	}

	bySlot, _ := s.readItems(roomID)
	for _, pair := range [][2]int{{0, 11}, {4, 5}, {1, 10}, {9, 2}, {0, 4}} {
		s.mustPlaceItem(roomID, bySlot[pair[0]], pair[1])
		bySlot, _ = s.readItems(roomID)
	}
	s.mustPlaceItem(roomID, bySlot[0], 15)

	if after := s.currentTier(roomID); after != before {
		t.Fatalf("tier moved from %d to %d across reorders — rule 3 forbids it", before, after)
	}
	items, _ := s.readItems(roomID)
	if len(items) != 12 {
		t.Fatalf("%d items after reordering, want 12", len(items))
	}
}

// MARK: - VERIFY 10 — concurrent reorders cannot duplicate a slot

func TestConcurrentReordersNeverDuplicateASlot(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.fillToItems(roomID, 6)

	bySlot, _ := s.readItems(roomID)
	ids := make([]string, 0, 6)
	for slot := 0; slot < 6; slot++ {
		ids = append(ids, bySlot[slot])
	}

	const attempts = 16
	var wait sync.WaitGroup
	statuses := make([]int, attempts)
	for attempt := 0; attempt < attempts; attempt++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			resp, _ := s.placeItem(roomID, ids[index%len(ids)], (index*3)%6)
			statuses[index] = resp.StatusCode
		}(attempt)
	}
	wait.Wait()

	for index, status := range statuses {
		if status != http.StatusOK && status != http.StatusConflict {
			t.Fatalf("request %d → %d; want 200 or 409", index, status)
		}
	}

	bySlot, slotOf := s.readItems(roomID)
	if len(bySlot) != 6 || len(slotOf) != 6 {
		t.Fatalf("%d slots hold %d items, want 6 and 6 — a slot was duplicated or an item lost",
			len(bySlot), len(slotOf))
	}
	for _, id := range ids {
		if _, present := slotOf[id]; !present {
			t.Fatalf("item %s disappeared during concurrent reordering", id)
		}
	}
	var distinct int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT count(DISTINCT slot_index) FROM collection_items WHERE collection_room_id = $1`,
		roomID).Scan(&distinct); err != nil {
		t.Fatal(err)
	}
	if distinct != 6 {
		t.Fatalf("%d distinct slot indices for 6 items", distinct)
	}
}

func TestConcurrentAddsTakeDistinctSlots(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	if resp, body := s.ratchet(roomID, 2); resp.StatusCode != http.StatusOK {
		s.t.Fatalf("ratchet: %d %s", resp.StatusCode, body)
	}

	const attempts = 8
	var wait sync.WaitGroup
	statuses := make([]int, attempts)
	for attempt := 0; attempt < attempts; attempt++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			resp, _ := s.addItem(roomID, syntheticModel)
			statuses[index] = resp.StatusCode
		}(attempt)
	}
	wait.Wait()

	for index, status := range statuses {
		if status != http.StatusCreated {
			t.Fatalf("concurrent add %d → %d, want 201", index, status)
		}
	}
	bySlot, slotOf := s.readItems(roomID)
	if len(slotOf) != attempts || len(bySlot) != attempts {
		t.Fatalf("%d items on %d slots after %d concurrent adds", len(slotOf), len(bySlot), attempts)
	}
	for slot := 0; slot < attempts; slot++ {
		if bySlot[slot] == "" {
			t.Fatalf("slot %d is empty; the lowest-free rule must produce 0..%d", slot, attempts-1)
		}
	}
}

// MARK: - VERIFY 11 — positions survive a restart

func TestItemPositionsSurviveARestart(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.fillToItems(roomID, 6)

	bySlot, _ := s.readItems(roomID)
	s.mustPlaceItem(roomID, bySlot[0], 5)
	bySlot, _ = s.readItems(roomID)
	s.mustPlaceItem(roomID, bySlot[2], 7)

	expectedBySlot, _ := s.readItems(roomID)
	expectedTier := s.currentTier(roomID)

	rows, err := s.pool.Pool().Query(context.Background(),
		`SELECT slot_index, id::text FROM collection_items WHERE collection_room_id = $1 ORDER BY slot_index`,
		roomID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	stored := map[int]string{}
	for rows.Next() {
		var slot int
		var id string
		if err := rows.Scan(&slot, &id); err != nil {
			t.Fatal(err)
		}
		stored[slot] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(stored) != fmt.Sprint(expectedBySlot) {
		t.Fatalf("persisted slots are %v, want %v", stored, expectedBySlot)
	}

	resp, body := s.do(http.MethodGet, "/collection-rooms", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", resp.StatusCode, body)
	}
	var list collectionRoomListJSON
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode list: %v (%s)", err, body)
	}
	found := false
	for _, room := range list.CollectionRooms {
		if room.ID != roomID {
			continue
		}
		found = true
		if room.CurrentTier != expectedTier {
			t.Fatalf("listed tier is %d, want %d", room.CurrentTier, expectedTier)
		}
		listed := map[int]string{}
		for _, item := range room.Items {
			listed[item.SlotIndex] = item.ID
		}
		if fmt.Sprint(listed) != fmt.Sprint(expectedBySlot) {
			t.Fatalf("listed slots are %v, want %v", listed, expectedBySlot)
		}
	}
	if !found {
		t.Fatalf("the Room is not in the list: %s", body)
	}
}

// MARK: - VERIFY 12 — ownership boundary, and Museum/Collection independence

func TestItemMutationIsOwnerOnlyAndRefusalsAreIndistinguishable(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	room := s.mustAddItem(roomID, syntheticModel)
	itemID := room.Items[0].ID

	stranger := s.strangerToken()

	type reply struct {
		status int
		body   string
	}
	attempt := func(method, path string, payload any) reply {
		resp, body := s.do(method, path, payload, stranger)
		return reply{resp.StatusCode, string(body)}
	}

	addReal := attempt(http.MethodPost, "/collection-rooms/"+roomID+"/items",
		map[string]string{"catalog_model_id": syntheticModel})
	addBogus := attempt(http.MethodPost, "/collection-rooms/"+bogusUUID+"/items",
		map[string]string{"catalog_model_id": syntheticModel})
	addMalformed := attempt(http.MethodPost, "/collection-rooms/not-a-uuid/items",
		map[string]string{"catalog_model_id": syntheticModel})

	for name, got := range map[string]reply{
		"real room": addReal, "nonexistent room": addBogus, "malformed room": addMalformed,
	} {
		if got.status != http.StatusNotFound {
			t.Fatalf("stranger add (%s) → %d %s, want 404", name, got.status, got.body)
		}
	}
	if addReal.body != addBogus.body || addReal.body != addMalformed.body {
		t.Fatalf("a stranger can tell a real Collection Room from a fake one: %q vs %q vs %q",
			addReal.body, addBogus.body, addMalformed.body)
	}

	placeReal := attempt(http.MethodPut,
		fmt.Sprintf("/collection-rooms/%s/items/%s/slot", roomID, itemID), map[string]any{"slot_index": 1})
	placeBogusItem := attempt(http.MethodPut,
		fmt.Sprintf("/collection-rooms/%s/items/%s/slot", roomID, bogusUUID), map[string]any{"slot_index": 1})
	placeBogusRoom := attempt(http.MethodPut,
		fmt.Sprintf("/collection-rooms/%s/items/%s/slot", bogusUUID, itemID), map[string]any{"slot_index": 1})

	for name, got := range map[string]reply{
		"real": placeReal, "bogus item": placeBogusItem, "bogus room": placeBogusRoom,
	} {
		if got.status != http.StatusNotFound {
			t.Fatalf("stranger place (%s) → %d %s, want 404", name, got.status, got.body)
		}
	}
	if placeReal.body != placeBogusItem.body || placeReal.body != placeBogusRoom.body {
		t.Fatalf("a stranger can distinguish real ids: %q vs %q vs %q",
			placeReal.body, placeBogusItem.body, placeBogusRoom.body)
	}

	bySlot, _ := s.readItems(roomID)
	if len(bySlot) != 1 || bySlot[0] != itemID {
		t.Fatalf("owner's items changed after a stranger's attempts: %v", bySlot)
	}

	otherRoom := s.roomWithPublishedDesign(t, "Other Watches")
	sameShape := map[string]reply{}
	for name, itemPath := range map[string]string{
		"another room's item": itemID,
		"nonexistent":         bogusUUID,
		"malformed":           "not-a-uuid",
	} {
		resp, body := s.do(http.MethodPut,
			fmt.Sprintf("/collection-rooms/%s/items/%s/slot", otherRoom, itemPath),
			map[string]any{"slot_index": 0}, s.token)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("owner place (%s) → %d %s, want 404", name, resp.StatusCode, body)
		}
		sameShape[name] = reply{resp.StatusCode, string(body)}
	}
	first := sameShape["another room's item"]
	for name, got := range sameShape {
		if got != first {
			t.Fatalf("owner can distinguish item ids (%s): %q vs %q", name, got.body, first.body)
		}
	}
	if _, slotOf := s.readItems(otherRoom); len(slotOf) != 0 {
		t.Fatalf("the other Room gained items: %v", slotOf)
	}
	if _, slotOf := s.readItems(roomID); slotOf[itemID] != 0 {
		t.Fatalf("the item left its own Room: %v", slotOf)
	}
}

func TestItemMutationLeavesTheMuseumUntouched(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)

	museumHalf := func() string {
		state := s.snapshotOwnerState()
		return state.Museum + "\n" + state.Rooms + "\n" + state.Slots + "\n" + state.Assets
	}
	before := museumHalf()

	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.fillToItems(roomID, 6)
	bySlot, _ := s.readItems(roomID)
	s.mustPlaceItem(roomID, bySlot[0], 5)
	s.mustPlaceItem(roomID, bySlot[1], 9)

	if after := museumHalf(); after != before {
		t.Fatalf("Collection item mutation changed the Museum:\nbefore %s\nafter  %s", before, after)
	}
	resp, _ := s.do(http.MethodGet, "/museum/me/rooms/"+f.publicRoom, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("museum room read after collection writes: %d", resp.StatusCode)
	}
}

// MARK: - Structure of the payload

func TestItemPayloadCarriesReferencesOnly(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.mustAddItem(roomID, syntheticModel)

	resp, body := s.do(http.MethodGet, "/collection-rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read room: %d %s", resp.StatusCode, body)
	}
	for _, forbidden := range []string{
		"transform", "position", "rotation", "scale", "mesh", "material",
		"asset_bundle", "geometry", "usdz", "bundle_id",
	} {
		if strings.Contains(string(body), `"`+forbidden+`"`) {
			t.Fatalf("the Collection Room payload names %q — presentation must not leak into content", forbidden)
		}
	}
}

// MARK: - The derived projection

func TestPublishDerivesTheFixtureCapacitiesFromItsLayout(t *testing.T) {
	s := newStack(t)

	result := s.publishCommittedDesignFixture(t)
	if result.AlreadyPublished {
		t.Fatal("a fresh stack should publish, not no-op")
	}
	if got := s.derivedCapacities(t, result.Bundle.BundleID, result.Bundle.Version); fmt.Sprint(got) != "[4 10 18]" {
		t.Fatalf("derived %v from the committed layout, want [4 10 18]", got)
	}
	var forbidden int
	if err := s.pool.Pool().QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		WHERE (table_name = 'asset_bundle_tier_capacities'
		       AND column_name IN ('design_id', 'collection_room_id', 'account_id', 'item_id', 'collection_item_id'))
		   OR (table_name = 'collection_designs'
		       AND column_name IN ('capacity', 'tier_capacity', 'tier_capacities', 'item_capacity', 'cumulative_capacity'))
		   OR (table_name IN ('collection_rooms', 'collection_items')
		       AND column_name LIKE '%capacity%')
	`).Scan(&forbidden); err != nil {
		t.Fatal(err)
	}
	if forbidden != 0 {
		t.Fatalf("%d capacity/content column(s) exist where the projection must not leak", forbidden)
	}
	resp, manifest := s.fetchManifest(t, result.Bundle.BundleID, "")
	if resp.StatusCode != http.StatusOK || manifest.Version != result.Bundle.Version {
		t.Fatalf("manifest → %d v%d, want 200 v%d", resp.StatusCode, manifest.Version, result.Bundle.Version)
	}
}

func TestProjectionCannotDriftFromThePublishedLayout(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	published := s.publishCommittedDesignFixture(t)
	bundleID, version := published.Bundle.BundleID, published.Bundle.Version

	_, manifest := s.fetchManifest(t, bundleID, "")
	var layoutURL string
	for _, file := range manifest.Files {
		if file.Role == "layout" {
			layoutURL = file.URL
		}
	}
	if layoutURL == "" {
		t.Fatal("the manifest lists no layout file")
	}
	served, err := testGet(layoutURL)
	if err != nil {
		t.Fatal(err)
	}
	defer served.Body.Close()
	layoutBytes, err := io.ReadAll(served.Body)
	if err != nil {
		t.Fatal(err)
	}
	_, rederived, err := catalogdomain.ParseCollectionDesignTierCapacities(layoutBytes)
	if err != nil {
		t.Fatalf("the served layout does not parse: %v", err)
	}
	registered := s.derivedCapacities(t, bundleID, version)
	if len(registered) != len(rederived) {
		t.Fatalf("registered %v, re-derived %v", registered, rederived)
	}
	for index, capacity := range rederived {
		if registered[index] != capacity.Cumulative || capacity.Tier != index+1 {
			t.Fatalf("registered %v, re-derived %v", registered, rederived)
		}
	}

	dir := committedDesignFixtureDir(t)
	geometry, _ := os.ReadFile(filepath.Join(dir, "geometry.usda"))
	republish := func(layout string) error {
		_, err := s.publisher.Publish(ctx, catalogapp.PublishRequest{
			BundleID: bundleID, Version: version, Kind: catalogdomain.BundleKindCollectionDesign,
			Format: "usda", MinAppVersion: 1,
			Files: []catalogapp.PublishSource{
				newPublishSource("geometry", catalogdomain.RoleGeometry, "model/vnd.usda+ascii", string(geometry)),
				newPublishSource("layout", catalogdomain.RoleLayout, "application/json", layout),
			},
		})
		return err
	}
	incoherent := strings.Replace(string(layoutBytes), `"cumulative_capacity": 4`, `"cumulative_capacity": 5`, 1)
	if incoherent == string(layoutBytes) {
		t.Fatal("the test did not actually alter the layout")
	}
	if err := republish(incoherent); !errors.Is(err, catalogdomain.ErrBundleInvalid) {
		t.Fatalf("re-publishing with an incoherent table → %v, want ErrBundleInvalid", err)
	}
	coherentButDifferent := strings.Replace(string(layoutBytes), `"yaw": 0.0`, `"yaw": 0.25`, 1)
	if coherentButDifferent == string(layoutBytes) {
		t.Fatal("the test did not actually alter the layout")
	}
	if _, _, err := catalogdomain.ParseCollectionDesignTierCapacities([]byte(coherentButDifferent)); err != nil {
		t.Fatalf("the altered layout must still be coherent for this half of the proof: %v", err)
	}
	if err := republish(coherentButDifferent); !errors.Is(err, catalogdomain.ErrBundleVersionImmutable) {
		t.Fatalf("re-publishing v%d with different bytes → %v, want ErrBundleVersionImmutable", version, err)
	}
	if got := s.derivedCapacities(t, bundleID, version); fmt.Sprint(got) != "[4 10 18]" {
		t.Fatalf("the projection changed under an immutable version: %v", got)
	}

	if _, err := s.pool.Pool().Exec(ctx,
		`DELETE FROM asset_bundle_tier_capacities WHERE bundle_id = $1 AND version = $2`, bundleID, version); err != nil {
		t.Fatal(err)
	}
	rerun := s.publishCommittedDesignFixture(t)
	if !rerun.AlreadyPublished || !rerun.ProjectionBackfilled || len(rerun.UploadedAssetIDs) != 0 {
		t.Fatalf("re-run: %+v — want AlreadyPublished, ProjectionBackfilled, no uploads", rerun)
	}
	if got := s.derivedCapacities(t, bundleID, version); fmt.Sprint(got) != "[4 10 18]" {
		t.Fatalf("backfilled %v, want [4 10 18]", got)
	}

	root := repoRoot(t)
	var writers []string
	for _, file := range goFilesUnder(t, root, "internal") {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		if fileNamesTableInSQL(t, file, "asset_bundle_tier_capacities") {
			rel, _ := filepath.Rel(root, file)
			writers = append(writers, rel)
		}
	}
	if len(writers) != 1 || !strings.HasSuffix(writers[0], "internal/catalog/infrastructure/postgres_bundle_repository.go") {
		t.Fatalf("the projection table is named in SQL by %v — the bundle registry must be its only writer", writers)
	}
}

func fileNamesTableInSQL(t *testing.T, file, table string) bool {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	pattern := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(table) + `([^A-Za-z0-9_]|$)`)
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING || found {
			return !found
		}
		if pattern.MatchString(literal.Value) {
			found = true
		}
		return !found
	})
	return found
}

func TestCapacityFollowsTheClientsEffectiveBundleVersion(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithPublishedDesign(t, "Watches")
	s.fillToItems(roomID, 2)
	bySlot, _ := s.readItems(roomID)
	item := bySlot[0]

	dir := committedDesignFixtureDir(t)
	geometry, _ := os.ReadFile(filepath.Join(dir, "geometry.usda"))
	layoutV2 := `{"_comment":"DEV FIXTURE v2 for a version test - NOT artwork","format_version":1,"design_id":"dev-fixture:collection-design","entry":{"position":[0,0,3.5],"yaw":0},"tiers":[` +
		`{"tier":1,"cumulative_capacity":6,"item_transforms":[{"slot_index":0,"position":[0,1,0]},{"slot_index":1,"position":[0,1,0]},{"slot_index":2,"position":[0,1,0]},{"slot_index":3,"position":[0,1,0]},{"slot_index":4,"position":[0,1,0]},{"slot_index":5,"position":[0,1,0]}]},` +
		`{"tier":2,"cumulative_capacity":12,"item_transforms":[{"slot_index":6,"position":[0,1,0]},{"slot_index":7,"position":[0,1,0]},{"slot_index":8,"position":[0,1,0]},{"slot_index":9,"position":[0,1,0]},{"slot_index":10,"position":[0,1,0]},{"slot_index":11,"position":[0,1,0]}]},` +
		`{"tier":3,"cumulative_capacity":20,"item_transforms":[{"slot_index":12,"position":[0,1,0]},{"slot_index":13,"position":[0,1,0]},{"slot_index":14,"position":[0,1,0]},{"slot_index":15,"position":[0,1,0]},{"slot_index":16,"position":[0,1,0]},{"slot_index":17,"position":[0,1,0]},{"slot_index":18,"position":[0,1,0]},{"slot_index":19,"position":[0,1,0]}]}]}`
	if _, err := s.publisher.Publish(context.Background(), catalogapp.PublishRequest{
		BundleID: "dev_fixture_collection_design", Version: 2, Kind: catalogdomain.BundleKindCollectionDesign,
		Format: "usda", MinAppVersion: 2,
		Files: []catalogapp.PublishSource{
			newPublishSource("geometry", catalogdomain.RoleGeometry, "model/vnd.usda+ascii", string(geometry)),
			newPublishSource("layout", catalogdomain.RoleLayout, "application/json", layoutV2),
		},
	}); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	place := func(generation string, slot int) (*http.Response, []byte) {
		return s.do(http.MethodPut,
			fmt.Sprintf("/collection-rooms/%s/items/%s/slot?app_asset_version=%s", roomID, item, generation),
			map[string]any{"slot_index": slot}, s.token)
	}
	if resp, body := place("1", 5); resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"slot_not_available"`) {
		t.Fatalf("generation 1 → slot 5: %d %s, want 400 slot_not_available", resp.StatusCode, body)
	}
	if resp, body := place("2", 5); resp.StatusCode != http.StatusOK {
		t.Fatalf("generation 2 → slot 5: %d %s, want 200", resp.StatusCode, body)
	}
	if _, m := s.fetchManifest(t, "dev_fixture_collection_design", "?app_asset_version=1"); m.Version != 1 {
		t.Fatalf("generation 1 resolves v%d, want v1", m.Version)
	}
	if _, m := s.fetchManifest(t, "dev_fixture_collection_design", "?app_asset_version=2"); m.Version != 2 {
		t.Fatalf("generation 2 resolves v%d, want v2", m.Version)
	}
	if resp, _ := place("0", 1); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("app_asset_version=0 → %d, want 400", resp.StatusCode)
	}
}
