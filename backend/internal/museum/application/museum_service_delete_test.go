package application_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/museum/application"
	"muse-backend/internal/museum/domain"
)

// MARK: - fakeRepo: method

func (f *fakeRepo) DeletePhotoSlotCompacting(_ context.Context, roomID domain.RoomID, photoAssetID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDelete != nil {
		return f.failDelete
	}
	room, ok := f.roomsByID[roomID]
	if !ok {
		return domain.ErrRoomNotFound
	}
	removedIndex := -1
	remaining := make([]domain.PhotoSlotAssignment, 0, len(room.PhotoSlots))
	for _, slot := range room.PhotoSlots {
		if slot.PhotoAssetID == photoAssetID {
			removedIndex = slot.SlotIndex
			continue
		}
		remaining = append(remaining, slot)
	}
	if removedIndex < 0 {
		return domain.ErrPhotoNotInRoom
	}
	for index := range remaining {
		if remaining[index].SlotIndex > removedIndex {
			remaining[index].SlotIndex--
		}
	}
	room.PhotoSlots = remaining
	f.roomsByID[roomID] = room
	f.deleteCalls++
	f.roomWrites++
	return nil
}

func (f *fakeRepo) breakLayout(roomID domain.RoomID, assetID string, index int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	room := f.roomsByID[roomID]
	for i := range room.PhotoSlots {
		if room.PhotoSlots[i].PhotoAssetID == assetID {
			room.PhotoSlots[i].SlotIndex = index
		}
	}
	f.roomsByID[roomID] = room
}

// MARK: - Compaction

func TestDeletePhoto_RemovesThePhotograph_AndCompactsTheRemaining(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 5)

	if err := h.service.DeletePhoto(context.Background(), h.account, h.roomID, "asset_2"); err != nil {
		t.Fatalf("DeletePhoto: %v", err)
	}

	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireOrder(t, room, []string{"asset_0", "asset_1", "asset_3", "asset_4"})
	if hasAsset(h.repo, h.roomID, "asset_2") {
		t.Error("the deleted photograph must be gone")
	}
}

func TestDeletePhoto_First_Last_AndOnly(t *testing.T) {
	cases := []struct {
		name   string
		seed   int
		delete string
		want   []string
	}{
		{"first of four", 4, "asset_0", []string{"asset_1", "asset_2", "asset_3"}},
		{"last of four", 4, "asset_3", []string{"asset_0", "asset_1", "asset_2"}},
		{"the only one", 1, "asset_0", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newPhotoHarness(t)
			h.repo.seedRoomSlots(h.roomID, tc.seed)
			if err := h.service.DeletePhoto(context.Background(), h.account, h.roomID, tc.delete); err != nil {
				t.Fatalf("DeletePhoto: %v", err)
			}
			room, _ := h.repo.FindRoom(context.Background(), h.roomID)
			requireOrder(t, room, tc.want)
		})
	}
}

func TestDeletePhoto_ReleasesTheAsset_InTheSameUnitOfWork(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 3)

	if err := h.service.DeletePhoto(context.Background(), h.account, h.roomID, "asset_1"); err != nil {
		t.Fatalf("DeletePhoto: %v", err)
	}

	if h.uow.runs != 1 || h.uow.committed != 1 || h.repo.lockCalls != 1 {
		t.Errorf("expected one locked, committed unit of work; got runs=%d committed=%d locks=%d", h.uow.runs, h.uow.committed, h.repo.lockCalls)
	}
	if len(h.assets.released) != 1 || len(h.assets.released[0]) != 1 || h.assets.released[0][0] != "asset_1" {
		t.Errorf("exactly the deleted asset must be released; got %v", h.assets.released)
	}
	if len(h.assets.committed) != 0 || len(h.assets.verified) != 0 {
		t.Error("deletion commits and verifies nothing")
	}
}

// MARK: - Refusals

func TestDeletePhoto_PhotographNotInRoom_IsRefused_AndNothingIsReleased(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	for name, target := range map[string]string{
		"unknown photograph": "not_here",
		"empty photograph":   "",
	} {
		t.Run(name, func(t *testing.T) {
			err := h.service.DeletePhoto(context.Background(), h.account, h.roomID, target)
			if !errors.Is(err, domain.ErrPhotoNotInRoom) {
				t.Fatalf("expected ErrPhotoNotInRoom, got %v", err)
			}
		})
	}
	if h.repo.deleteCalls != 0 || len(h.assets.released) != 0 {
		t.Error("a refused target must delete or release nothing")
	}
}

func TestDeletePhoto_Repeat_IsNotInRoom_AndChangesNothing(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 3)
	if err := h.service.DeletePhoto(context.Background(), h.account, h.roomID, "asset_1"); err != nil {
		t.Fatalf("first: %v", err)
	}
	releases := len(h.assets.released)

	err := h.service.DeletePhoto(context.Background(), h.account, h.roomID, "asset_1")

	if !errors.Is(err, domain.ErrPhotoNotInRoom) {
		t.Fatalf("expected ErrPhotoNotInRoom on repeat, got %v", err)
	}
	if len(h.assets.released) != releases {
		t.Error("a repeat must not release anything again")
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireOrder(t, room, []string{"asset_0", "asset_2"})
}

func TestDeletePhoto_NonOwner_IsRefusedBeforeAnyLock(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	err := h.service.DeletePhoto(context.Background(), "acct_stranger", h.roomID, "asset_0")

	if !errors.Is(err, domain.ErrMuseumNotFound) {
		t.Fatalf("expected the stranger's missing Museum to refuse, got %v", err)
	}
	if h.uow.runs != 0 || h.repo.lockCalls != 0 || len(h.assets.released) != 0 {
		t.Error("a non-owner must never reach the lock, a transaction, or a release")
	}
	if !hasAsset(h.repo, h.roomID, "asset_0") {
		t.Error("the owner's photograph must be untouched")
	}
}

func TestDeletePhoto_WithoutObjectStorage_Is503Shaped(t *testing.T) {
	service := application.NewMuseumService(newFakeRepo(), newFakeCatalog())

	err := service.DeletePhoto(context.Background(), "acct", "room", "asset")

	if !errors.Is(err, domain.ErrPhotosUnavailable) {
		t.Fatalf("expected ErrPhotosUnavailable, got %v", err)
	}
}

func TestDeletePhoto_InconsistentLayout_IsRefused(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 3)
	h.repo.breakLayout(h.roomID, "asset_2", 9)

	err := h.service.DeletePhoto(context.Background(), h.account, h.roomID, "asset_0")

	if !errors.Is(err, domain.ErrSlotLayoutInconsistent) {
		t.Fatalf("expected ErrSlotLayoutInconsistent, got %v", err)
	}
	if h.repo.deleteCalls != 0 || len(h.assets.released) != 0 {
		t.Error("nothing may be deleted or released from an inconsistent Room")
	}
}

// MARK: - All-or-nothing

func TestDeletePhoto_ReleaseFailure_RollsBackTheRemoval(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 3)
	h.assets.releaseErr = errors.New("simulated release failure")

	err := h.service.DeletePhoto(context.Background(), h.account, h.roomID, "asset_1")

	if err == nil {
		t.Fatal("expected the release failure to surface")
	}
	if h.uow.rolledBack != 1 || h.uow.committed != 0 {
		t.Errorf("expected one rollback and no commit; got rolledBack=%d committed=%d", h.uow.rolledBack, h.uow.committed)
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireOrder(t, room, []string{"asset_0", "asset_1", "asset_2"})
}

func TestDeletePhoto_RepositoryFailure_RollsBack_AndReleasesNothing(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)
	h.repo.failDelete = errors.New("simulated write failure")

	err := h.service.DeletePhoto(context.Background(), h.account, h.roomID, "asset_0")

	if err == nil {
		t.Fatal("expected the failure to surface")
	}
	if h.uow.rolledBack != 1 || len(h.assets.released) != 0 {
		t.Error("a repository failure must roll back before any asset state moves")
	}
}

// MARK: - The compacted Room works with every other operation

func TestDeletePhoto_ThenAppend_TakesTheNextPosition(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 5)
	ctx := context.Background()
	if err := h.service.DeletePhoto(ctx, h.account, h.roomID, "asset_2"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	slots, err := h.service.AddPhotos(ctx, h.account, h.roomID, []string{"new"})
	if err != nil {
		t.Fatalf("AddPhotos after delete: %v", err)
	}

	if len(slots) != 1 || slots[0].SlotIndex != 4 {
		t.Fatalf("the new photograph must take position 4, got %+v", slots)
	}
	room, _ := h.repo.FindRoom(ctx, h.roomID)
	got := orderOf(room)
	want := []string{"asset_0", "asset_1", "asset_3", "asset_4", "new"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestDeletePhoto_FromAFullRoom_MakesRoomForExactlyOneMore(t *testing.T) {
	h := newPhotoHarness(t, "new", "another")
	h.repo.seedRoomSlots(h.roomID, domain.MaxPhotosPerRoom)
	ctx := context.Background()
	if _, err := h.service.AddPhotos(ctx, h.account, h.roomID, []string{"new"}); !errors.Is(err, domain.ErrPhotoCapacityReached) {
		t.Fatalf("a full Room must refuse; got %v", err)
	}

	if err := h.service.DeletePhoto(ctx, h.account, h.roomID, "asset_13"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	slots, err := h.service.AddPhotos(ctx, h.account, h.roomID, []string{"new"})
	if err != nil {
		t.Fatalf("add after delete: %v", err)
	}
	if slots[0].SlotIndex != domain.MaxPhotosPerRoom-1 {
		t.Errorf("the freed position is the last one, %d; got %d", domain.MaxPhotosPerRoom-1, slots[0].SlotIndex)
	}
	if _, err := h.service.AddPhotos(ctx, h.account, h.roomID, []string{"another"}); !errors.Is(err, domain.ErrPhotoCapacityReached) {
		t.Errorf("the Room is full again and must refuse; got %v", err)
	}
}

func TestDeletePhoto_ThenReorderAndCaption_UseTheCompactedRoom(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 4)
	ctx := context.Background()
	if err := h.service.DeletePhoto(ctx, h.account, h.roomID, "asset_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if err := h.service.ReorderPhotos(ctx, h.account, h.roomID, []string{"asset_3", "asset_1", "asset_0", "asset_2"}); !errors.Is(err, domain.ErrPhotoOrderMismatch) {
		t.Errorf("expected ErrPhotoOrderMismatch for a stale order, got %v", err)
	}
	if err := h.service.ReorderPhotos(ctx, h.account, h.roomID, []string{"asset_3", "asset_0", "asset_2"}); err != nil {
		t.Fatalf("reorder of the compacted Room: %v", err)
	}
	room, _ := h.repo.FindRoom(ctx, h.roomID)
	requireOrder(t, room, []string{"asset_3", "asset_0", "asset_2"})

	if err := h.service.SetPhotoCaption(ctx, h.account, h.roomID, "asset_1", "x"); !errors.Is(err, domain.ErrPhotoNotInRoom) {
		t.Errorf("captioning the deleted photograph must be refused, got %v", err)
	}
	if err := h.service.SetPhotoCaption(ctx, h.account, h.roomID, "asset_2", "still here"); err != nil {
		t.Fatalf("caption on a remaining photograph: %v", err)
	}
	if got := captionOf(t, h.repo, h.roomID, "asset_2"); got != "still here" {
		t.Errorf("caption = %q", got)
	}
}
