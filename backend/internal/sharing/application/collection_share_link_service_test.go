package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"muse-backend/internal/sharing/application"
	"muse-backend/internal/sharing/domain"
)

type fakeCollectionLinks struct {
	byCode  map[domain.Code]domain.CollectionShareLink
	lookups int
	ensures int
	rotates int
	revokes int
}

func newFakeCollectionLinks() *fakeCollectionLinks {
	return &fakeCollectionLinks{byCode: map[domain.Code]domain.CollectionShareLink{}}
}

func (f *fakeCollectionLinks) active(roomID string) (domain.CollectionShareLink, bool) {
	for _, l := range f.byCode {
		if l.CollectionRoomID == roomID && l.IsActive() {
			return l, true
		}
	}
	return domain.CollectionShareLink{}, false
}

func (f *fakeCollectionLinks) FindActiveByRoom(_ context.Context, roomID string) (domain.CollectionShareLink, error) {
	if l, ok := f.active(roomID); ok {
		return l, nil
	}
	return domain.CollectionShareLink{}, domain.ErrNoActiveCollectionLink
}

func (f *fakeCollectionLinks) FindByCode(_ context.Context, code domain.Code) (domain.CollectionShareLink, error) {
	f.lookups++
	if l, ok := f.byCode[code]; ok {
		return l, nil
	}
	return domain.CollectionShareLink{}, domain.ErrLinkNotAvailable
}

func (f *fakeCollectionLinks) EnsureActive(_ context.Context, roomID string, code domain.Code, now time.Time) (domain.CollectionShareLink, error) {
	f.ensures++
	if l, ok := f.active(roomID); ok {
		return l, nil
	}
	l := domain.CollectionShareLink{ID: "cl-" + string(code), CollectionRoomID: roomID, Code: code, Status: domain.StatusActive, CreatedAt: now}
	f.byCode[code] = l
	return l, nil
}

func (f *fakeCollectionLinks) Rotate(_ context.Context, roomID string, code domain.Code, now time.Time) (domain.CollectionShareLink, error) {
	f.rotates++
	f.revokeActive(roomID, now)
	l := domain.CollectionShareLink{ID: "cl-" + string(code), CollectionRoomID: roomID, Code: code, Status: domain.StatusActive, CreatedAt: now}
	f.byCode[code] = l
	return l, nil
}

func (f *fakeCollectionLinks) Revoke(_ context.Context, roomID string, now time.Time) (bool, error) {
	f.revokes++
	return f.revokeActive(roomID, now), nil
}

func (f *fakeCollectionLinks) revokeActive(roomID string, now time.Time) bool {
	revoked := false
	for c, l := range f.byCode {
		if l.CollectionRoomID == roomID && l.IsActive() {
			at := now
			l.Status, l.RevokedAt = domain.StatusRevoked, &at
			f.byCode[c] = l
			revoked = true
		}
	}
	return revoked
}

type fakeCollectionRooms struct {
	owners  map[string]string
	content map[string]application.CollectionRoomContent
}

func (f fakeCollectionRooms) OwnedCollectionRoom(_ context.Context, accountID, roomID string) (application.CollectionRoomRef, error) {
	owner, ok := f.owners[roomID]
	if !ok || owner != accountID {
		return application.CollectionRoomRef{}, domain.ErrNoCollectionRoom
	}
	return application.CollectionRoomRef{ID: roomID, OwnerAccountID: owner}, nil
}

func (f fakeCollectionRooms) VisitorCollectionRoom(_ context.Context, roomID string) (application.CollectionRoomContent, error) {
	c, ok := f.content[roomID]
	if !ok {
		return application.CollectionRoomContent{}, application.ErrContentNotVisible
	}
	return c, nil
}

type queuedCodes struct{ queue []domain.Code }

func (q *queuedCodes) NewCode() (domain.Code, error) {
	if len(q.queue) == 0 {
		return "", errors.New("queuedCodes: exhausted")
	}
	c := q.queue[0]
	q.queue = q.queue[1:]
	return c, nil
}

const (
	ownerAcct    = "acct-owner"
	strangerAcct = "acct-stranger"
	roomA        = "room-a"
	roomB        = "room-b"
)

func newCollectionService(t *testing.T, codes ...domain.Code) (*application.CollectionShareLinkService, *fakeCollectionLinks) {
	t.Helper()
	links := newFakeCollectionLinks()
	rooms := fakeCollectionRooms{
		owners: map[string]string{roomA: ownerAcct, roomB: ownerAcct},
		content: map[string]application.CollectionRoomContent{
			roomA: {ID: roomA, Name: "Watches", CategoryID: "category_watches", CurrentTier: 1,
				Items: []application.CollectionItemRef{{ID: "item-1", SlotIndex: 0, CatalogModelID: "model-1"}}},
			roomB: {ID: roomB, Name: "Coins", CategoryID: "category_coins", CurrentTier: 1},
		},
	}
	fixed := func() time.Time { return time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC) }
	return application.NewCollectionShareLinkService(links, &queuedCodes{queue: codes}, rooms, fixed), links
}

func TestCollectionEnsureLink_CreatesOnceThenReturnsTheSame(t *testing.T) {
	svc, links := newCollectionService(t, codeA, codeB)
	ctx := context.Background()

	first, err := svc.EnsureLink(ctx, ownerAcct, roomA)
	if err != nil || first.Code != codeA || first.CollectionRoomID != roomA || !first.IsActive() {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := svc.EnsureLink(ctx, ownerAcct, roomA)
	if err != nil || second.Code != codeA {
		t.Fatalf("second ensure must return the same link: %+v %v", second, err)
	}
	if links.ensures != 2 || len(links.byCode) != 1 {
		t.Fatalf("ensures=%d rows=%d", links.ensures, len(links.byCode))
	}
	current, err := svc.CurrentLink(ctx, ownerAcct, roomA)
	if err != nil || current.Code != codeA {
		t.Fatalf("current: %+v %v", current, err)
	}
}

func TestCollectionOwnerOperations_RefuseARoomTheCallerDoesNotOwn(t *testing.T) {
	svc, links := newCollectionService(t, codeA, codeB, codeC)
	ctx := context.Background()
	if _, err := svc.EnsureLink(ctx, ownerAcct, roomA); err != nil {
		t.Fatal(err)
	}

	for name, roomID := range map[string]string{"foreign room": roomA, "nonexistent room": "room-zzz"} {
		if _, err := svc.EnsureLink(ctx, strangerAcct, roomID); !errors.Is(err, domain.ErrNoCollectionRoom) {
			t.Errorf("%s ensure: %v", name, err)
		}
		if _, err := svc.CurrentLink(ctx, strangerAcct, roomID); !errors.Is(err, domain.ErrNoCollectionRoom) {
			t.Errorf("%s current: %v", name, err)
		}
		if _, err := svc.RegenerateLink(ctx, strangerAcct, roomID); !errors.Is(err, domain.ErrNoCollectionRoom) {
			t.Errorf("%s regenerate: %v", name, err)
		}
		if _, err := svc.RevokeLink(ctx, strangerAcct, roomID); !errors.Is(err, domain.ErrNoCollectionRoom) {
			t.Errorf("%s revoke: %v", name, err)
		}
	}
	if links.ensures != 1 || links.rotates != 0 || links.revokes != 0 {
		t.Fatalf("repository touched by a stranger: ensures=%d rotates=%d revokes=%d", links.ensures, links.rotates, links.revokes)
	}
	if l, _ := links.active(roomA); l.Code != codeA {
		t.Fatalf("owner's link changed: %+v", l)
	}
}

func TestCollectionCurrentLink_WithoutOneIsItsOwnRefusal(t *testing.T) {
	svc, _ := newCollectionService(t)
	if _, err := svc.CurrentLink(context.Background(), ownerAcct, roomA); !errors.Is(err, domain.ErrNoActiveCollectionLink) {
		t.Fatalf("expected ErrNoActiveCollectionLink, got %v", err)
	}
}

func TestCollectionRegenerateLink_RevokesTheOldAndIssuesANew(t *testing.T) {
	svc, links := newCollectionService(t, codeA, codeB)
	ctx := context.Background()
	if _, err := svc.EnsureLink(ctx, ownerAcct, roomA); err != nil {
		t.Fatal(err)
	}
	renewed, err := svc.RegenerateLink(ctx, ownerAcct, roomA)
	if err != nil || renewed.Code != codeB || !renewed.IsActive() {
		t.Fatalf("renewed: %+v %v", renewed, err)
	}
	if old := links.byCode[codeA]; old.IsActive() || old.RevokedAt == nil {
		t.Fatalf("old link must be revoked with a timestamp: %+v", old)
	}
	if _, err := svc.VisitorCollectionRoom(ctx, codeA); !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("old code must be refused: %v", err)
	}
	if room, err := svc.VisitorCollectionRoom(ctx, codeB); err != nil || room.ID != roomA {
		t.Fatalf("new code: %+v %v", room, err)
	}
}

func TestCollectionRevokeLink_ClosesTheLinkAndReportsWhetherOneWasActive(t *testing.T) {
	svc, _ := newCollectionService(t, codeA, codeB)
	ctx := context.Background()
	if _, err := svc.EnsureLink(ctx, ownerAcct, roomA); err != nil {
		t.Fatal(err)
	}
	revoked, err := svc.RevokeLink(ctx, ownerAcct, roomA)
	if err != nil || !revoked {
		t.Fatalf("first revoke: %v %v", revoked, err)
	}
	revoked, err = svc.RevokeLink(ctx, ownerAcct, roomA)
	if err != nil || revoked {
		t.Fatalf("second revoke must be a reported no-op: %v %v", revoked, err)
	}
	if _, err := svc.VisitorCollectionRoom(ctx, codeA); !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("revoked code must be refused: %v", err)
	}
	if _, err := svc.CurrentLink(ctx, ownerAcct, roomA); !errors.Is(err, domain.ErrNoActiveCollectionLink) {
		t.Fatalf("current after revoke: %v", err)
	}
	fresh, err := svc.EnsureLink(ctx, ownerAcct, roomA)
	if err != nil || fresh.Code != codeB {
		t.Fatalf("fresh: %+v %v", fresh, err)
	}
}

func TestCollectionVisitor_ImplausibleCodeIsRefusedWithoutALookup(t *testing.T) {
	svc, links := newCollectionService(t)
	for _, code := range []domain.Code{"", "short", "has spaces in it 123456", "ZZZZZZZZZZZZZZZZZZZZZZZ", "room-a"} {
		if _, err := svc.VisitorCollectionRoom(context.Background(), code); !errors.Is(err, domain.ErrLinkNotAvailable) {
			t.Errorf("%q: %v", code, err)
		}
	}
	if links.lookups != 0 {
		t.Fatalf("implausible codes must never reach the repository; lookups=%d", links.lookups)
	}
}

func TestCollectionVisitor_OneRefusalForUnknownRevokedAndGone(t *testing.T) {
	svc, links := newCollectionService(t, codeA, codeB)
	ctx := context.Background()
	if _, err := svc.EnsureLink(ctx, ownerAcct, roomA); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RevokeLink(ctx, ownerAcct, roomA); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnsureLink(ctx, ownerAcct, roomB); err != nil {
		t.Fatal(err)
	}
	gone := links.byCode[codeB]
	gone.CollectionRoomID = "room-deleted"
	links.byCode[codeB] = gone

	for name, code := range map[string]domain.Code{
		"unknown": codeC, "revoked": codeA, "room gone": codeB,
	} {
		if _, err := svc.VisitorCollectionRoom(ctx, code); !errors.Is(err, domain.ErrLinkNotAvailable) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCollectionVisitor_ALinkResolvesItsOwnRoomOnly(t *testing.T) {
	svc, _ := newCollectionService(t, codeA, codeB)
	ctx := context.Background()
	if _, err := svc.EnsureLink(ctx, ownerAcct, roomA); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnsureLink(ctx, ownerAcct, roomB); err != nil {
		t.Fatal(err)
	}
	a, err := svc.VisitorCollectionRoom(ctx, codeA)
	if err != nil || a.ID != roomA || a.Name != "Watches" || len(a.Items) != 1 || a.Items[0].CatalogModelID != "model-1" {
		t.Fatalf("A: %+v %v", a, err)
	}
	b, err := svc.VisitorCollectionRoom(ctx, codeB)
	if err != nil || b.ID != roomB || b.Name != "Coins" || len(b.Items) != 0 {
		t.Fatalf("B: %+v %v", b, err)
	}
	if _, err := svc.RevokeLink(ctx, ownerAcct, roomA); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VisitorCollectionRoom(ctx, codeA); !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("A after revoke: %v", err)
	}
	if b, err := svc.VisitorCollectionRoom(ctx, codeB); err != nil || b.ID != roomB {
		t.Fatalf("B after revoking A: %+v %v", b, err)
	}
}

func TestCollectionVisitor_MusicIsGatedByTheSharedPolicy(t *testing.T) {
	links := newFakeCollectionLinks()
	rooms := fakeCollectionRooms{
		owners: map[string]string{roomA: ownerAcct},
		content: map[string]application.CollectionRoomContent{
			roomA: {ID: roomA, Name: "Watches", CategoryID: "category_watches", CurrentTier: 1, MusicTrackID: "track_dev_a"},
		},
	}
	ctx := context.Background()

	gated := application.NewCollectionShareLinkService(links, &queuedCodes{queue: []domain.Code{codeA}}, rooms, nil)
	if _, err := gated.EnsureLink(ctx, ownerAcct, roomA); err != nil {
		t.Fatal(err)
	}
	seen, err := gated.VisitorCollectionRoom(ctx, codeA)
	if err != nil {
		t.Fatal(err)
	}
	if seen.MusicTrackID != "" {
		t.Fatalf("visitor must not be told about music while clearance is unresolved, got %q", seen.MusicTrackID)
	}
	if seen.Name != "Watches" || seen.ID != roomA {
		t.Fatalf("the rest of the Room must be intact: %+v", seen)
	}

	cleared := application.NewCollectionShareLinkService(links, &queuedCodes{}, rooms, nil).
		WithVisitorMusicPolicy(application.VisitorMusicPolicy{AudibleToVisitors: true})
	seen, err = cleared.VisitorCollectionRoom(ctx, codeA)
	if err != nil {
		t.Fatal(err)
	}
	if seen.MusicTrackID != "track_dev_a" {
		t.Fatalf("with clearance the visitor is told the track, got %q", seen.MusicTrackID)
	}
	if rooms.content[roomA].MusicTrackID != "track_dev_a" {
		t.Fatal("the gate must not mutate the source content")
	}
}
