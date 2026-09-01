package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"muse-backend/internal/museum/application"
	"muse-backend/internal/museum/domain"
)

// MARK: - fakeRepo: methods

func (f *fakeRepo) LockRoomForUpdate(ctx context.Context, id domain.RoomID) (domain.Room, error) {
	f.mu.Lock()
	f.lockCalls++
	f.mu.Unlock()
	return f.FindRoom(ctx, id)
}

func (f *fakeRepo) InsertPhotoSlots(_ context.Context, roomID domain.RoomID, slots []domain.PhotoSlotAssignment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failInsert != nil {
		return f.failInsert
	}
	room, ok := f.roomsByID[roomID]
	if !ok {
		return domain.ErrRoomNotFound
	}
	for _, slot := range slots {
		for _, existing := range f.allSlots() {
			if existing.PhotoAssetID == slot.PhotoAssetID {
				return &domain.PhotoAssetError{AssetID: slot.PhotoAssetID, Err: domain.ErrPhotoAssetAlreadyAssigned}
			}
		}
		room.PhotoSlots = append(room.PhotoSlots, slot)
	}
	f.roomsByID[roomID] = room
	f.roomWrites++
	return nil
}

func (f *fakeRepo) FindPhotoSlotRoomsByAssetIDs(_ context.Context, assetIDs []string) (map[string]domain.RoomID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]domain.RoomID{}
	for roomID, room := range f.roomsByID {
		for _, slot := range room.PhotoSlots {
			for _, id := range assetIDs {
				if slot.PhotoAssetID == id {
					out[id] = roomID
				}
			}
		}
	}
	return out, nil
}

func (f *fakeRepo) allSlots() []domain.PhotoSlotAssignment {
	var all []domain.PhotoSlotAssignment
	for _, room := range f.roomsByID {
		all = append(all, room.PhotoSlots...)
	}
	return all
}

func (f *fakeRepo) setRoomSlots(roomID domain.RoomID, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	room := f.roomsByID[roomID]
	room.PhotoSlots = nil
	for i := 0; i < n; i++ {
		room.PhotoSlots = append(room.PhotoSlots, domain.PhotoSlotAssignment{
			SlotIndex: i, PhotoAssetID: fmt.Sprintf("existing_%d", i),
		})
	}
	f.roomsByID[roomID] = room
}

// MARK: - Fakes for the photo ports

type fakeUnitOfWork struct {
	repo       *fakeRepo
	runs       int
	committed  int
	rolledBack int
}

func (u *fakeUnitOfWork) Run(ctx context.Context, fn func(ctx context.Context) error) error {
	u.runs++
	snapshot := u.repo.snapshot()
	if err := fn(ctx); err != nil {
		u.repo.restore(snapshot)
		u.rolledBack++
		return err
	}
	u.committed++
	return nil
}

func (f *fakeRepo) snapshot() map[domain.RoomID]domain.Room {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := make(map[domain.RoomID]domain.Room, len(f.roomsByID))
	for id, room := range f.roomsByID {
		room.PhotoSlots = append([]domain.PhotoSlotAssignment(nil), room.PhotoSlots...)
		copied[id] = room
	}
	return copied
}

func (f *fakeRepo) restore(snapshot map[domain.RoomID]domain.Room) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.roomsByID = snapshot
}

type fakeAssets struct {
	owned       map[string]bool
	verifyErr   error
	commitErr   error
	verified    [][]string
	committed   [][]string
	verifyOrder []string
	released    [][]string
	releaseErr  error
}

func (a *fakeAssets) VerifyPhotoAssets(_ context.Context, _ string, ids []string) error {
	a.verified = append(a.verified, ids)
	if a.verifyErr != nil {
		return a.verifyErr
	}
	for _, id := range ids {
		if !a.owned[id] {
			return &domain.PhotoAssetError{AssetID: id, Err: domain.ErrPhotoAssetNotFound}
		}
	}
	return nil
}

func (a *fakeAssets) CommitPhotoAssets(_ context.Context, ids []string) error {
	a.committed = append(a.committed, ids)
	return a.commitErr
}

func (a *fakeAssets) ReleasePhotoAssets(_ context.Context, ids []string) error {
	a.released = append(a.released, ids)
	return a.releaseErr
}

type fakeDelivery struct {
	requested []string
}

func (d *fakeDelivery) IssuePhotoDownloadTickets(_ context.Context, _ string, ids []string) ([]application.PhotoDownloadTicket, error) {
	d.requested = ids
	out := make([]application.PhotoDownloadTicket, 0, len(ids))
	for _, id := range ids {
		out = append(out, application.PhotoDownloadTicket{
			PhotoAssetID: id, URL: "https://signed.example/" + id, ExpiresAt: time.Now().Add(time.Minute),
			PixelWidth: 3072, PixelHeight: 2048,
		})
	}
	return out, nil
}

// MARK: - Harness

type photoHarness struct {
	service *application.MuseumService
	repo    *fakeRepo
	uow     *fakeUnitOfWork
	assets  *fakeAssets
	deliver *fakeDelivery
	catalog *fakeCatalog
	account string
	roomID  domain.RoomID
}

func newPhotoHarness(t *testing.T, ownedAssets ...string) *photoHarness {
	t.Helper()
	repo := newFakeRepo()
	uow := &fakeUnitOfWork{repo: repo}
	assets := &fakeAssets{owned: map[string]bool{}}
	for _, id := range ownedAssets {
		assets.owned[id] = true
	}
	deliver := &fakeDelivery{}
	catalog := newFakeCatalog()

	service := application.NewMuseumService(repo, catalog).EnablePhotos(uow, assets, deliver)

	ctx := context.Background()
	const account = "acct_owner"
	if _, err := service.CreateMuseum(ctx, account, "style_modern"); err != nil {
		t.Fatalf("create museum: %v", err)
	}
	room, err := service.CreateRoom(ctx, account, "Trabzon", "modern_hall")
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	return &photoHarness{service: service, repo: repo, uow: uow, assets: assets, deliver: deliver, catalog: catalog, account: account, roomID: room.ID}
}

func ids(prefix string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("%s_%d", prefix, i))
	}
	return out
}

// MARK: - Tests

func TestAddPhotos_AppendsContiguouslyInRequestOrder(t *testing.T) {
	h := newPhotoHarness(t, "a", "b", "c")
	h.repo.setRoomSlots(h.roomID, 2)

	slots, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, []string{"c", "a", "b"})
	if err != nil {
		t.Fatalf("AddPhotos: %v", err)
	}

	want := []struct {
		asset string
		index int
	}{{"c", 2}, {"a", 3}, {"b", 4}}
	if len(slots) != len(want) {
		t.Fatalf("got %d slots, want %d", len(slots), len(want))
	}
	for i, w := range want {
		if slots[i].PhotoAssetID != w.asset || slots[i].SlotIndex != w.index {
			t.Errorf("slot %d = (%s, %d), want (%s, %d)", i, slots[i].PhotoAssetID, slots[i].SlotIndex, w.asset, w.index)
		}
	}
	if h.uow.committed != 1 {
		t.Errorf("expected exactly one committed unit of work, got %d", h.uow.committed)
	}
	if len(h.assets.committed) != 1 || len(h.assets.committed[0]) != 3 {
		t.Errorf("expected the three new assets committed, got %v", h.assets.committed)
	}
}

func TestAddPhotos_VerifiesBeforeOpeningTheTransaction(t *testing.T) {
	h := newPhotoHarness(t)
	h.assets.verifyErr = &domain.PhotoAssetError{AssetID: "x", Err: domain.ErrPhotoAssetNotUploaded}

	_, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, []string{"x"})

	if !errors.Is(err, domain.ErrPhotoAssetNotUploaded) {
		t.Fatalf("expected ErrPhotoAssetNotUploaded, got %v", err)
	}
	if len(h.assets.verified) != 1 {
		t.Errorf("expected verification to run once, got %d", len(h.assets.verified))
	}
	if h.uow.runs != 0 {
		t.Errorf("a failed verification must not open a transaction; uow ran %d times", h.uow.runs)
	}
	if h.repo.lockCalls != 0 {
		t.Errorf("no Room lock may be taken before verification passes; got %d", h.repo.lockCalls)
	}
}

func TestAddPhotos_RefusesToExceedTheCap_AndRollsBack(t *testing.T) {
	h := newPhotoHarness(t, ids("new", 3)...)
	h.repo.setRoomSlots(h.roomID, domain.MaxPhotosPerRoom-2)

	_, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, ids("new", 3))

	if !errors.Is(err, domain.ErrPhotoCapacityReached) {
		t.Fatalf("expected ErrPhotoCapacityReached, got %v", err)
	}
	if h.uow.rolledBack != 1 || h.uow.committed != 0 {
		t.Errorf("expected rollback without commit, got committed=%d rolledBack=%d", h.uow.committed, h.uow.rolledBack)
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	if len(room.PhotoSlots) != domain.MaxPhotosPerRoom-2 {
		t.Errorf("a refused batch must leave the Room untouched; got %d slots", len(room.PhotoSlots))
	}
	if len(h.assets.committed) != 0 {
		t.Errorf("no asset may commit when the batch is refused; got %v", h.assets.committed)
	}
}

func TestAddPhotos_FillingExactlyToTheCap_IsAllowed(t *testing.T) {
	h := newPhotoHarness(t, ids("new", 2)...)
	h.repo.setRoomSlots(h.roomID, domain.MaxPhotosPerRoom-2)

	slots, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, ids("new", 2))
	if err != nil {
		t.Fatalf("AddPhotos: %v", err)
	}
	if slots[1].SlotIndex != domain.MaxPhotosPerRoom-1 {
		t.Errorf("last slot index = %d, want %d", slots[1].SlotIndex, domain.MaxPhotosPerRoom-1)
	}
}

func TestAddPhotos_MoreThanTheCapInOneRequest_IsRefusedBeforeAnyIO(t *testing.T) {
	h := newPhotoHarness(t)

	_, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, ids("new", domain.MaxPhotosPerRoom+1))

	if !errors.Is(err, domain.ErrPhotoCapacityReached) {
		t.Fatalf("expected ErrPhotoCapacityReached, got %v", err)
	}
	if len(h.assets.verified) != 0 {
		t.Error("an obviously over-cap request must not reach storage verification")
	}
}

func TestAddPhotos_RetryingTheSameBatch_IsIdempotent(t *testing.T) {
	h := newPhotoHarness(t, "a", "b")

	first, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, []string{"a", "b"})
	if err != nil {
		t.Fatalf("first AddPhotos: %v", err)
	}
	second, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, []string{"a", "b"})
	if err != nil {
		t.Fatalf("retried AddPhotos: %v", err)
	}

	for i := range first {
		if first[i].SlotIndex != second[i].SlotIndex || first[i].PhotoAssetID != second[i].PhotoAssetID {
			t.Errorf("retry diverged at %d: %+v vs %+v", i, first[i], second[i])
		}
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	if len(room.PhotoSlots) != 2 {
		t.Errorf("retry must not duplicate slots; got %d", len(room.PhotoSlots))
	}
	if len(h.assets.committed) != 1 {
		t.Errorf("already-committed assets must not be committed again; got %v", h.assets.committed)
	}
}

func TestAddPhotos_RecomposedBatch_AddsOnlyTheNewAssets(t *testing.T) {
	h := newPhotoHarness(t, "a", "b", "c")
	if _, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, []string{"a", "b"}); err != nil {
		t.Fatalf("first AddPhotos: %v", err)
	}

	slots, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, []string{"a", "c"})
	if err != nil {
		t.Fatalf("recomposed AddPhotos: %v", err)
	}

	if slots[0].PhotoAssetID != "a" || slots[0].SlotIndex != 0 {
		t.Errorf("existing asset must keep its slot: %+v", slots[0])
	}
	if slots[1].PhotoAssetID != "c" || slots[1].SlotIndex != 2 {
		t.Errorf("new asset must take the next position (2): %+v", slots[1])
	}
	if len(h.assets.committed) != 2 || len(h.assets.committed[1]) != 1 || h.assets.committed[1][0] != "c" {
		t.Errorf("only the new asset may commit on the second call; got %v", h.assets.committed)
	}
}

func TestAddPhotos_AssetHangingInAnotherRoom_IsRefused(t *testing.T) {
	h := newPhotoHarness(t, "shared")
	other, err := h.service.CreateRoom(context.Background(), h.account, "Other", "modern_hall")
	if err != nil {
		t.Fatalf("create other room: %v", err)
	}
	if _, err := h.service.AddPhotos(context.Background(), h.account, other.ID, []string{"shared"}); err != nil {
		t.Fatalf("assign to other room: %v", err)
	}

	_, err = h.service.AddPhotos(context.Background(), h.account, h.roomID, []string{"shared"})

	if !errors.Is(err, domain.ErrPhotoAssetAlreadyAssigned) {
		t.Fatalf("expected ErrPhotoAssetAlreadyAssigned, got %v", err)
	}
	var assetErr *domain.PhotoAssetError
	if !errors.As(err, &assetErr) || assetErr.AssetID != "shared" {
		t.Errorf("the refused asset must be identified; got %v", err)
	}
}

func TestAddPhotos_CommitFailure_RollsBackSlotInserts(t *testing.T) {
	h := newPhotoHarness(t, "a", "b")
	h.assets.commitErr = errors.New("simulated commit failure")

	_, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, []string{"a", "b"})

	if err == nil {
		t.Fatal("expected the commit failure to surface")
	}
	if h.uow.rolledBack != 1 {
		t.Errorf("expected exactly one rollback, got %d", h.uow.rolledBack)
	}
	room, _ := h.repo.FindRoom(context.Background(), h.roomID)
	if len(room.PhotoSlots) != 0 {
		t.Errorf("slot inserts must roll back with the failed commit; got %d slots", len(room.PhotoSlots))
	}
}

func TestAddPhotos_NonOwner_IsRefusedBeforeAnyIO(t *testing.T) {
	h := newPhotoHarness(t, "a")

	_, err := h.service.AddPhotos(context.Background(), "acct_stranger", h.roomID, []string{"a"})

	if !errors.Is(err, domain.ErrMuseumNotFound) {
		t.Fatalf("expected ErrMuseumNotFound, got %v", err)
	}
	if len(h.assets.verified) != 0 || h.uow.runs != 0 {
		t.Error("a non-owner must never reach verification or a transaction")
	}
}

func TestAddPhotos_RequestValidation(t *testing.T) {
	h := newPhotoHarness(t, "a")
	ctx := context.Background()

	if _, err := h.service.AddPhotos(ctx, h.account, h.roomID, nil); !errors.Is(err, domain.ErrNoPhotosSupplied) {
		t.Errorf("empty: expected ErrNoPhotosSupplied, got %v", err)
	}
	if _, err := h.service.AddPhotos(ctx, h.account, h.roomID, []string{"a", "a"}); !errors.Is(err, domain.ErrDuplicatePhotoAssetIDs) {
		t.Errorf("duplicates: expected ErrDuplicatePhotoAssetIDs, got %v", err)
	}
}

func TestAddPhotos_WithoutObjectStorage_Is503Shaped(t *testing.T) {
	repo := newFakeRepo()
	service := application.NewMuseumService(repo, newFakeCatalog())

	_, err := service.AddPhotos(context.Background(), "acct", "room", []string{"a"})

	if !errors.Is(err, domain.ErrPhotosUnavailable) {
		t.Fatalf("expected ErrPhotosUnavailable, got %v", err)
	}
}

// MARK: - Delivery tickets (the seam)

func TestPhotoDownloadTickets_CoverEveryAssignedPhoto_InSlotOrder(t *testing.T) {
	h := newPhotoHarness(t, "a", "b", "c")
	if _, err := h.service.AddPhotos(context.Background(), h.account, h.roomID, []string{"a", "b", "c"}); err != nil {
		t.Fatalf("AddPhotos: %v", err)
	}

	tickets, err := h.service.PhotoDownloadTickets(context.Background(), h.account, h.roomID)
	if err != nil {
		t.Fatalf("PhotoDownloadTickets: %v", err)
	}

	if len(tickets) != 3 {
		t.Fatalf("got %d tickets, want 3", len(tickets))
	}
	for i, want := range []string{"a", "b", "c"} {
		if tickets[i].PhotoAssetID != want {
			t.Errorf("ticket %d = %s, want %s", i, tickets[i].PhotoAssetID, want)
		}
		if tickets[i].URL == "" || tickets[i].ExpiresAt.IsZero() {
			t.Errorf("ticket %d must carry a URL and expiry", i)
		}
	}
}

func TestPhotoDownloadTickets_EmptyRoom_IsEmptyNotError(t *testing.T) {
	h := newPhotoHarness(t)

	tickets, err := h.service.PhotoDownloadTickets(context.Background(), h.account, h.roomID)
	if err != nil {
		t.Fatalf("PhotoDownloadTickets: %v", err)
	}
	if len(tickets) != 0 || len(h.deliver.requested) != 0 {
		t.Error("an empty Room must produce no tickets and no delivery call")
	}
}

func TestPhotoDownloadTickets_NonOwner_IsRefused(t *testing.T) {
	h := newPhotoHarness(t)

	_, err := h.service.PhotoDownloadTickets(context.Background(), "acct_stranger", h.roomID)

	if err == nil {
		t.Fatal("a non-owner must not receive download tickets")
	}
	if len(h.deliver.requested) != 0 {
		t.Error("delivery must not be consulted for a non-owner")
	}
}
