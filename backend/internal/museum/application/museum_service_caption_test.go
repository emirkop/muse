package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"muse-backend/internal/museum/application"
	"muse-backend/internal/museum/domain"
)

// MARK: - fakeRepo: method

func (f *fakeRepo) UpdatePhotoCaption(_ context.Context, roomID domain.RoomID, photoAssetID string, caption string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCaption != nil {
		return f.failCaption
	}
	room, ok := f.roomsByID[roomID]
	if !ok {
		return domain.ErrRoomNotFound
	}
	for index := range room.PhotoSlots {
		if room.PhotoSlots[index].PhotoAssetID == photoAssetID {
			room.PhotoSlots[index].Caption = caption
			f.roomsByID[roomID] = room
			f.captionWrites++
			return nil
		}
	}
	return domain.ErrPhotoNotInRoom
}

func captionOf(t *testing.T, repo *fakeRepo, roomID domain.RoomID, assetID string) string {
	t.Helper()
	room, err := repo.FindRoom(context.Background(), roomID)
	if err != nil {
		t.Fatalf("find room: %v", err)
	}
	for _, slot := range room.PhotoSlots {
		if slot.PhotoAssetID == assetID {
			return slot.Caption
		}
	}
	t.Fatalf("asset %s not in room", assetID)
	return ""
}

// MARK: - Add / update / clear

func TestSetPhotoCaption_AddsACaption(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 3)
	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_1", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}

	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_1", "Trabzon, 1998"); err != nil {
		t.Fatalf("SetPhotoCaption: %v", err)
	}

	if got := captionOf(t, h.repo, h.roomID, "asset_1"); got != "Trabzon, 1998" {
		t.Errorf("caption = %q, want %q", got, "Trabzon, 1998")
	}
	if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != "caption for asset_0" {
		t.Errorf("a neighbour's caption changed: %q", got)
	}
}

func TestSetPhotoCaption_UpdatesAnExistingCaption(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", "new text"); err != nil {
		t.Fatalf("SetPhotoCaption: %v", err)
	}
	if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != "new text" {
		t.Errorf("caption = %q", got)
	}
}

func TestSetPhotoCaption_ClearingIsTheEmptyString(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != "" {
		t.Errorf("expected the no-caption state, got %q", got)
	}
}

func TestSetPhotoCaption_WhitespaceOnly_IsNoCaption(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", "   \n\t "); err != nil {
		t.Fatalf("SetPhotoCaption: %v", err)
	}
	if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != "" {
		t.Errorf("expected whitespace to normalise to no caption, got %q", got)
	}
}

func TestSetPhotoCaption_TrimsSurroundingWhitespace(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", "  Trabzon  "); err != nil {
		t.Fatalf("SetPhotoCaption: %v", err)
	}
	if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != "Trabzon" {
		t.Errorf("caption = %q, want trimmed", got)
	}
}

func TestSetPhotoCaption_KeepsTextExactlyAsPlainContent(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)
	for _, text := range []string{
		"**not bold**", "<b>not html</b>", "# not a heading",
		"emoji 📷 and ünïcode", "Trabzon — 1998; \"quoted\"", "a\nnewline",
	} {
		if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", text); err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != domain.NormalisedCaption(text) {
			t.Errorf("caption %q was transformed to %q", text, got)
		}
	}
}

// MARK: - Idempotency

func TestSetPhotoCaption_SubmittingTheSameCaption_IsANoOp(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", "same"); err != nil {
		t.Fatalf("first: %v", err)
	}
	writesAfterFirst := h.repo.captionWrites

	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", "same"); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", "  same  "); err != nil {
		t.Fatalf("repeat with whitespace: %v", err)
	}

	if h.repo.captionWrites != writesAfterFirst {
		t.Errorf("an unchanged caption must not write again; writes went %d → %d", writesAfterFirst, h.repo.captionWrites)
	}
}

func TestSetPhotoCaption_ClearingAnAlreadyEmptyCaption_IsANoOp(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)
	_ = h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", "")
	writes := h.repo.captionWrites

	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", ""); err != nil {
		t.Fatalf("clear again: %v", err)
	}
	if h.repo.captionWrites != writes {
		t.Error("clearing an already-empty caption must not write")
	}
}

// MARK: - Ordering and identity are untouched

func TestSetPhotoCaption_LeavesOrderingAndIdentityUntouched(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 5)
	before, _ := h.repo.FindRoom(context.Background(), h.roomID)
	orderBefore := orderOf(before)

	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_2", "middle"); err != nil {
		t.Fatalf("SetPhotoCaption: %v", err)
	}

	after, _ := h.repo.FindRoom(context.Background(), h.roomID)
	requireSameOrder(t, orderBefore, orderOf(after))
	if h.repo.reorderCalls != 0 {
		t.Error("a caption edit must never trigger a reorder")
	}
}

func requireSameOrder(t *testing.T, before, after []string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("count changed: %d → %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("order changed at %d: %v → %v", i, before, after)
		}
	}
}

// MARK: - Rejections

func TestSetPhotoCaption_NonOwner_IsRefusedBeforeAnyLock(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	err := h.service.SetPhotoCaption(context.Background(), "acct_stranger", h.roomID, "asset_0", "hijacked")

	if !errors.Is(err, domain.ErrMuseumNotFound) {
		t.Fatalf("expected the stranger's missing Museum to refuse, got %v", err)
	}
	if h.uow.runs != 0 || h.repo.lockCalls != 0 {
		t.Error("a non-owner must never reach the lock or a transaction")
	}
	if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != "caption for asset_0" {
		t.Errorf("caption changed under a non-owner: %q", got)
	}
}

func TestSetPhotoCaption_ForeignOrUnknownPhoto_IsRefused(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	for name, assetID := range map[string]string{
		"unknown asset":  "not_in_this_room",
		"empty asset id": "",
	} {
		t.Run(name, func(t *testing.T) {
			err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, assetID, "x")
			if !errors.Is(err, domain.ErrPhotoNotInRoom) {
				t.Fatalf("expected ErrPhotoNotInRoom, got %v", err)
			}
		})
	}
	if h.repo.captionWrites != 0 {
		t.Error("a refused target must not write")
	}
}

func TestSetPhotoCaption_TooLong_IsRefusedBeforeAnyIO(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)

	err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0",
		strings.Repeat("x", domain.MaxCaptionBytes+1))

	if !errors.Is(err, domain.ErrCaptionTooLong) {
		t.Fatalf("expected ErrCaptionTooLong, got %v", err)
	}
	if h.uow.runs != 0 {
		t.Error("an over-long caption must be refused before any transaction")
	}
}

func TestSetPhotoCaption_AtTheBound_IsAccepted(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)
	exact := strings.Repeat("x", domain.MaxCaptionBytes)

	if err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", exact); err != nil {
		t.Fatalf("a caption exactly at the bound must be accepted: %v", err)
	}
	if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != exact {
		t.Error("caption at the bound was not stored intact")
	}
}

func TestSetPhotoCaption_RepositoryFailure_RollsBack(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 2)
	h.repo.failCaption = errors.New("simulated write failure")

	err := h.service.SetPhotoCaption(context.Background(), h.account, h.roomID, "asset_0", "nope")

	if err == nil {
		t.Fatal("expected the failure to surface")
	}
	if h.uow.rolledBack != 1 {
		t.Errorf("expected one rollback, got %d", h.uow.rolledBack)
	}
	if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != "caption for asset_0" {
		t.Errorf("caption changed despite rollback: %q", got)
	}
}

func TestSetPhotoCaption_WithoutAUnitOfWork_Is503Shaped(t *testing.T) {
	service := application.NewMuseumService(newFakeRepo(), newFakeCatalog())

	err := service.SetPhotoCaption(context.Background(), "acct", "room", "asset", "x")

	if !errors.Is(err, domain.ErrTransactionsUnavailable) {
		t.Fatalf("expected ErrTransactionsUnavailable, got %v", err)
	}
}

// MARK: - Caption survives reordering (the confirmed acceptance criterion)

func TestCaption_SurvivesReordering_AttachedToItsPhotograph(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 5)
	ctx := context.Background()
	if err := h.service.SetPhotoCaption(ctx, h.account, h.roomID, "asset_0", "the focal one"); err != nil {
		t.Fatalf("caption: %v", err)
	}

	order := []string{"asset_4", "asset_1", "asset_2", "asset_3", "asset_0"}
	if err := h.service.ReorderPhotos(ctx, h.account, h.roomID, order); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	room, _ := h.repo.FindRoom(ctx, h.roomID)
	requireSameOrder(t, order, orderOf(room))
	if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != "the focal one" {
		t.Errorf("the caption did not follow its photograph: %q", got)
	}
	for _, slot := range room.PhotoSlots {
		if slot.PhotoAssetID == "asset_0" {
			continue
		}
		if slot.Caption != "caption for "+slot.PhotoAssetID {
			t.Errorf("caption detached: %+v", slot)
		}
	}
}

func TestSetPhotoCaption_AfterReorder_TargetsThePhotographNotTheSlot(t *testing.T) {
	h := newPhotoHarness(t)
	h.repo.seedRoomSlots(h.roomID, 3)
	ctx := context.Background()
	if err := h.service.ReorderPhotos(ctx, h.account, h.roomID, []string{"asset_2", "asset_1", "asset_0"}); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	if err := h.service.SetPhotoCaption(ctx, h.account, h.roomID, "asset_0", "still mine"); err != nil {
		t.Fatalf("caption: %v", err)
	}

	if got := captionOf(t, h.repo, h.roomID, "asset_0"); got != "still mine" {
		t.Errorf("asset_0 caption = %q", got)
	}
	if got := captionOf(t, h.repo, h.roomID, "asset_2"); got != "caption for asset_2" {
		t.Errorf("the photograph now at slot 0 was wrongly mutated: %q", got)
	}
}
