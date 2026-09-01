package application_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"muse-backend/internal/museum/application"
	"muse-backend/internal/museum/domain"
)

// MARK: - fakeRepo: method

func (f *fakeRepo) ReorderPhotoSlots(_ context.Context, roomID domain.RoomID, orderedAssetIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failReorder != nil {
		return f.failReorder
	}
	f.reorderCalls++
	room, ok := f.roomsByID[roomID]
	if !ok {
		return domain.ErrRoomNotFound
	}
	byAsset := make(map[string]domain.PhotoSlotAssignment, len(room.PhotoSlots))
	for _, slot := range room.PhotoSlots {
		byAsset[slot.PhotoAssetID] = slot
	}
	reordered := make([]domain.PhotoSlotAssignment, 0, len(orderedAssetIDs))
	for newIndex, assetID := range orderedAssetIDs {
		slot, present := byAsset[assetID]
		if !present {
			return domain.ErrPhotoOrderMismatch
		}
		slot.SlotIndex = newIndex
		reordered = append(reordered, slot)
	}
	room.PhotoSlots = reordered
	f.roomsByID[roomID] = room
	f.roomWrites++
	return nil
}

func (f *fakeRepo) seedRoomSlots(roomID domain.RoomID, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	room := f.roomsByID[roomID]
	room.PhotoSlots = nil
	for i := 0; i < n; i++ {
		room.PhotoSlots = append(room.PhotoSlots, domain.PhotoSlotAssignment{
			SlotIndex: i, PhotoAssetID: fmt.Sprintf("asset_%d", i), Caption: fmt.Sprintf("caption for asset_%d", i),
		})
	}
	f.roomsByID[roomID] = room
}

func orderOf(room domain.Room) []string {
	out := make([]string, len(room.PhotoSlots))
	for _, slot := range room.PhotoSlots {
		out[slot.SlotIndex] = slot.PhotoAssetID
	}
	return out
}

func assetIDs(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("asset_%d", i))
	}
	return out
}

func requireOrder(t *testing.T, room domain.Room, want []string) {
	t.Helper()
	got := orderOf(room)
	if len(got) != len(want) {
		t.Fatalf("order length %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d holds %s, want %s (full: %v)", i, got[i], want[i], got)
		}
	}
	seen := map[int]bool{}
	for _, slot := range room.PhotoSlots {
		if slot.SlotIndex < 0 || slot.SlotIndex >= len(room.PhotoSlots) || seen[slot.SlotIndex] {
			t.Fatalf("slot indices are not contiguous 0..%d: %+v", len(room.PhotoSlots)-1, room.PhotoSlots)
		}
		seen[slot.SlotIndex] = true
	}
	for _, slot := range room.PhotoSlots {
		if slot.Caption != "caption for "+slot.PhotoAssetID {
			t.Fatalf("caption detached from its photograph: %+v", slot)
		}
	}
}

// MARK: - Tests

func TestReorderPhotos_TwoPhotoSwap(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	if err := h.service.ReorderPhotos(context.Background(), h.account, h.roomID, []string{"asset_1", "asset_0"}); err != nil {
		t.Fatalf("ReorderPhotos: %v", err)
	}

	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireOrder(t, room, []string{"asset_1", "asset_0"})
	if h.uow.committed != 1 || h.repo.lockCalls != 1 {
		t.Errorf("expected one locked, committed unit of work; got committed=%d locks=%d", h.uow.committed, h.repo.lockCalls)
	}
}

func TestReorderPhotos_ArbitraryPermutationOf28(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, domain.MaxPhotosPerRoom)
	want := assetIDs(domain.MaxPhotosPerRoom)
	rand.New(rand.NewSource(38)).Shuffle(len(want), func(i, j int) { want[i], want[j] = want[j], want[i] })

	if err := h.service.ReorderPhotos(context.Background(), h.account, h.roomID, want); err != nil {
		t.Fatalf("ReorderPhotos: %v", err)
	}

	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireOrder(t, room, want)
}

func TestReorderPhotos_CrossWallMove_IsJustAPermutation(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, domain.MaxPhotosPerRoom)
	want := assetIDs(domain.MaxPhotosPerRoom)
	want[0], want[27] = want[27], want[0]

	if err := h.service.ReorderPhotos(context.Background(), h.account, h.roomID, want); err != nil {
		t.Fatalf("ReorderPhotos: %v", err)
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireOrder(t, room, want)
}

func TestReorderPhotos_SinglePhoto_IdentityIsANoOp(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 1)

	if err := h.service.ReorderPhotos(context.Background(), h.account, h.roomID, []string{"asset_0"}); err != nil {
		t.Fatalf("ReorderPhotos: %v", err)
	}
	if h.repo.reorderCalls != 0 {
		t.Errorf("an identity order must not write; got %d writes", h.repo.reorderCalls)
	}
}

func TestReorderPhotos_SubmittingTheCurrentOrder_IsANoOp(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 4)
	want := []string{"asset_2", "asset_0", "asset_3", "asset_1"}

	if err := h.service.ReorderPhotos(context.Background(), h.account, h.roomID, want); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := h.service.ReorderPhotos(context.Background(), h.account, h.roomID, want); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if h.repo.reorderCalls != 1 {
		t.Errorf("retrying the same order must not write again; got %d writes", h.repo.reorderCalls)
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireOrder(t, room, want)
}

func TestReorderPhotos_MalformedOrders_AreInvalid(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 3)
	ctx := context.Background()

	cases := map[string][]string{
		"empty":        {},
		"duplicate":    {"asset_0", "asset_0", "asset_1"},
		"blank id":     {"asset_0", "", "asset_1"},
		"over the cap": assetIDs(domain.MaxPhotosPerRoom + 1),
	}
	for name, order := range cases {
		t.Run(name, func(t *testing.T) {
			err := h.service.ReorderPhotos(ctx, h.account, h.roomID, order)
			if !errors.Is(err, domain.ErrInvalidPhotoOrder) {
				t.Fatalf("expected ErrInvalidPhotoOrder, got %v", err)
			}
		})
	}
	if h.uow.runs != 0 {
		t.Error("malformed input must be refused before any transaction")
	}
}

func TestReorderPhotos_NotAPermutationOfTheRoom_IsAMismatch(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 3)
	ctx := context.Background()

	cases := map[string][]string{
		"foreign asset": {"asset_0", "asset_1", "someone_elses"},
		"missing asset": {"asset_0", "asset_1"},
		"too many":      {"asset_0", "asset_1", "asset_2", "asset_3"},
	}
	for name, order := range cases {
		t.Run(name, func(t *testing.T) {
			err := h.service.ReorderPhotos(ctx, h.account, h.roomID, order)
			if !errors.Is(err, domain.ErrPhotoOrderMismatch) {
				t.Fatalf("expected ErrPhotoOrderMismatch, got %v", err)
			}
		})
	}
	room, _ := h.repo.FindRoom(ctx, h.roomID)
	requireOrder(t, room, []string{"asset_0", "asset_1", "asset_2"})
	if h.uow.committed != 0 {
		t.Errorf("a mismatch must roll back, never commit; committed=%d", h.uow.committed)
	}
}

func TestReorderPhotos_NonOwner_IsRefusedBeforeAnyLock(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	err := h.service.ReorderPhotos(context.Background(), "acct_stranger", h.roomID, []string{"asset_1", "asset_0"})

	if !errors.Is(err, domain.ErrMuseumNotFound) {
		t.Fatalf("expected the stranger's missing Museum to refuse, got %v", err)
	}
	if h.uow.runs != 0 || h.repo.lockCalls != 0 {
		t.Error("a non-owner must never reach the lock or a transaction")
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireOrder(t, room, []string{"asset_0", "asset_1"})
}

func TestReorderPhotos_WithoutAUnitOfWork_Is503Shaped(t *testing.T) {
	service := application.NewMuseumService(newFakeRepo(), newFakeCatalog())

	err := service.ReorderPhotos(context.Background(), "acct", "room", []string{"a"})

	if !errors.Is(err, domain.ErrTransactionsUnavailable) {
		t.Fatalf("expected ErrTransactionsUnavailable, got %v", err)
	}
}

func TestReorderPhotos_RepositoryFailure_RollsBack(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 3)
	h.repo.failReorder = errors.New("simulated write failure")

	err := h.service.ReorderPhotos(context.Background(), h.account, h.roomID, []string{"asset_2", "asset_1", "asset_0"})

	if err == nil {
		t.Fatal("expected the failure to surface")
	}
	if h.uow.rolledBack != 1 {
		t.Errorf("expected one rollback, got %d", h.uow.rolledBack)
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireOrder(t, room, []string{"asset_0", "asset_1", "asset_2"})
}
