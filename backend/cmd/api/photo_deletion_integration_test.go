package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	catalinfra "muse-backend/internal/catalog/infrastructure"
	identityiface "muse-backend/internal/identity/interfaces"
	museumapp "muse-backend/internal/museum/application"
	museumdomain "muse-backend/internal/museum/domain"
	museuminfra "muse-backend/internal/museum/infrastructure"
	museumiface "muse-backend/internal/museum/interfaces"
	platformhttp "muse-backend/internal/platform/http"
)

func (s *stack) deletePhoto(roomID, photoAssetID, token string) (*http.Response, []slotJSON, map[string]any) {
	s.t.Helper()
	resp, raw := s.do(http.MethodDelete, "/museum/me/rooms/"+roomID+"/photos/"+photoAssetID, nil, token)
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

// MARK: - Compaction

func TestDeleteAPI_RemovesThePhotograph_CompactsTheRest_AndReleasesTheAsset(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 600, 800, "b"))
	c := s.uploaded(newPhoto(t, 800, 600, "c"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset, c.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	for _, p := range []*photoFixture{a, b, c} {
		if resp, _, _ := s.setCaption(roomID, p.asset, "caption "+p.cuid, s.token); resp.StatusCode != http.StatusOK {
			t.Fatal("caption")
		}
	}
	rowA, rowC := s.slotRowID(roomID, a.asset), s.slotRowID(roomID, c.asset)

	resp, slots, errBody := s.deletePhoto(roomID, b.asset, s.token)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d %v", resp.StatusCode, errBody)
	}
	if got := orderFrom(slots); !equalStrings(got, []string{a.asset, c.asset}) {
		t.Fatalf("order = %v, want [%s %s]", got, a.asset, c.asset)
	}
	if captionFor(slots, a.asset) != "caption a" || captionFor(slots, c.asset) != "caption c" {
		t.Error("captions must stay with their photographs through compaction")
	}
	if s.slotRowID(roomID, a.asset) != rowA || s.slotRowID(roomID, c.asset) != rowC {
		t.Error("the remaining rows must be the same rows, shifted — not re-created")
	}
	if s.slotCount(roomID) != 2 {
		t.Errorf("row count = %d, want 2", s.slotCount(roomID))
	}
	if s.assetState(b.asset) != "released" {
		t.Errorf("the deleted asset must be released, got %s", s.assetState(b.asset))
	}
	if s.assetState(a.asset) != "committed" || s.assetState(c.asset) != "committed" {
		t.Error("the remaining assets must stay committed")
	}
	if tickets := s.ticketAssetIDs(roomID); !equalStrings(tickets, []string{a.asset, c.asset}) {
		t.Errorf("photo-urls = %v", tickets)
	}
	resp, raw := s.do(http.MethodGet, "/museum/me/rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get room: %d", resp.StatusCode)
	}
	var room struct {
		PhotoSlots []slotJSON `json:"photo_slots"`
	}
	_ = json.Unmarshal(raw, &room)
	if !equalStrings(orderFrom(room.PhotoSlots), []string{a.asset, c.asset}) {
		t.Error("a fresh read must show the compacted Room")
	}
}

func TestDeleteAPI_TheOnlyPhotograph_LeavesAnEmptyRoom(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	if resp, _, _ := s.assign(roomID, []string{a.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}

	resp, slots, errBody := s.deletePhoto(roomID, a.asset, s.token)

	if resp.StatusCode != http.StatusOK || len(slots) != 0 {
		t.Fatalf("delete the only photograph: %d %v %v", resp.StatusCode, slots, errBody)
	}
	if tickets := s.ticketAssetIDs(roomID); len(tickets) != 0 {
		t.Errorf("an empty Room serves nothing; got %v", tickets)
	}
	if s.assetState(a.asset) != "released" {
		t.Error("released")
	}
}

func TestDeleteAPI_FullRoom_DeleteOne_AddOne_TakesTheLastPosition(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	var all []string
	for i := 0; i < museumdomain.MaxPhotosPerRoom; i++ {
		all = append(all, s.uploaded(newPhoto(t, 640, 480, fmt.Sprintf("p-%d", i))).asset)
	}
	if resp, _, _ := s.assign(roomID, all); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed 28")
	}
	extra := s.uploaded(newPhoto(t, 640, 480, "extra"))
	if resp, _, _ := s.assign(roomID, []string{extra.asset}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("a full Room must refuse a 29th, got %d", resp.StatusCode)
	}

	if resp, slots, errBody := s.deletePhoto(roomID, all[5], s.token); resp.StatusCode != http.StatusOK || len(slots) != 27 {
		t.Fatalf("delete from full: %d %v", resp.StatusCode, errBody)
	}
	resp, slots, errBody := s.assign(roomID, []string{extra.asset})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add after delete: %d %v", resp.StatusCode, errBody)
	}
	order := orderFrom(slots)
	if order == nil || len(order) != museumdomain.MaxPhotosPerRoom {
		t.Fatalf("expected a contiguous full Room, got %v", order)
	}
	if order[museumdomain.MaxPhotosPerRoom-1] != extra.asset {
		t.Errorf("the newcomer takes the last position; got %v at 27", order[27])
	}
	want := append(append([]string{}, all[:5]...), all[6:]...)
	if !equalStrings(order[:27], want) {
		t.Error("the remaining photographs must keep their relative order")
	}
}

// MARK: - Refusals

func TestDeleteAPI_Repeat_Foreign_AndUnknown_AreRefused_AndChangeNothing(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	orphan := s.uploaded(newPhoto(t, 800, 600, "orphan"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	if resp, _, _ := s.deletePhoto(roomID, b.asset, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("first delete")
	}
	before := s.snapshotOwnerState()

	cases := []struct {
		name, asset string
	}{
		{"repeat of a completed deletion", b.asset},
		{"photograph never in the room", orphan.asset},
		{"unknown photograph", "00000000-0000-4000-8000-000000000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, _, errBody := s.deletePhoto(roomID, tc.asset, s.token)
			if resp.StatusCode != http.StatusNotFound || errBody["code"] != "photo_not_in_room" {
				t.Errorf("got %d %v, want 404 photo_not_in_room", resp.StatusCode, errBody)
			}
			if s.snapshotOwnerState() != before {
				t.Error("a refused deletion must change nothing")
			}
		})
	}
	if s.assetState(orphan.asset) != "pending" {
		t.Error("an unassigned asset is untouched by a refused deletion")
	}
	if resp, _, _ := s.deletePhoto("00000000-0000-4000-8000-000000000000", a.asset, s.token); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown room: got %d, want 404", resp.StatusCode)
	}
}

func TestDeleteAPI_ReorderAndCaptionAfterwards_UseTheCompactedRoom(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	c := s.uploaded(newPhoto(t, 800, 600, "c"))
	d := s.uploaded(newPhoto(t, 800, 600, "d"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset, c.asset, d.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	if resp, _, _ := s.deletePhoto(roomID, b.asset, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("delete")
	}

	if resp, _, errBody := s.reorder(roomID, []string{d.asset, c.asset, b.asset, a.asset}, s.token); resp.StatusCode != http.StatusConflict || errBody["code"] != "order_mismatch" {
		t.Errorf("a stale order must be a mismatch: %d %v", resp.StatusCode, errBody)
	}
	resp, slots, errBody := s.reorder(roomID, []string{d.asset, c.asset, a.asset}, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reorder of the compacted Room: %d %v", resp.StatusCode, errBody)
	}
	if !equalStrings(orderFrom(slots), []string{d.asset, c.asset, a.asset}) {
		t.Errorf("order = %v", orderFrom(slots))
	}
	if resp, _, errBody := s.setCaption(roomID, b.asset, "x", s.token); resp.StatusCode != http.StatusNotFound || errBody["code"] != "photo_not_in_room" {
		t.Errorf("captioning the deleted photograph must be refused: %d %v", resp.StatusCode, errBody)
	}
	if resp, slots, _ := s.setCaption(roomID, c.asset, "still here", s.token); resp.StatusCode != http.StatusOK || captionFor(slots, c.asset) != "still here" {
		t.Errorf("captioning a remaining photograph: %d", resp.StatusCode)
	}
}

// MARK: - Lifecycle

func TestDeleteAPI_ReleasedAsset_IsNotReusable_AndIsSweptAfterTheGracePeriod(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	if resp, _, _ := s.deletePhoto(roomID, b.asset, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("delete")
	}

	if resp, _, _ := s.assign(roomID, []string{b.asset}); resp.StatusCode != http.StatusGone {
		t.Errorf("a released asset must be refused by assignment, got %d", resp.StatusCode)
	}
	if resp, _, _ := s.replace(roomID, a.asset, b.asset, s.token); resp.StatusCode != http.StatusGone {
		t.Errorf("a released asset must be refused as a replacement, got %d", resp.StatusCode)
	}
	if _, err := s.storage.Stat(context.Background(), "photos/"+s.accountID+"/"+b.asset); err != nil {
		t.Error("released bytes must remain until the sweep")
	}
	if n, _ := s.media.ReclaimReleasedAssets(context.Background(), time.Hour, 100); n != 0 {
		t.Errorf("a just-released asset must wait out the grace period; reclaimed %d", n)
	}
	if _, err := s.pool.Pool().Exec(context.Background(), `UPDATE assets SET released_at = now() - interval '2 hours' WHERE id = $1`, b.asset); err != nil {
		t.Fatal(err)
	}
	if n, err := s.media.ReclaimReleasedAssets(context.Background(), time.Hour, 100); err != nil || n != 1 {
		t.Fatalf("reclaim: n=%d err=%v", n, err)
	}
	if s.assetState(b.asset) != "discarded" {
		t.Error("swept asset must be discarded")
	}
	if _, err := s.storage.Stat(context.Background(), "photos/"+s.accountID+"/"+b.asset); err == nil {
		t.Error("swept bytes must be gone")
	}
	if s.assetState(a.asset) != "committed" {
		t.Error("the remaining photograph is untouched by the sweep")
	}
}

// MARK: - Concurrency

func TestDeleteAPI_TwoRacersOnTheSamePhotograph_ExactlyOneWins(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}

	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, _, _ := s.deletePhoto(roomID, b.asset, s.token)
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
	if s.slotCount(roomID) != 1 || s.assetState(b.asset) != "released" || s.assetState(a.asset) != "committed" {
		t.Error("exactly one deletion must have applied")
	}
}

func TestDeleteAPI_TwoRacersOnDifferentPhotographs_BothWin_AndIndicesStayContiguous(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	var all []string
	for i := 0; i < 6; i++ {
		all = append(all, s.uploaded(newPhoto(t, 640, 480, fmt.Sprintf("r-%d", i))).asset)
	}
	if resp, _, _ := s.assign(roomID, all); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}

	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, target := range []string{all[1], all[4]} {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			resp, _, _ := s.deletePhoto(roomID, target, s.token)
			statuses <- resp.StatusCode
		}(target)
	}
	wg.Wait()
	close(statuses)
	for code := range statuses {
		if code != http.StatusOK {
			t.Errorf("a deletion of a distinct photograph failed with %d", code)
		}
	}

	rows, err := s.pool.Pool().Query(context.Background(), `SELECT slot_index, photo_asset_id FROM room_photo_slots WHERE room_id = $1 ORDER BY slot_index`, roomID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var final []string
	expect := 0
	for rows.Next() {
		var idx int
		var asset string
		_ = rows.Scan(&idx, &asset)
		if idx != expect {
			t.Fatalf("index %d, want %d — compaction left a gap", idx, expect)
		}
		expect++
		final = append(final, asset)
	}
	if !equalStrings(final, []string{all[0], all[2], all[3], all[5]}) {
		t.Errorf("final = %v", final)
	}
}

// MARK: - Degraded mode

func TestDeleteAPI_WithoutObjectStorage_Answers503(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()

	degraded := museumapp.NewMuseumService(museuminfra.NewPostgresMuseumRepository(s.pool.Pool()), catalinfra.NewPostgresCatalogRepository(s.pool.Pool()))
	router := platformhttp.NewRouter()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	museumiface.NewHandlers(degraded, identityiface.NewBearerAuthenticator(s.signer), logger).RegisterRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/museum/me/rooms/"+roomID+"/photos/00000000-0000-4000-8000-000000000000", bytes.NewReader(nil))
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := testHTTPClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 without object storage, got %d", resp.StatusCode)
	}
}
