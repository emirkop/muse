package application_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/collection/application"
	"muse-backend/internal/collection/domain"
)

type stubCapacity struct {
	allow bool
	err   error
	asked []string
}

func (s *stubCapacity) MayAddCollectionItem(_ context.Context, accountID, collectionRoomID string) (bool, error) {
	s.asked = append(s.asked, accountID+"|"+collectionRoomID)
	if s.err != nil {
		return false, s.err
	}
	return s.allow, nil
}

type allowAll struct{}

func (allowAll) MayAddCollectionItem(context.Context, string, string) (bool, error) { return true, nil }

var _ application.ItemCapacityAuthority = allowAll{}

func TestAddItem_WithoutAnAuthority_NoCeilingApplies(t *testing.T) {
	ctx := context.Background()

	unwired, _, _, _, _, _ := newServiceWithItems()
	roomA := roomWithDesign(t, unwired, "design_universal")
	if _, err := unwired.AddItem(ctx, "owner", roomA.ID, "model-watch", 1); err != nil {
		t.Fatalf("no authority wired: %v", err)
	}

	permissive, _, _, _, _, _ := newServiceWithItems()
	permissive = permissive.WithEntitlements(allowAll{})
	roomB := roomWithDesign(t, permissive, "design_universal")
	if _, err := permissive.AddItem(ctx, "owner", roomB.ID, "model-watch", 1); err != nil {
		t.Fatalf("permissive authority wired: %v", err)
	}
}

func TestAddItem_AuthoritativeCheckRunsUnderTheAccountLock(t *testing.T) {
	service, repo, _, _, _, uow := newServiceWithItems()
	capacity := &stubCapacity{allow: true}
	service = service.WithEntitlements(capacity)
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	if _, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1); err != nil {
		t.Fatal(err)
	}
	if len(capacity.asked) != 2 {
		t.Fatalf("expected the authority to be asked twice (advisory + authoritative), got %v", capacity.asked)
	}
	if len(repo.accountLocks) != 1 || repo.accountLocks[0] != "owner" {
		t.Fatalf("the account lock must be taken once, for the owner: %v", repo.accountLocks)
	}
	if len(repo.locks) != 1 {
		t.Fatalf("the Room lock must be taken once: %v", repo.locks)
	}
	if uow.runs != 1 {
		t.Fatalf("one transaction: %d", uow.runs)
	}
}

func TestAddItem_ARefusedAccountAddsNothing(t *testing.T) {
	service, repo, _, _, models, _ := newServiceWithItems()
	capacity := &stubCapacity{allow: false}
	service = service.WithEntitlements(capacity)
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	before := len(repo.inserts)

	_, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1)

	if !errors.Is(err, domain.ErrItemCapacityReached) {
		t.Fatalf("expected ErrItemCapacityReached, got %v", err)
	}
	if len(repo.inserts) != before {
		t.Fatalf("a refused add must write nothing: %v", repo.inserts)
	}
	if errors.Is(err, domain.ErrTierCapacityReached) {
		t.Fatal("the entitlement ceiling must not be reported as a tier ceiling")
	}
	if models.calls != 0 {
		t.Fatalf("the catalog must not be probed for a refused add; asked %d times", models.calls)
	}
}

func TestAddItem_TheAuthorityIsAskedWithBothIdentifiers(t *testing.T) {
	service, _, _, _, _, _ := newServiceWithItems()
	capacity := &stubCapacity{allow: true}
	service = service.WithEntitlements(capacity)
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	if _, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1); err != nil {
		t.Fatal(err)
	}

	want := "owner|" + string(room.ID)
	for _, asked := range capacity.asked {
		if asked != want {
			t.Fatalf("asked %v, want only %s", capacity.asked, want)
		}
	}
	if len(capacity.asked) == 0 {
		t.Fatal("the authority was never asked")
	}
}

func TestAddItem_OwnershipIsCheckedBeforeEntitlement(t *testing.T) {
	service, _, _, _, _, _ := newServiceWithItems()
	capacity := &stubCapacity{allow: true}
	service = service.WithEntitlements(capacity)
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	if _, err := service.AddItem(ctx, "stranger", room.ID, "model-watch", 1); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("stranger: %v", err)
	}
	if len(capacity.asked) != 0 {
		t.Fatalf("the authority must not be consulted for a Room the caller does not own: %v", capacity.asked)
	}
}

func TestAddItem_AnUnansweredDecisionFailsClosed(t *testing.T) {
	service, repo, _, _, _, _ := newServiceWithItems()
	service = service.WithEntitlements(&stubCapacity{err: errors.New("entitlement store unreachable")})
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	_, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1)

	if err == nil {
		t.Fatal("an unanswerable capacity decision must not permit the add")
	}
	if errors.Is(err, domain.ErrItemCapacityReached) {
		t.Fatal("a transport failure is not a capacity refusal — the two must stay distinguishable")
	}
	if len(repo.inserts) != 0 {
		t.Fatalf("nothing may be written: %v", repo.inserts)
	}
}

func TestPlaceItemAtSlot_NeverConsultsEntitlement(t *testing.T) {
	service, repo, _, _, _, _ := newServiceWithItems()
	capacity := &stubCapacity{allow: false}
	ctx := context.Background()
	room := roomWithDesign(t, service.WithEntitlements(allowAll{}), "design_universal")
	first, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1)
	if err != nil {
		t.Fatal(err)
	}
	service = service.WithEntitlements(capacity)
	itemID := first.Items[0].ID

	if _, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, itemID, 2, 1); err != nil {
		t.Fatalf("a refused account must still be able to rearrange its own items: %v", err)
	}
	if len(capacity.asked) != 0 {
		t.Fatalf("a drop must not consult the entitlement authority: %v", capacity.asked)
	}
	if len(repo.moves) != 1 {
		t.Fatalf("moves: %v", repo.moves)
	}
}
