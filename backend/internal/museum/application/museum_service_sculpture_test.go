package application_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/museum/application"
	"muse-backend/internal/museum/domain"
)

// MARK: - fakeRepo: methods

func (f *fakeRepo) InsertSculpture(_ context.Context, roomID domain.RoomID, sculpture domain.SculptureInstance) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failInsertSculpture != nil {
		return f.failInsertSculpture
	}
	room, ok := f.roomsByID[roomID]
	if !ok {
		return domain.ErrRoomNotFound
	}
	if !domain.IsValidSculptureSlotIndex(sculpture.SlotIndex) {
		return domain.ErrInvalidSculptureSlot
	}
	for _, existing := range room.Sculptures {
		if existing.SlotIndex == sculpture.SlotIndex {
			return domain.ErrSlotOccupied
		}
	}
	room.Sculptures = append(room.Sculptures, sculpture)
	f.roomsByID[roomID] = room
	f.sculptureWrites++
	return nil
}

func (f *fakeRepo) DeleteSculpture(_ context.Context, roomID domain.RoomID, slotIndex int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDeleteSculpture != nil {
		return f.failDeleteSculpture
	}
	room, ok := f.roomsByID[roomID]
	if !ok {
		return domain.ErrRoomNotFound
	}
	remaining := make([]domain.SculptureInstance, 0, len(room.Sculptures))
	found := false
	for _, existing := range room.Sculptures {
		if existing.SlotIndex == slotIndex {
			found = true
			continue
		}
		remaining = append(remaining, existing)
	}
	if !found {
		return domain.ErrSculptureNotInRoom
	}
	room.Sculptures = remaining
	f.roomsByID[roomID] = room
	f.sculptureWrites++
	return nil
}

// MARK: - Helpers

func sculptureSlots(t *testing.T, repo *fakeRepo, roomID domain.RoomID) []int {
	t.Helper()
	room, err := repo.FindRoom(context.Background(), roomID)
	if err != nil {
		t.Fatalf("find room: %v", err)
	}
	out := make([]int, 0, len(room.Sculptures))
	for _, sculpture := range room.Sculptures {
		out = append(out, sculpture.SlotIndex)
	}
	return out
}

func requireSculptureSlots(t *testing.T, got []int, want ...int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("sculpture slots = %v, want %v", got, want)
	}
	seen := map[int]bool{}
	for _, index := range got {
		seen[index] = true
	}
	for _, index := range want {
		if !seen[index] {
			t.Fatalf("sculpture slots = %v, want %v", got, want)
		}
	}
}

func newSculptureHarness(t *testing.T, catalogIDs ...string) *photoHarness {
	t.Helper()
	h := newPhotoHarness(t)
	for _, id := range catalogIDs {
		h.catalog.sculptures[id] = true
	}
	return h
}

// MARK: - Adding

func TestAddSculpture_PlacesAtTheLowestFreeSlot(t *testing.T) {
	h := newSculptureHarness(t, "sculpture_one", "sculpture_two")
	ctx := context.Background()

	first, err := h.service.AddSculpture(ctx, h.account, h.roomID, "sculpture_one")
	if err != nil {
		t.Fatalf("first add: %v", err)
	}
	second, err := h.service.AddSculpture(ctx, h.account, h.roomID, "sculpture_two")
	if err != nil {
		t.Fatalf("second add: %v", err)
	}

	if first.SlotIndex != 0 || second.SlotIndex != 1 {
		t.Errorf("slots = %d, %d; want 0, 1", first.SlotIndex, second.SlotIndex)
	}
	if first.CatalogID != "sculpture_one" || second.CatalogID != "sculpture_two" {
		t.Errorf("catalog ids not carried: %+v %+v", first, second)
	}
	requireSculptureSlots(t, sculptureSlots(t, h.repo, h.roomID), 0, 1)
	if h.uow.committed != 2 || h.repo.lockCalls != 2 {
		t.Errorf("each add must be one locked, committed unit of work; committed=%d locks=%d", h.uow.committed, h.repo.lockCalls)
	}
}

func TestAddSculpture_TheSameSculptureTwice_IsAllowed(t *testing.T) {
	h := newSculptureHarness(t, "sculpture_one")
	ctx := context.Background()

	if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, "sculpture_one"); err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := h.service.AddSculpture(ctx, h.account, h.roomID, "sculpture_one")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.SlotIndex != 1 {
		t.Errorf("second copy slot = %d, want 1", second.SlotIndex)
	}
}

func TestAddSculpture_FourthIsRefused(t *testing.T) {
	h := newSculptureHarness(t, "s")
	ctx := context.Background()
	for i := 0; i < domain.MaxSculpturesPerRoom; i++ {
		if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, "s"); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	writes := h.repo.sculptureWrites

	_, err := h.service.AddSculpture(ctx, h.account, h.roomID, "s")

	if !errors.Is(err, domain.ErrSculptureCapacityReached) {
		t.Fatalf("expected ErrSculptureCapacityReached, got %v", err)
	}
	if h.repo.sculptureWrites != writes {
		t.Error("a refused add must not write")
	}
	requireSculptureSlots(t, sculptureSlots(t, h.repo, h.roomID), 0, 1, 2)
}

func TestAddSculpture_WithAnEmptyCatalog_IsRefused(t *testing.T) {
	h := newPhotoHarness(t)
	ctx := context.Background()

	_, err := h.service.AddSculpture(ctx, h.account, h.roomID, "sculpture_anything")

	if !errors.Is(err, domain.ErrUnknownSculpture) {
		t.Fatalf("expected ErrUnknownSculpture, got %v", err)
	}
	if h.uow.runs != 0 || h.repo.lockCalls != 0 {
		t.Error("an unknown sculpture must be refused before any lock or transaction")
	}
	if len(sculptureSlots(t, h.repo, h.roomID)) != 0 {
		t.Error("nothing may be placed")
	}
}

func TestAddSculpture_UnknownOrEmptyCatalogID_IsRefused(t *testing.T) {
	h := newSculptureHarness(t, "sculpture_one")
	ctx := context.Background()

	for name, id := range map[string]string{"unknown": "sculpture_invented", "empty": ""} {
		t.Run(name, func(t *testing.T) {
			if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, id); !errors.Is(err, domain.ErrUnknownSculpture) {
				t.Fatalf("expected ErrUnknownSculpture, got %v", err)
			}
		})
	}
	if h.repo.sculptureWrites != 0 {
		t.Error("a refused add must not write")
	}
}

func TestAddSculpture_NonOwner_IsRefusedBeforeTheCatalogOrAnyLock(t *testing.T) {
	h := newSculptureHarness(t, "sculpture_one")

	_, err := h.service.AddSculpture(context.Background(), "acct_stranger", h.roomID, "sculpture_one")

	if !errors.Is(err, domain.ErrMuseumNotFound) {
		t.Fatalf("expected the stranger's missing Museum to refuse, got %v", err)
	}
	if h.uow.runs != 0 || h.repo.lockCalls != 0 || h.repo.sculptureWrites != 0 {
		t.Error("a non-owner must never reach the lock, a transaction, or a write")
	}
}

func TestAddSculpture_WithoutAUnitOfWork_Is503Shaped(t *testing.T) {
	service := application.NewMuseumService(newFakeRepo(), newFakeCatalog())

	_, err := service.AddSculpture(context.Background(), "acct", "room", "s")

	if !errors.Is(err, domain.ErrTransactionsUnavailable) {
		t.Fatalf("expected ErrTransactionsUnavailable, got %v", err)
	}
}

func TestAddSculpture_RepositoryFailure_RollsBack(t *testing.T) {
	h := newSculptureHarness(t, "s")
	h.repo.failInsertSculpture = errors.New("simulated write failure")

	_, err := h.service.AddSculpture(context.Background(), h.account, h.roomID, "s")

	if err == nil {
		t.Fatal("expected the failure to surface")
	}
	if h.uow.rolledBack != 1 || h.uow.committed != 0 {
		t.Errorf("expected rollback without commit; rolledBack=%d committed=%d", h.uow.rolledBack, h.uow.committed)
	}
	if len(sculptureSlots(t, h.repo, h.roomID)) != 0 {
		t.Error("nothing may survive a rolled-back add")
	}
}

// MARK: - Removing

func TestRemoveSculpture_LeavesTheSlotEmpty_AndMovesNothingElse(t *testing.T) {
	h := newSculptureHarness(t, "a", "b", "c")
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, id); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	if err := h.service.RemoveSculpture(ctx, h.account, h.roomID, 1); err != nil {
		t.Fatalf("RemoveSculpture: %v", err)
	}

	requireSculptureSlots(t, sculptureSlots(t, h.repo, h.roomID), 0, 2)
	room, _ := h.repo.FindRoom(ctx, h.roomID)
	for _, sculpture := range room.Sculptures {
		switch sculpture.SlotIndex {
		case 0:
			if sculpture.CatalogID != "a" {
				t.Errorf("slot 0 holds %q, want a — nothing may move", sculpture.CatalogID)
			}
		case 2:
			if sculpture.CatalogID != "c" {
				t.Errorf("slot 2 holds %q, want c — nothing may move", sculpture.CatalogID)
			}
		}
	}
}

func TestRemoveSculpture_ThenAdd_ReusesTheFreedSlot(t *testing.T) {
	h := newSculptureHarness(t, "a", "b", "c", "d")
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, id); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := h.service.RemoveSculpture(ctx, h.account, h.roomID, 0); err != nil {
		t.Fatalf("remove: %v", err)
	}

	placed, err := h.service.AddSculpture(ctx, h.account, h.roomID, "d")
	if err != nil {
		t.Fatalf("add after remove: %v", err)
	}

	if placed.SlotIndex != 0 {
		t.Errorf("the freed slot 0 must be reused, got %d", placed.SlotIndex)
	}
	requireSculptureSlots(t, sculptureSlots(t, h.repo, h.roomID), 0, 1, 2)
}

func TestRemoveSculpture_FromAFullRoom_MakesRoomForExactlyOneMore(t *testing.T) {
	h := newSculptureHarness(t, "s")
	ctx := context.Background()
	for i := 0; i < domain.MaxSculpturesPerRoom; i++ {
		if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, "s"); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := h.service.RemoveSculpture(ctx, h.account, h.roomID, 2); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, "s"); err != nil {
		t.Fatalf("add after remove: %v", err)
	}
	if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, "s"); !errors.Is(err, domain.ErrSculptureCapacityReached) {
		t.Errorf("the Room is full again and must refuse; got %v", err)
	}
}

func TestRemoveSculpture_EmptySlot_IsRefused(t *testing.T) {
	h := newSculptureHarness(t, "a")
	ctx := context.Background()
	if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, "a"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := h.service.RemoveSculpture(ctx, h.account, h.roomID, 1); !errors.Is(err, domain.ErrSculptureNotInRoom) {
		t.Fatalf("expected ErrSculptureNotInRoom for an empty slot, got %v", err)
	}
	if err := h.service.RemoveSculpture(ctx, h.account, h.roomID, 0); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := h.service.RemoveSculpture(ctx, h.account, h.roomID, 0); !errors.Is(err, domain.ErrSculptureNotInRoom) {
		t.Errorf("a repeat must be refused, got %v", err)
	}
}

func TestRemoveSculpture_SlotOutsideTheCap_IsRefusedBeforeAnyIO(t *testing.T) {
	h := newSculptureHarness(t, "a")

	for _, slot := range []int{-1, domain.MaxSculpturesPerRoom, 99} {
		if err := h.service.RemoveSculpture(context.Background(), h.account, h.roomID, slot); !errors.Is(err, domain.ErrInvalidSculptureSlot) {
			t.Errorf("slot %d: expected ErrInvalidSculptureSlot, got %v", slot, err)
		}
	}
	if h.repo.sculptureWrites != 0 {
		t.Error("an out-of-range slot must not write")
	}
}

func TestRemoveSculpture_NonOwner_IsRefused(t *testing.T) {
	h := newSculptureHarness(t, "a")
	ctx := context.Background()
	if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, "a"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := h.service.RemoveSculpture(ctx, "acct_stranger", h.roomID, 0)

	if !errors.Is(err, domain.ErrMuseumNotFound) {
		t.Fatalf("expected ErrMuseumNotFound, got %v", err)
	}
	requireSculptureSlots(t, sculptureSlots(t, h.repo, h.roomID), 0)
}

// MARK: - Sculptures do not disturb photographs

func TestSculptures_DoNotTouchPhotographsOrTheAssetLifecycle(t *testing.T) {
	h := newSculptureHarness(t, "a", "b")
	h.assets.owned["photo_1"] = true
	h.assets.owned["photo_2"] = true
	ctx := context.Background()
	if _, err := h.service.AddPhotos(ctx, h.account, h.roomID, []string{"photo_1", "photo_2"}); err != nil {
		t.Fatalf("seed photos: %v", err)
	}
	if err := h.service.SetPhotoCaption(ctx, h.account, h.roomID, "photo_1", "kept"); err != nil {
		t.Fatalf("caption: %v", err)
	}
	commitsBefore, releasesBefore := len(h.assets.committed), len(h.assets.released)

	if _, err := h.service.AddSculpture(ctx, h.account, h.roomID, "a"); err != nil {
		t.Fatalf("add sculpture: %v", err)
	}
	if err := h.service.RemoveSculpture(ctx, h.account, h.roomID, 0); err != nil {
		t.Fatalf("remove sculpture: %v", err)
	}

	room, _ := h.repo.FindRoom(ctx, h.roomID)
	if len(room.PhotoSlots) != 2 {
		t.Fatalf("photo count changed: %d", len(room.PhotoSlots))
	}
	if got := orderOf(room); got[0] != "photo_1" || got[1] != "photo_2" {
		t.Errorf("photo order changed: %v", got)
	}
	if captionOf(t, h.repo, h.roomID, "photo_1") != "kept" {
		t.Error("a sculpture operation must not touch a caption")
	}
	if len(h.assets.committed) != commitsBefore || len(h.assets.released) != releasesBefore {
		t.Error("sculptures must never commit or release a media asset")
	}
	if h.repo.reorderCalls != 0 || h.repo.deleteCalls != 0 || h.repo.replaceCalls != 0 {
		t.Error("a sculpture operation must not reorder, delete, or replace a photograph")
	}
}
