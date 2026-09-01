package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	collectiondomain "muse-backend/internal/collection/domain"
)

var fixtureCapacities = collectiondomain.TierCapacities{4, 10, 18}

func (s *stack) roomWithDesign(name string) string {
	s.t.Helper()
	roomID := s.createCollectionRoom(s.token, name)
	resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]string{"design_id": devFixtureDesign}, s.token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("assign the fixture design: %d %s", resp.StatusCode, body)
	}
	return roomID
}

func (s *stack) ratchet(roomID string, tier int) (*http.Response, []byte) {
	s.t.Helper()
	return s.do(http.MethodPost, "/collection-rooms/"+roomID+"/tier",
		map[string]any{"tier": tier}, s.token)
}

func (s *stack) currentTier(roomID string) int {
	s.t.Helper()
	resp, body := s.do(http.MethodGet, "/collection-rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("read room: %d %s", resp.StatusCode, body)
	}
	return decodeCollectionRoom(s.t, body).CurrentTier
}

const syntheticModel = "dev-fixture:model-chrono-one"

func (s *stack) insertSyntheticItems(roomID string, from, count int) {
	s.t.Helper()
	ctx := context.Background()
	for index := from; index < from+count; index++ {
		_, err := s.pool.Pool().Exec(ctx, `
			INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id)
			VALUES ($1, $2, $3)
		`, roomID, index, syntheticModel)
		if err != nil {
			s.t.Fatalf("insert synthetic item %d: %v", index, err)
		}
	}
}

func requiredTierFor(t *testing.T, itemCount int) int {
	t.Helper()
	tier, err := collectiondomain.RequiredTier(itemCount, fixtureCapacities)
	if err != nil {
		t.Fatalf("required tier for %d items: %v", itemCount, err)
	}
	return int(tier)
}

func TestWithinTierOne_DoesNotExpand(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithDesign("Watches")

	for _, count := range []int{0, 1, 4} {
		if got := requiredTierFor(t, count); got != 1 {
			t.Fatalf("%d items requires tier %d, want 1", count, got)
		}
	}

	s.insertSyntheticItems(roomID, 0, 4)
	resp, body := s.ratchet(roomID, requiredTierFor(t, 4))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ratchet: %d %s", resp.StatusCode, body)
	}
	if got := s.currentTier(roomID); got != 1 {
		t.Fatalf("the room expanded to tier %d while still inside tier 1", got)
	}
}

func TestCrossingACapacityExpandsExactlyOneTier(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithDesign("Watches")

	s.insertSyntheticItems(roomID, 0, 5)
	resp, body := s.ratchet(roomID, requiredTierFor(t, 5))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ratchet: %d %s", resp.StatusCode, body)
	}
	if got := decodeCollectionRoom(t, body).CurrentTier; got != 2 {
		t.Fatalf("5 items put the room at tier %d, want exactly 2", got)
	}

	s.insertSyntheticItems(roomID, 5, 5)
	if _, body := s.ratchet(roomID, requiredTierFor(t, 10)); decodeCollectionRoom(t, body).CurrentTier != 2 {
		t.Fatalf("10 items moved the room off tier 2")
	}
}

func TestJumpingMultipleThresholdsReachesTheCorrectTier(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithDesign("Watches")

	s.insertSyntheticItems(roomID, 0, 15)
	required := requiredTierFor(t, 15)
	if required != 3 {
		t.Fatalf("15 items requires tier %d, want 3", required)
	}

	resp, body := s.ratchet(roomID, required)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ratchet: %d %s", resp.StatusCode, body)
	}
	if got := decodeCollectionRoom(t, body).CurrentTier; got != 3 {
		t.Fatalf("a multi-threshold jump landed on tier %d, want 3", got)
	}
}

func TestDeletingItemsNeverRetractsTheTier(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	roomID := s.roomWithDesign("Watches")

	s.insertSyntheticItems(roomID, 0, 15)
	if _, body := s.ratchet(roomID, requiredTierFor(t, 15)); decodeCollectionRoom(t, body).CurrentTier != 3 {
		t.Fatal("failed to reach tier 3")
	}

	if _, err := s.pool.Pool().Exec(ctx,
		`DELETE FROM collection_items WHERE collection_room_id = $1`, roomID); err != nil {
		t.Fatal(err)
	}

	if got := requiredTierFor(t, 0); got != 1 {
		t.Fatalf("an empty room requires tier %d, want 1", got)
	}
	if got := s.currentTier(roomID); got != 3 {
		t.Fatalf("the tier retracted to %d —: the room is intentionally ever-expanding", got)
	}
	resp, body := s.ratchet(roomID, 1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a lower request should succeed as a no-op: %d %s", resp.StatusCode, body)
	}
	if got := decodeCollectionRoom(t, body).CurrentTier; got != 3 {
		t.Fatalf("an explicit request for tier 1 shrank the room to %d", got)
	}
}

func TestHighestTierSurvivesRestart(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithDesign("Watches")

	s.insertSyntheticItems(roomID, 0, 12)
	if _, body := s.ratchet(roomID, requiredTierFor(t, 12)); decodeCollectionRoom(t, body).CurrentTier != 3 {
		t.Fatal("failed to reach tier 3")
	}

	var stored int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT current_tier FROM collection_rooms WHERE id = $1`, roomID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 3 {
		t.Fatalf("the persisted tier is %d, want 3", stored)
	}
	resp, body := s.do(http.MethodGet, "/collection-rooms", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatal(body)
	}
	if !strings.Contains(string(body), `"current_tier":3`) {
		t.Fatalf("the list does not report tier 3: %s", body)
	}
}

func TestBeyondTheHighestTier_FailsExplicitly(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithDesign("Watches")

	if _, err := collectiondomain.RequiredTier(19, fixtureCapacities); err == nil {
		t.Fatal("19 items should exhaust a 4/10/18 table rather than resolving")
	}

	resp, body := s.ratchet(roomID, 4)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "tier_not_authored") {
		t.Fatalf("tier 4 → %d %s; want 400 tier_not_authored", resp.StatusCode, body)
	}
	if got := s.currentTier(roomID); got != 1 {
		t.Fatalf("a refused request moved the tier to %d", got)
	}

	resp, body = s.ratchet(roomID, 0)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid_tier") {
		t.Fatalf("tier 0 → %d %s; want 400 invalid_tier", resp.StatusCode, body)
	}
}

func TestARoomWithNoDesignCannotExpand(t *testing.T) {
	s := newStack(t)
	roomID := s.createCollectionRoom(s.token, "No design yet")

	resp, body := s.ratchet(roomID, 2)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "design_required") {
		t.Fatalf("→ %d %s; want 400 design_required", resp.StatusCode, body)
	}
}

func TestTierRatchetIsOwnerOnly(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithDesign("Watches")
	before := s.snapshotOwnerState()

	stranger := s.strangerToken()
	resp, body := s.do(http.MethodPost, "/collection-rooms/"+roomID+"/tier",
		map[string]any{"tier": 3}, stranger)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a stranger's ratchet → %d %s; want 404", resp.StatusCode, body)
	}
	bogus := "00000000-0000-4000-8000-000000000000"
	absentResp, absentBody := s.do(http.MethodPost, "/collection-rooms/"+bogus+"/tier",
		map[string]any{"tier": 3}, stranger)
	if absentResp.StatusCode != resp.StatusCode || string(absentBody) != string(body) {
		t.Fatalf("foreign (%d %s) and nonexistent (%d %s) must be indistinguishable",
			resp.StatusCode, body, absentResp.StatusCode, absentBody)
	}

	if after := s.snapshotOwnerState(); after != before {
		t.Fatalf("the owner's state moved:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestConcurrentRatchetsLeaveTheHighest(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithDesign("Watches")

	requests := []int{3, 1, 2, 3, 1, 2, 3, 2}
	var wg sync.WaitGroup
	statuses := make([]int, len(requests))
	for index, tier := range requests {
		wg.Add(1)
		go func(slot, requested int) {
			defer wg.Done()
			resp, _ := s.ratchet(roomID, requested)
			statuses[slot] = resp.StatusCode
		}(index, tier)
	}
	wg.Wait()

	for index, status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("concurrent request #%d → %d", index, status)
		}
	}
	if got := s.currentTier(roomID); got != 3 {
		t.Fatalf("stored tier = %d after interleaved concurrent requests, want 3", got)
	}
}

func TestTierIsNotReachableThroughTheGeneralPatch(t *testing.T) {
	s := newStack(t)
	roomID := s.roomWithDesign("Watches")

	resp, body := s.do(http.MethodPatch, "/collection-rooms/"+roomID,
		map[string]any{"name": "Renamed", "current_tier": 3}, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch: %d %s", resp.StatusCode, body)
	}
	patched := decodeCollectionRoom(t, body)
	if patched.Name != "Renamed" {
		t.Fatalf("the rename did not apply: %q", patched.Name)
	}
	if patched.CurrentTier != 1 {
		t.Fatalf("a PATCH moved the tier to %d — only the ratchet route may", patched.CurrentTier)
	}

	s.insertSyntheticItems(roomID, 0, 5)
	if _, body := s.ratchet(roomID, 2); decodeCollectionRoom(t, body).Name != "Renamed" {
		t.Fatal("a tier bump disturbed the Room's name")
	}
}

func TestPerformanceGate3_LargeSyntheticCollection(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	roomID := s.roomWithDesign("Large synthetic")

	large := collectiondomain.TierCapacities{}
	for tier := 1; tier <= 200; tier++ {
		large = append(large, tier*25)
	}

	const itemCount = 5000
	started := time.Now()
	batch := 0
	for batch < itemCount {
		size := 500
		if remaining := itemCount - batch; remaining < size {
			size = remaining
		}
		values := make([]string, 0, size)
		args := make([]any, 0, size*3)
		for index := 0; index < size; index++ {
			slot := batch + index
			values = append(values, fmt.Sprintf("($%d,$%d,$%d)", index*3+1, index*3+2, index*3+3))
			args = append(args, roomID, slot, syntheticModel)
		}
		query := `INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id) VALUES ` +
			strings.Join(values, ",")
		if _, err := s.pool.Pool().Exec(ctx, query, args...); err != nil {
			t.Fatalf("bulk insert at %d: %v", batch, err)
		}
		batch += size
	}
	insertDuration := time.Since(started)

	started = time.Now()
	tier, err := collectiondomain.RequiredTier(itemCount, large)
	arithmeticDuration := time.Since(started)
	if err != nil {
		t.Fatalf("required tier for %d items: %v", itemCount, err)
	}
	if tier != 200 {
		t.Fatalf("5000 items against a 25..5000 table requires tier %d, want 200", tier)
	}

	started = time.Now()
	var counted int
	if err := s.pool.Pool().QueryRow(ctx,
		`SELECT count(*) FROM collection_items WHERE collection_room_id = $1`, roomID).Scan(&counted); err != nil {
		t.Fatal(err)
	}
	countDuration := time.Since(started)
	if counted != itemCount {
		t.Fatalf("counted %d items, want %d", counted, itemCount)
	}

	started = time.Now()
	resp, body := s.do(http.MethodGet, "/collection-rooms/"+roomID, nil, s.token)
	loadDuration := time.Since(started)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("load: %d", resp.StatusCode)
	}
	loaded := decodeCollectionRoom(t, body)
	if len(loaded.Items) != itemCount {
		t.Fatalf("loaded %d items, want %d", len(loaded.Items), itemCount)
	}
	for index, item := range loaded.Items {
		if item.SlotIndex != index {
			t.Fatalf("item %d has slot %d — ordering broke at scale", index, item.SlotIndex)
		}
	}

	started = time.Now()
	if resp, body := s.ratchet(roomID, 3); resp.StatusCode != http.StatusOK {
		t.Fatalf("ratchet at scale: %d %s", resp.StatusCode, body)
	}
	ratchetDuration := time.Since(started)
	if got := s.currentTier(roomID); got != 3 {
		t.Fatalf("tier = %d after ratcheting at scale", got)
	}

	t.Logf(" (server, synthetic fixture range — NOT production thresholds):\n"+
		"  %d synthetic items inserted in %v\n"+
		"  RequiredTier over a 200-tier table: %v\n"+
		"  item COUNT query: %v\n"+
		"  GET room with %d items (HTTP + scan + JSON): %v\n"+
		"  tier ratchet write: %v",
		itemCount, insertDuration, arithmeticDuration, countDuration, itemCount, loadDuration, ratchetDuration)

	if arithmeticDuration > 10*time.Millisecond {
		t.Errorf("tier arithmetic took %v for one call — expected microseconds", arithmeticDuration)
	}
	if ratchetDuration > 2*time.Second {
		t.Errorf("the ratchet write took %v — it is one statement and should not scale with collection size", ratchetDuration)
	}
}
