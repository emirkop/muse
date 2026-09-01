package application_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/museum/application"
	"muse-backend/internal/museum/domain"
)

// MARK: - fakeRepo: method

func (f *fakeRepo) ReplacePhotoSlotAsset(_ context.Context, roomID domain.RoomID, photoAssetID string, replacementAssetID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failReplace != nil {
		return f.failReplace
	}
	for _, slot := range f.allSlots() {
		if slot.PhotoAssetID == replacementAssetID {
			return &domain.PhotoAssetError{AssetID: replacementAssetID, Err: domain.ErrPhotoAssetAlreadyAssigned}
		}
	}
	room, ok := f.roomsByID[roomID]
	if !ok {
		return domain.ErrRoomNotFound
	}
	for index := range room.PhotoSlots {
		if room.PhotoSlots[index].PhotoAssetID == photoAssetID {
			room.PhotoSlots[index].PhotoAssetID = replacementAssetID
			f.roomsByID[roomID] = room
			f.replaceCalls++
			f.roomWrites++
			return nil
		}
	}
	return domain.ErrPhotoNotInRoom
}

func slotOf(t *testing.T, repo *fakeRepo, roomID domain.RoomID, assetID string) domain.PhotoSlotAssignment {
	t.Helper()
	room, err := repo.FindRoom(context.Background(), roomID)
	if err != nil {
		t.Fatalf("find room: %v", err)
	}
	for _, slot := range room.PhotoSlots {
		if slot.PhotoAssetID == assetID {
			return slot
		}
	}
	t.Fatalf("asset %s not in room", assetID)
	return domain.PhotoSlotAssignment{}
}

func hasAsset(repo *fakeRepo, roomID domain.RoomID, assetID string) bool {
	room, _ := repo.FindRoom(context.Background(), roomID)
	for _, slot := range room.PhotoSlots {
		if slot.PhotoAssetID == assetID {
			return true
		}
	}
	return false
}

// MARK: - The happy path

func TestReplacePhoto_SwapsTheAsset_KeepingSlotIndexAndCaption(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 4)
	before := slotOf(t, h.repo, h.roomID, "asset_2")

	if err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_2", "new"); err != nil {
		t.Fatalf("ReplacePhoto: %v", err)
	}

	after := slotOf(t, h.repo, h.roomID, "new")
	if after.SlotIndex != before.SlotIndex {
		t.Errorf("slot index moved: %d → %d — replacement must preserve position", before.SlotIndex, after.SlotIndex)
	}
	if after.Caption != before.Caption {
		t.Errorf("caption changed: %q → %q — requires it preserved", before.Caption, after.Caption)
	}
	if hasAsset(h.repo, h.roomID, "asset_2") {
		t.Error("the replaced photograph must no longer hang in the Room")
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireSameOrder(t, []string{"asset_0", "asset_1", "new", "asset_3"}, orderOf(room))
}

func TestReplacePhoto_PreservesTheCaption_PD009(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 2)
	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", "Trabzon, 1998"); err != nil {
		t.Fatalf("caption: %v", err)
	}

	if err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_0", "new"); err != nil {
		t.Fatalf("ReplacePhoto: %v", err)
	}

	if got := slotOf(t, h.repo, h.roomID, "new").Caption; got != "Trabzon, 1998" {
		t.Errorf("caption after replacement = %q, want the original", got)
	}
	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "new", "Trabzon, 1999"); err != nil {
		t.Fatalf("caption after replace: %v", err)
	}
	if got := slotOf(t, h.repo, h.roomID, "new").Caption; got != "Trabzon, 1999" {
		t.Errorf("caption edit after replacement did not apply: %q", got)
	}
}

func TestReplacePhoto_CommitsTheNewAsset_AndReleasesTheOld_InOneTransaction(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 3)

	if err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_1", "new"); err != nil {
		t.Fatalf("ReplacePhoto: %v", err)
	}

	if h.uow.committed != 1 || h.uow.runs != 1 || h.repo.lockCalls != 1 {
		t.Errorf("expected exactly one locked, committed unit of work; got runs=%d committed=%d locks=%d", h.uow.runs, h.uow.committed, h.repo.lockCalls)
	}
	if len(h.assets.committed) != 1 || len(h.assets.committed[0]) != 1 || h.assets.committed[0][0] != "new" {
		t.Errorf("exactly the replacement must commit; got %v", h.assets.committed)
	}
	if len(h.assets.released) != 1 || len(h.assets.released[0]) != 1 || h.assets.released[0][0] != "asset_1" {
		t.Errorf("exactly the replaced asset must be released; got %v", h.assets.released)
	}
}

func TestReplacePhoto_VerifiesTheReplacementBeforeOpeningTheTransaction(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)
	h.assets.verifyErr = &domain.PhotoAssetError{AssetID: "new", Err: domain.ErrPhotoAssetNotUploaded}

	err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_0", "new")

	if !errors.Is(err, domain.ErrPhotoAssetNotUploaded) {
		t.Fatalf("expected ErrPhotoAssetNotUploaded, got %v", err)
	}
	if len(h.assets.verified) != 1 || h.assets.verified[0][0] != "new" {
		t.Errorf("only the replacement is verified; got %v", h.assets.verified)
	}
	if h.uow.runs != 0 || h.repo.lockCalls != 0 {
		t.Error("a failed verification must not open a transaction or take a lock")
	}
	if !hasAsset(h.repo, h.roomID, "asset_0") {
		t.Error("nothing may change when verification fails")
	}
}

// MARK: - Idempotency

func TestReplacePhoto_RetryAfterSuccess_IsIdempotent(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 3)
	if err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_1", "new"); err != nil {
		t.Fatalf("first: %v", err)
	}
	writes := h.repo.replaceCalls
	commits, releases := len(h.assets.committed), len(h.assets.released)

	if err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_1", "new"); err != nil {
		t.Fatalf("retry must succeed, got %v", err)
	}

	if h.repo.replaceCalls != writes {
		t.Error("a retry must not write the slot again")
	}
	if len(h.assets.committed) != commits || len(h.assets.released) != releases {
		t.Error("a retry must not commit or release anything again")
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireSameOrder(t, []string{"asset_0", "new", "asset_2"}, orderOf(room))
}

// MARK: - Refusals

func TestReplacePhoto_PhotographNotInRoom_IsRefused(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 2)

	for name, target := range map[string]string{
		"unknown photograph": "not_here",
		"empty photograph":   "",
	} {
		t.Run(name, func(t *testing.T) {
			err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, target, "new")
			if !errors.Is(err, domain.ErrPhotoNotInRoom) {
				t.Fatalf("expected ErrPhotoNotInRoom, got %v", err)
			}
		})
	}
	if h.repo.replaceCalls != 0 || len(h.assets.committed) != 0 || len(h.assets.released) != 0 {
		t.Error("a refused target must write, commit, or release nothing")
	}
}

func TestReplacePhoto_MissingOrSelfReplacement_IsInvalidBeforeAnyIO(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	for name, replacement := range map[string]string{
		"empty replacement": "",
		"same photograph":   "asset_0",
	} {
		t.Run(name, func(t *testing.T) {
			err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_0", replacement)
			if !errors.Is(err, domain.ErrInvalidReplacement) {
				t.Fatalf("expected ErrInvalidReplacement, got %v", err)
			}
		})
	}
	if len(h.assets.verified) != 0 || h.uow.runs != 0 {
		t.Error("an invalid request must be refused before verification or a transaction")
	}
}

func TestReplacePhoto_ReplacementAlreadyInThisRoom_IsRefused(t *testing.T) {
	h := newPhotoHarness(t, "asset_1")
	h.repo.seedRoomSlots(h.roomID, 3)

	err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_0", "asset_1")

	if !errors.Is(err, domain.ErrPhotoAssetAlreadyAssigned) {
		t.Fatalf("expected ErrPhotoAssetAlreadyAssigned, got %v", err)
	}
	var assetErr *domain.PhotoAssetError
	if !errors.As(err, &assetErr) || assetErr.AssetID != "asset_1" {
		t.Errorf("the refused asset must be named; got %v", err)
	}
	if h.uow.committed != 0 {
		t.Error("a refusal must roll back, never commit")
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireSameOrder(t, []string{"asset_0", "asset_1", "asset_2"}, orderOf(room))
}

func TestReplacePhoto_ReplacementHangingInAnotherRoom_IsRefused(t *testing.T) {
	h := newPhotoHarness(t, "shared")
	h.repo.seedRoomSlots(h.roomID, 2)
	other, err := h.service.CreateRoom(context.Background(), h.account, "Other", "modern_hall")
	if err != nil {
		t.Fatalf("create other room: %v", err)
	}
	if _, err := h.service.AddPhotos(context.Background(), h.account, other.ID, []string{"shared"}); err != nil {
		t.Fatalf("assign to other room: %v", err)
	}

	err = h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_0", "shared")

	if !errors.Is(err, domain.ErrPhotoAssetAlreadyAssigned) {
		t.Fatalf("expected ErrPhotoAssetAlreadyAssigned, got %v", err)
	}
	if !hasAsset(h.repo, h.roomID, "asset_0") || !hasAsset(h.repo, other.ID, "shared") {
		t.Error("both Rooms must be untouched")
	}
}

func TestReplacePhoto_NonOwner_IsRefusedBeforeAnyIO(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 2)

	err := h.service.ReplacePhoto(context.Background(), "acct_stranger", h.roomID, "asset_0", "new")

	if !errors.Is(err, domain.ErrMuseumNotFound) {
		t.Fatalf("expected the stranger's missing Museum to refuse, got %v", err)
	}
	if len(h.assets.verified) != 0 || h.uow.runs != 0 || h.repo.lockCalls != 0 {
		t.Error("a non-owner must never reach verification, the lock, or a transaction")
	}
	if !hasAsset(h.repo, h.roomID, "asset_0") {
		t.Error("the owner's photograph must be untouched")
	}
}

func TestReplacePhoto_WithoutObjectStorage_Is503Shaped(t *testing.T) {
	service := application.NewMuseumService(newFakeRepo(), newFakeCatalog())

	err := service.ReplacePhoto(context.Background(), "acct", "room", "old", "new")

	if !errors.Is(err, domain.ErrPhotosUnavailable) {
		t.Fatalf("expected ErrPhotosUnavailable, got %v", err)
	}
}

// MARK: - All-or-nothing

func TestReplacePhoto_CommitFailure_RollsBackTheSlotAndReleasesNothing(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 2)
	h.assets.commitErr = errors.New("simulated commit failure")

	err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_0", "new")

	if err == nil {
		t.Fatal("expected the commit failure to surface")
	}
	if h.uow.rolledBack != 1 || h.uow.committed != 0 {
		t.Errorf("expected one rollback and no commit; got rolledBack=%d committed=%d", h.uow.rolledBack, h.uow.committed)
	}
	if !hasAsset(h.repo, h.roomID, "asset_0") || hasAsset(h.repo, h.roomID, "new") {
		t.Error("the slot must still hold the original photograph")
	}
	if len(h.assets.released) != 0 {
		t.Error("the old asset must not be released when the new one failed to commit")
	}
}

func TestReplacePhoto_ReleaseFailure_RollsBackEverything(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 2)
	h.assets.releaseErr = errors.New("simulated release failure")

	err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_0", "new")

	if err == nil {
		t.Fatal("expected the release failure to surface")
	}
	if h.uow.rolledBack != 1 {
		t.Errorf("expected one rollback, got %d", h.uow.rolledBack)
	}
	if !hasAsset(h.repo, h.roomID, "asset_0") {
		t.Error("the slot must be restored to the original photograph")
	}
}

func TestReplacePhoto_RepositoryFailure_RollsBack(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 2)
	h.repo.failReplace = errors.New("simulated write failure")

	err := h.service.ReplacePhoto(context.Background(), h.account, h.roomID, "asset_0", "new")

	if err == nil {
		t.Fatal("expected the failure to surface")
	}
	if h.uow.rolledBack != 1 || len(h.assets.committed) != 0 || len(h.assets.released) != 0 {
		t.Error("a repository failure must roll back before any asset state moves")
	}
}

// MARK: - The other axes are untouched

func TestReplacePhoto_LeavesOrderingUntouched_AndTheNewIdentityIsReorderable(t *testing.T) {
	h := newPhotoHarness(t, "new")
	h.repo.seedRoomSlots(h.roomID, 5)
	ctx := context.Background()

	if err := h.service.ReplacePhoto(ctx, h.account, h.roomID, "asset_3", "new"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	room, _ := h.repo.FindRoom(ctx, h.roomID)
	requireSameOrder(t, []string{"asset_0", "asset_1", "asset_2", "new", "asset_4"}, orderOf(room))
	if h.repo.reorderCalls != 0 {
		t.Error("a replacement must never trigger a reorder")
	}

	if err := h.service.ReorderPhotos(ctx, h.account, h.roomID, []string{"new", "asset_1", "asset_2", "asset_3", "asset_4"}); !errors.Is(err, domain.ErrPhotoOrderMismatch) {
		t.Errorf("an order still naming the replaced photograph is stale and must be refused; got %v", err)
	}
	if err := h.service.ReorderPhotos(ctx, h.account, h.roomID, []string{"new", "asset_1", "asset_2", "asset_0", "asset_4"}); err != nil {
		t.Fatalf("reorder with the new identity: %v", err)
	}
	room, _ = h.repo.FindRoom(ctx, h.roomID)
	requireSameOrder(t, []string{"new", "asset_1", "asset_2", "asset_0", "asset_4"}, orderOf(room))
}
