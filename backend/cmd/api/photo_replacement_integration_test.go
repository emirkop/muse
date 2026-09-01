package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	museuminfra "muse-backend/internal/museum/infrastructure"
	museumiface "muse-backend/internal/museum/interfaces"
	platformhttp "muse-backend/internal/platform/http"
)

func (s *stack) replace(roomID, photoAssetID, replacementAssetID, token string) (*http.Response, []slotJSON, map[string]any) {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/museum/me/rooms/"+roomID+"/photos/"+photoAssetID+"/replacement",
		map[string]string{"asset_id": replacementAssetID}, token)
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

func (s *stack) slotRowID(roomID, assetID string) string {
	s.t.Helper()
	var id string
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT id::text FROM room_photo_slots WHERE room_id = $1 AND photo_asset_id = $2`, roomID, assetID).Scan(&id); err != nil {
		s.t.Fatalf("slot row id for %s: %v", assetID, err)
	}
	return id
}

func (s *stack) ticketAssetIDs(roomID string) []string {
	s.t.Helper()
	resp, raw := s.do(http.MethodGet, "/museum/me/rooms/"+roomID+"/photo-urls", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("photo-urls: %d %s", resp.StatusCode, raw)
	}
	var body struct {
		Tickets []struct {
			PhotoAssetID string `json:"photo_asset_id"`
		} `json:"tickets"`
	}
	_ = json.Unmarshal(raw, &body)
	out := make([]string, 0, len(body.Tickets))
	for _, t := range body.Tickets {
		out = append(out, t.PhotoAssetID)
	}
	return out
}

// MARK: - The happy path

func TestReplaceAPI_ReplacesTheImage_KeepingSlotIndexAndCaption(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 600, 800, "b"))
	c := s.uploaded(newPhoto(t, 800, 600, "c"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset, c.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	if resp, _, _ := s.setCaption(roomID, b.asset, "Trabzon, 1998", s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("caption")
	}
	rowBefore := s.slotRowID(roomID, b.asset)
	replacement := s.uploaded(newPhoto(t, 1200, 900, "replacement"))
	if s.assetState(replacement.asset) != "pending" {
		t.Fatal("a fresh upload must be pending before replacement")
	}

	resp, slots, errBody := s.replace(roomID, b.asset, replacement.asset, s.token)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replace: %d %v", resp.StatusCode, errBody)
	}
	if got := orderFrom(slots); !equalStrings(got, []string{a.asset, replacement.asset, c.asset}) {
		t.Fatalf("order = %v, want [%s %s %s]", got, a.asset, replacement.asset, c.asset)
	}
	if captionFor(slots, replacement.asset) != "Trabzon, 1998" {
		t.Errorf("caption after replacement = %q, want preserved", captionFor(slots, replacement.asset))
	}
	if captionFor(slots, b.asset) != "<absent>" {
		t.Error("the replaced photograph must no longer be in the Room")
	}
	if s.slotRowID(roomID, replacement.asset) != rowBefore {
		t.Error("the slot row must be updated in place, not recreated")
	}
	if s.assetState(replacement.asset) != "committed" {
		t.Errorf("replacement must be committed, got %s", s.assetState(replacement.asset))
	}
	if s.assetState(b.asset) != "released" {
		t.Errorf("the replaced asset must be released, got %s", s.assetState(b.asset))
	}
	resp, raw := s.do(http.MethodGet, "/museum/me/rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get room: %d", resp.StatusCode)
	}
	var room struct {
		PhotoSlots []slotJSON `json:"photo_slots"`
	}
	_ = json.Unmarshal(raw, &room)
	if !equalStrings(orderFrom(room.PhotoSlots), []string{a.asset, replacement.asset, c.asset}) {
		t.Error("a fresh read must show the replacement in place")
	}
	tickets := s.ticketAssetIDs(roomID)
	if !equalStrings(tickets, []string{a.asset, replacement.asset, c.asset}) {
		t.Errorf("photo-urls = %v, want the current photographs only", tickets)
	}
}

func TestReplaceAPI_OldAsset_CannotBeServedOrReused(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	replacement := s.uploaded(newPhoto(t, 800, 600, "replacement"))
	if resp, _, _ := s.replace(roomID, b.asset, replacement.asset, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("replace")
	}
	before := s.snapshotOwnerState()

	if _, err := s.media.IssuePhotoDownloadTickets(context.Background(), s.accountID, []string{b.asset}); err == nil {
		t.Error("a released asset must not be served")
	}
	resp, _, errBody := s.replace(roomID, a.asset, b.asset, s.token)
	if resp.StatusCode != http.StatusGone || errBody["code"] != "asset_discarded" {
		t.Errorf("a released asset must be refused as content: %d %v", resp.StatusCode, errBody)
	}
	if resp, _, errBody := s.assign(roomID, []string{b.asset}); resp.StatusCode != http.StatusGone {
		t.Errorf("a released asset must be refused by assignment: %d %v", resp.StatusCode, errBody)
	}
	if resp, raw := s.initiate(newPhoto(t, 800, 600, "b")); resp.StatusCode != http.StatusGone {
		t.Errorf("re-initiating a released upload id must be refused: %d %s", resp.StatusCode, raw)
	}
	if s.snapshotOwnerState() != before {
		t.Error("none of the refusals may change stored state")
	}
	if _, err := s.storage.Stat(context.Background(), "photos/"+s.accountID+"/"+b.asset); err != nil {
		t.Error("released bytes must remain until the reclamation sweep")
	}
}

func TestReplaceAPI_ReclaimSweep_DeletesOldBytesAfterTheGracePeriod(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	if resp, _, _ := s.assign(roomID, []string{a.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	replacement := s.uploaded(newPhoto(t, 800, 600, "replacement"))
	if resp, _, _ := s.replace(roomID, a.asset, replacement.asset, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("replace")
	}

	n, err := s.media.ReclaimReleasedAssets(context.Background(), time.Hour, 100)
	if err != nil || n != 0 {
		t.Fatalf("a just-released asset must wait out the grace period; n=%d err=%v", n, err)
	}
	if _, err := s.pool.Pool().Exec(context.Background(),
		`UPDATE assets SET released_at = now() - interval '2 hours' WHERE id = $1`, a.asset); err != nil {
		t.Fatal(err)
	}

	n, err = s.media.ReclaimReleasedAssets(context.Background(), time.Hour, 100)
	if err != nil || n != 1 {
		t.Fatalf("reclaim: n=%d err=%v", n, err)
	}
	if s.assetState(a.asset) != "discarded" {
		t.Errorf("released asset must be discarded after the sweep, got %s", s.assetState(a.asset))
	}
	if _, err := s.storage.Stat(context.Background(), "photos/"+s.accountID+"/"+a.asset); err == nil {
		t.Error("released bytes must be deleted by the sweep")
	}
	if s.assetState(replacement.asset) != "committed" {
		t.Error("the replacement must be untouched by the sweep")
	}
	if _, err := s.storage.Stat(context.Background(), "photos/"+s.accountID+"/"+replacement.asset); err != nil {
		t.Error("the replacement's bytes must remain")
	}
	if resp, raw := s.initiate(newPhoto(t, 800, 600, "a")); resp.StatusCode != http.StatusCreated {
		t.Errorf("a discarded upload id must be reusable: %d %s", resp.StatusCode, raw)
	}
}

// MARK: - Idempotency and the other axes

func TestReplaceAPI_RetryAfterSuccess_IsIdempotent(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	replacement := s.uploaded(newPhoto(t, 800, 600, "replacement"))
	first, slots1, _ := s.replace(roomID, b.asset, replacement.asset, s.token)
	if first.StatusCode != http.StatusOK {
		t.Fatal("first replace")
	}
	after := s.snapshotOwnerState()

	second, slots2, errBody := s.replace(roomID, b.asset, replacement.asset, s.token)

	if second.StatusCode != http.StatusOK {
		t.Fatalf("a retry must succeed, got %d %v", second.StatusCode, errBody)
	}
	if !equalStrings(orderFrom(slots1), orderFrom(slots2)) {
		t.Error("a retry must converge on the same Room")
	}
	if s.snapshotOwnerState() != after {
		t.Error("a retry must change nothing")
	}
}

func TestReplaceAPI_ReorderAndCaptionAfterwards_UseTheNewIdentity(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	c := s.uploaded(newPhoto(t, 800, 600, "c"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset, c.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	replacement := s.uploaded(newPhoto(t, 800, 600, "replacement"))
	if resp, _, _ := s.replace(roomID, b.asset, replacement.asset, s.token); resp.StatusCode != http.StatusOK {
		t.Fatal("replace")
	}

	if resp, _, errBody := s.reorder(roomID, []string{c.asset, b.asset, a.asset}, s.token); resp.StatusCode != http.StatusConflict || errBody["code"] != "order_mismatch" {
		t.Errorf("an order naming the replaced photograph must be a mismatch: %d %v", resp.StatusCode, errBody)
	}
	if resp, _, errBody := s.setCaption(roomID, b.asset, "x", s.token); resp.StatusCode != http.StatusNotFound || errBody["code"] != "photo_not_in_room" {
		t.Errorf("captioning the replaced photograph must be refused: %d %v", resp.StatusCode, errBody)
	}
	resp, slots, errBody := s.reorder(roomID, []string{c.asset, replacement.asset, a.asset}, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reorder with the new identity: %d %v", resp.StatusCode, errBody)
	}
	if !equalStrings(orderFrom(slots), []string{c.asset, replacement.asset, a.asset}) {
		t.Errorf("order = %v", orderFrom(slots))
	}
	resp, slots, _ = s.setCaption(roomID, replacement.asset, "new words", s.token)
	if resp.StatusCode != http.StatusOK || captionFor(slots, replacement.asset) != "new words" {
		t.Errorf("captioning the replacement: %d %q", resp.StatusCode, captionFor(slots, replacement.asset))
	}
}

// MARK: - Refusals

func TestReplaceAPI_Rejections_ChangeNothing(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	uploaded := s.uploaded(newPhoto(t, 800, 600, "uploaded"))
	declaredOnly := newPhoto(t, 800, 600, "declared")
	s.initiate(declaredOnly)
	orphan := s.uploaded(newPhoto(t, 800, 600, "orphan"))
	liar := newPhoto(t, 1200, 800, "liar")
	liar.w, liar.h = 640, 480
	s.initiate(liar)
	s.put(liar)
	before := s.snapshotOwnerState()

	cases := []struct {
		name, photo, replacement string
		want                     int
		wantCode                 string
	}{
		{"same photograph", a.asset, a.asset, http.StatusBadRequest, "invalid_replacement"},
		{"empty replacement", a.asset, "", http.StatusBadRequest, "invalid_replacement"},
		{"photograph not in room", orphan.asset, uploaded.asset, http.StatusNotFound, "photo_not_in_room"},
		{"unknown photograph", "00000000-0000-4000-8000-000000000000", uploaded.asset, http.StatusNotFound, "photo_not_in_room"},
		{"replacement already in room", a.asset, b.asset, http.StatusConflict, "asset_already_assigned"},
		{"replacement not uploaded", a.asset, declaredOnly.asset, http.StatusConflict, "asset_not_uploaded"},
		{"replacement fails verification", a.asset, liar.asset, http.StatusUnprocessableEntity, "asset_invalid"},
		{"unknown replacement", a.asset, "00000000-0000-4000-8000-000000000000", http.StatusNotFound, "asset_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, _, errBody := s.replace(roomID, tc.photo, tc.replacement, s.token)
			if resp.StatusCode != tc.want || errBody["code"] != tc.wantCode {
				t.Errorf("got %d %v, want %d %s", resp.StatusCode, errBody, tc.want, tc.wantCode)
			}
			if s.snapshotOwnerState() != before {
				t.Error("a refused replacement must change nothing")
			}
		})
	}
	for _, id := range []string{uploaded.asset, declaredOnly.asset, liar.asset} {
		if s.assetState(id) != "pending" {
			t.Errorf("asset %s state = %s, want pending", id, s.assetState(id))
		}
	}
	if resp, _ := s.do(http.MethodPost, "/museum/me/rooms/"+roomID+"/photos/"+a.asset+"/replacement", "not json", s.token); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body: got %d, want 400", resp.StatusCode)
	}
	if resp, _, _ := s.replace("00000000-0000-4000-8000-000000000000", a.asset, uploaded.asset, s.token); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown room: got %d, want 404", resp.StatusCode)
	}
}

// MARK: - Rollback

func TestReplaceAPI_CommitFailureInsideTheTransaction_RollsBackEverything(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	if resp, _, _ := s.assign(roomID, []string{a.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	replacement := s.uploaded(newPhoto(t, 800, 600, "replacement"))
	if _, err := s.pool.Pool().Exec(context.Background(),
		`UPDATE assets SET state = 'committed', committed_at = now() WHERE id = $1`, replacement.asset); err != nil {
		t.Fatal(err)
	}
	before := s.snapshotOwnerState()

	resp, _, _ := s.replace(roomID, a.asset, replacement.asset, s.token)

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected the mismatched commit to fail the transaction")
	}
	if s.snapshotOwnerState() != before {
		t.Error("a failed replacement must leave slot, asset states, and captions exactly as they were")
	}
	if s.assetState(a.asset) != "committed" {
		t.Error("the original must not be released when the replacement failed to commit")
	}
}

// MARK: - Concurrency

func TestReplaceAPI_TwoRacersOnTheSamePhotograph_ExactlyOneWins(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 800, 600, "a"))
	b := s.uploaded(newPhoto(t, 800, 600, "b"))
	if resp, _, _ := s.assign(roomID, []string{a.asset, b.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("seed")
	}
	racers := []*photoFixture{
		s.uploaded(newPhoto(t, 800, 600, "r1")),
		s.uploaded(newPhoto(t, 800, 600, "r2")),
	}

	var wg sync.WaitGroup
	statuses := make(chan int, len(racers))
	for _, r := range racers {
		wg.Add(1)
		go func(r *photoFixture) {
			defer wg.Done()
			resp, _, _ := s.replace(roomID, b.asset, r.asset, s.token)
			statuses <- resp.StatusCode
		}(r)
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
	if s.slotCount(roomID) != 2 {
		t.Errorf("row count must be unchanged, got %d", s.slotCount(roomID))
	}
	committed, pending := 0, 0
	for _, r := range racers {
		switch s.assetState(r.asset) {
		case "committed":
			committed++
		case "pending":
			pending++
		}
	}
	if committed != 1 || pending != 1 {
		t.Errorf("exactly one racer may commit and the other must stay pending; committed=%d pending=%d", committed, pending)
	}
	if s.assetState(b.asset) != "released" {
		t.Error("the replaced asset must be released exactly once")
	}
	if s.assetState(a.asset) != "committed" {
		t.Error("the untouched photograph must remain committed")
	}
}

// MARK: - Degraded mode

func TestReplaceAPI_WithoutObjectStorage_Answers503(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()

	degraded := museumapp.NewMuseumService(museuminfra.NewPostgresMuseumRepository(s.pool.Pool()), catalinfra.NewPostgresCatalogRepository(s.pool.Pool()))
	router := platformhttp.NewRouter()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	museumiface.NewHandlers(degraded, identityiface.NewBearerAuthenticator(s.signer), logger).RegisterRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/museum/me/rooms/"+roomID+"/photos/00000000-0000-4000-8000-000000000000/replacement",
		bytes.NewReader([]byte(`{"asset_id":"00000000-0000-4000-8000-000000000001"}`)))
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := testHTTPClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 without object storage, got %d", resp.StatusCode)
	}
}
