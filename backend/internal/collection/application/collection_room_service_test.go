package application_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"muse-backend/internal/collection/application"
	"muse-backend/internal/collection/domain"
)

type fakeRepo struct {
	rooms   map[domain.CollectionRoomID]domain.CollectionRoom
	next    int
	updates []domain.CollectionRoomPatch
	failOn  map[string]error

	nextItem int
	locks    []domain.CollectionRoomID
	inserts  []int
	moves    []string
	swaps    []string

	musicSets []*string

	accountLocks []string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		rooms:  map[domain.CollectionRoomID]domain.CollectionRoom{},
		failOn: map[string]error{},
	}
}

func (f *fakeRepo) Create(_ context.Context, room domain.CollectionRoom) (domain.CollectionRoom, error) {
	if err := f.failOn["Create"]; err != nil {
		return domain.CollectionRoom{}, err
	}
	f.next++
	room.ID = domain.CollectionRoomID(fmt.Sprintf("room-%d", f.next))
	room.CreatedAt = time.Unix(int64(f.next), 0)
	room.UpdatedAt = room.CreatedAt
	room.Items = []domain.CollectionItem{}
	f.rooms[room.ID] = room
	return room, nil
}

func (f *fakeRepo) ListForAccount(_ context.Context, accountID string) ([]domain.CollectionRoom, error) {
	if err := f.failOn["ListForAccount"]; err != nil {
		return nil, err
	}
	out := []domain.CollectionRoom{}
	for index := 1; index <= f.next; index++ {
		room, ok := f.rooms[domain.CollectionRoomID(fmt.Sprintf("room-%d", index))]
		if ok && room.AccountID == accountID {
			out = append(out, room)
		}
	}
	return out, nil
}

func (f *fakeRepo) Find(_ context.Context, id domain.CollectionRoomID) (domain.CollectionRoom, error) {
	room, ok := f.rooms[id]
	if !ok {
		return domain.CollectionRoom{}, domain.ErrCollectionRoomNotFound
	}
	return room, nil
}

func (f *fakeRepo) Update(_ context.Context, id domain.CollectionRoomID, patch domain.CollectionRoomPatch) error {
	room, ok := f.rooms[id]
	if !ok {
		return domain.ErrCollectionRoomNotFound
	}
	f.updates = append(f.updates, patch)
	if patch.Name != nil {
		room.Name = *patch.Name
	}
	if patch.CategoryID != nil {
		room.CategoryID = *patch.CategoryID
	}
	if patch.DesignID != nil {
		room.DesignID = *patch.DesignID
	}
	f.rooms[id] = room
	return nil
}

func (f *fakeRepo) LockAccountItems(_ context.Context, accountID string) error {
	if err := f.failOn["LockAccountItems"]; err != nil {
		return err
	}
	f.accountLocks = append(f.accountLocks, accountID)
	return nil
}

func (f *fakeRepo) CountItemsForAccount(_ context.Context, accountID string) (int, error) {
	count := 0
	for _, room := range f.rooms {
		if room.AccountID == accountID {
			count += len(room.Items)
		}
	}
	return count, nil
}

func (f *fakeRepo) SetMusic(_ context.Context, id domain.CollectionRoomID, trackID *string) error {
	if err := f.failOn["SetMusic"]; err != nil {
		return err
	}
	room, ok := f.rooms[id]
	if !ok {
		return domain.ErrCollectionRoomNotFound
	}
	if trackID == nil {
		room.MusicTrackID = ""
	} else {
		room.MusicTrackID = *trackID
	}
	f.rooms[id] = room
	f.musicSets = append(f.musicSets, trackID)
	return nil
}

func (f *fakeRepo) RatchetTier(_ context.Context, id domain.CollectionRoomID, tier domain.Tier) (bool, error) {
	room, ok := f.rooms[id]
	if !ok {
		return false, domain.ErrCollectionRoomNotFound
	}
	raised := tier > room.CurrentTier
	room.CurrentTier = domain.RatchetedTier(room.CurrentTier, tier)
	f.rooms[id] = room
	return raised, nil
}

func (f *fakeRepo) Delete(_ context.Context, id domain.CollectionRoomID) error {
	if _, ok := f.rooms[id]; !ok {
		return domain.ErrCollectionRoomNotFound
	}
	delete(f.rooms, id)
	return nil
}

// MARK: -: the item write surface

func (f *fakeRepo) LockRoomForUpdate(
	_ context.Context, id domain.CollectionRoomID,
) (domain.CollectionRoom, error) {
	f.locks = append(f.locks, id)
	if err := f.failOn["LockRoomForUpdate"]; err != nil {
		return domain.CollectionRoom{}, err
	}
	room, ok := f.rooms[id]
	if !ok {
		return domain.CollectionRoom{}, domain.ErrCollectionRoomNotFound
	}
	return room, nil
}

func (f *fakeRepo) InsertItem(
	_ context.Context,
	roomID domain.CollectionRoomID,
	slotIndex int,
	catalogModelID string,
) (domain.CollectionItem, error) {
	f.inserts = append(f.inserts, slotIndex)
	if err := f.failOn["InsertItem"]; err != nil {
		return domain.CollectionItem{}, err
	}
	room, ok := f.rooms[roomID]
	if !ok {
		return domain.CollectionItem{}, domain.ErrCollectionRoomNotFound
	}
	if _, taken := domain.ItemAtSlot(room.Items, slotIndex); taken {
		return domain.CollectionItem{}, domain.ErrItemSlotTaken
	}
	f.nextItem++
	item := domain.CollectionItem{
		ID:             domain.CollectionItemID(fmt.Sprintf("item-%d", f.nextItem)),
		SlotIndex:      slotIndex,
		CatalogModelID: catalogModelID,
	}
	room.Items = append(room.Items, item)
	sortItemsBySlot(room.Items)
	f.rooms[roomID] = room
	return item, nil
}

func (f *fakeRepo) MoveItemToSlot(
	_ context.Context,
	roomID domain.CollectionRoomID,
	itemID domain.CollectionItemID,
	slotIndex int,
) error {
	f.moves = append(f.moves, fmt.Sprintf("%s→%d", itemID, slotIndex))
	if err := f.failOn["MoveItemToSlot"]; err != nil {
		return err
	}
	room, ok := f.rooms[roomID]
	if !ok {
		return domain.ErrCollectionRoomNotFound
	}
	if _, taken := domain.ItemAtSlot(room.Items, slotIndex); taken {
		return domain.ErrItemSlotTaken
	}
	for index := range room.Items {
		if room.Items[index].ID == itemID {
			room.Items[index].SlotIndex = slotIndex
			sortItemsBySlot(room.Items)
			f.rooms[roomID] = room
			return nil
		}
	}
	return domain.ErrItemNotInRoom
}

func (f *fakeRepo) SwapItemSlots(
	_ context.Context,
	roomID domain.CollectionRoomID,
	first domain.CollectionItemID,
	second domain.CollectionItemID,
) error {
	f.swaps = append(f.swaps, fmt.Sprintf("%s↔%s", first, second))
	if err := f.failOn["SwapItemSlots"]; err != nil {
		return err
	}
	room, ok := f.rooms[roomID]
	if !ok {
		return domain.ErrCollectionRoomNotFound
	}
	firstIndex, secondIndex := -1, -1
	for index, item := range room.Items {
		switch item.ID {
		case first:
			firstIndex = index
		case second:
			secondIndex = index
		}
	}
	if firstIndex < 0 || secondIndex < 0 {
		return domain.ErrItemNotInRoom
	}
	room.Items[firstIndex].SlotIndex, room.Items[secondIndex].SlotIndex =
		room.Items[secondIndex].SlotIndex, room.Items[firstIndex].SlotIndex
	sortItemsBySlot(room.Items)
	f.rooms[roomID] = room
	return nil
}

func sortItemsBySlot(items []domain.CollectionItem) {
	sort.Slice(items, func(a, b int) bool { return items[a].SlotIndex < items[b].SlotIndex })
}

func (f *fakeRepo) seedItems(id domain.CollectionRoomID, slots ...int) []domain.CollectionItem {
	room := f.rooms[id]
	for _, slot := range slots {
		f.nextItem++
		room.Items = append(room.Items, domain.CollectionItem{
			ID:             domain.CollectionItemID(fmt.Sprintf("item-%d", f.nextItem)),
			SlotIndex:      slot,
			CatalogModelID: "model-seed",
		})
	}
	sortItemsBySlot(room.Items)
	f.rooms[id] = room
	return room.Items
}

type fakeModels struct {
	category map[string]string
	err      error
	calls    int
}

func newFakeModels() *fakeModels {
	return &fakeModels{category: map[string]string{
		"model-watch":  "category_watches",
		"model-watch2": "category_watches",
		"model-car":    "category_hot_wheels",
	}}
}

func (f *fakeModels) IsCollectionModelPlaceable(
	_ context.Context, modelID, categoryID string,
) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	scopedTo, known := f.category[modelID]
	if !known {
		return false, nil
	}
	return scopedTo == categoryID, nil
}

type fakeUnitOfWork struct {
	runs int
}

func (f *fakeUnitOfWork) Run(ctx context.Context, fn func(ctx context.Context) error) error {
	f.runs++
	return fn(ctx)
}

type fakeCategories struct {
	known map[string]bool
	err   error
	calls int
}

func newFakeCategories() *fakeCategories {
	return &fakeCategories{known: map[string]bool{
		"category_watches":        true,
		"category_hot_wheels":     true,
		"category_coins":          true,
		"category_license_plates": true,
	}}
}

func (f *fakeCategories) CollectionCategoryExists(_ context.Context, categoryID string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.known[categoryID], nil
}

type fakeDesigns struct {
	universal       map[string]bool
	scoped          map[string]string
	err             error
	tiers           map[string]int
	capacities      map[string][]int
	capacityLookups []int
	calls           int
}

func newFakeDesigns() *fakeDesigns {
	return &fakeDesigns{
		universal: map[string]bool{"design_universal": true},
		scoped:    map[string]string{"design_watches_only": "category_watches"},
	}
}

func (f *fakeDesigns) tierCount(designID string) (int, bool) {
	if f.tiers != nil {
		count, ok := f.tiers[designID]
		return count, ok
	}
	if f.universal[designID] {
		return 3, true
	}
	if _, ok := f.scoped[designID]; ok {
		return 3, true
	}
	return 0, false
}

func (f *fakeDesigns) DesignTierBound(_ context.Context, designID string) (int, bool, error) {
	f.calls++
	if f.err != nil {
		return 0, false, f.err
	}
	count, ok := f.tierCount(designID)
	return count, ok, nil
}

func (f *fakeDesigns) capacityTable(designID string) []int {
	if f.capacities != nil {
		return f.capacities[designID]
	}
	if _, known := f.tierCount(designID); known {
		return []int{4, 10, 18}
	}
	return nil
}

func (f *fakeDesigns) DesignSlotCapacity(_ context.Context, designID string, appAssetVersion int, tier int) (int, bool, error) {
	f.calls++
	f.capacityLookups = append(f.capacityLookups, appAssetVersion)
	if f.err != nil {
		return 0, false, f.err
	}
	table := f.capacityTable(designID)
	if tier < 1 || tier > len(table) {
		return 0, false, nil
	}
	return table[tier-1], true, nil
}

func (f *fakeDesigns) IsDesignApplicable(_ context.Context, designID, categoryID string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	if f.universal[designID] {
		return true, nil
	}
	scopedTo, known := f.scoped[designID]
	if !known {
		return false, nil
	}
	return scopedTo == categoryID, nil
}

func newService() (*application.CollectionRoomService, *fakeRepo) {
	service, repo, _, _ := newServiceWithDependencies()
	return service, repo
}

func newServiceWithCategories() (*application.CollectionRoomService, *fakeRepo, *fakeCategories) {
	service, repo, categories, _ := newServiceWithDependencies()
	return service, repo, categories
}

func newServiceWithDependencies() (
	*application.CollectionRoomService, *fakeRepo, *fakeCategories, *fakeDesigns,
) {
	service, repo, categories, designs, _, _ := newServiceWithItems()
	return service, repo, categories, designs
}

func newServiceWithItems() (
	*application.CollectionRoomService,
	*fakeRepo, *fakeCategories, *fakeDesigns, *fakeModels, *fakeUnitOfWork,
) {
	repo := newFakeRepo()
	categories := newFakeCategories()
	designs := newFakeDesigns()
	models := newFakeModels()
	uow := &fakeUnitOfWork{}
	service := application.NewCollectionRoomService(repo, categories, designs, models).
		WithUnitOfWork(uow)
	return service, repo, categories, designs, models, uow
}

func validInput(name string) application.CreateInput {
	return application.CreateInput{Name: name, CategoryID: "category_watches"}
}

func TestCreate_StartsAtTheBaseTierWithNoItems(t *testing.T) {
	service, _ := newService()

	room, err := service.Create(context.Background(), "acct-1", application.CreateInput{
		Name:       "Watches",
		CategoryID: "category_watches",
		DesignID:   "design_universal",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if room.CurrentTier != domain.BaseTier {
		t.Fatalf("CurrentTier = %d, want BaseTier (%d)", room.CurrentTier, domain.BaseTier)
	}
	if len(room.Items) != 0 {
		t.Fatalf("a new Collection Room has %d items, want 0", len(room.Items))
	}
	if room.AccountID != "acct-1" {
		t.Fatalf("AccountID = %q, want the authenticated account", room.AccountID)
	}
}

func TestCreate_IsUnlimitedPerAccount(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()

	seen := map[domain.CollectionRoomID]bool{}
	for index := 0; index < 50; index++ {
		room, err := service.Create(ctx, "acct-1", validInput(fmt.Sprintf("Room %d", index)))
		if err != nil {
			t.Fatalf("Create #%d refused: %v — no per-account count limit may exist", index+1, err)
		}
		if seen[room.ID] {
			t.Fatalf("duplicate id %q", room.ID)
		}
		seen[room.ID] = true
	}

	rooms, err := service.List(ctx, "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 50 {
		t.Fatalf("List = %d rooms, want 50", len(rooms))
	}
}

func TestCreate_RequiresACategoryButNotADesign(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()

	room, err := service.Create(ctx, "acct-1", validInput("Untitled"))
	if err != nil {
		t.Fatalf("Create without a Design: %v", err)
	}
	if room.DesignID != "" {
		t.Fatalf("DesignID = %q, want unset — chooses it", room.DesignID)
	}
	if room.CategoryID != "category_watches" {
		t.Fatalf("CategoryID = %q, want the supplied category", room.CategoryID)
	}

	if _, err := service.Create(ctx, "acct-1", application.CreateInput{Name: "No category"}); !errors.Is(err, domain.ErrCategoryRequired) {
		t.Fatalf("Create with no category = %v, want ErrCategoryRequired", err)
	}
}

func TestCreate_RefusesAnUnknownCategory(t *testing.T) {
	service, _, categories := newServiceWithCategories()
	ctx := context.Background()

	for _, categoryID := range []string{"watches", "category_stamps", "CATEGORY_WATCHES", "  category_watches  "} {
		_, err := service.Create(ctx, "acct-1", application.CreateInput{Name: "Nope", CategoryID: categoryID})
		if !errors.Is(err, domain.ErrUnknownCategory) {
			t.Fatalf("Create with category %q = %v, want ErrUnknownCategory", categoryID, err)
		}
	}

	for _, categoryID := range []string{"category_watches", "category_hot_wheels", "category_coins", "category_license_plates"} {
		if _, err := service.Create(ctx, "acct-1", application.CreateInput{Name: "Yes", CategoryID: categoryID}); err != nil {
			t.Fatalf("Create with category %q: %v", categoryID, err)
		}
	}

	before := categories.calls
	long := strings.Repeat("x", 1000)
	if _, err := service.Create(ctx, "acct-1", application.CreateInput{Name: "Long", CategoryID: long}); !errors.Is(err, domain.ErrInvalidCategoryReference) {
		t.Fatalf("over-long category = %v, want ErrInvalidCategoryReference", err)
	}
	if categories.calls != before {
		t.Fatalf("the registry was consulted %d extra time(s) for a malformed reference", categories.calls-before)
	}
}

func TestCreate_PropagatesACategoryLookupFailure(t *testing.T) {
	service, _, categories := newServiceWithCategories()
	categories.err = errors.New("registry unavailable")

	_, err := service.Create(context.Background(), "acct-1", validInput("Watches"))
	if err == nil {
		t.Fatal("expected the lookup failure to surface")
	}
	if errors.Is(err, domain.ErrUnknownCategory) {
		t.Fatal("a registry outage must not be reported as an unknown category")
	}
}

func TestUpdate_ValidatesTheCategoryAndRefusesClearingIt(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()

	room, err := service.Create(ctx, "owner", validInput("Watches"))
	if err != nil {
		t.Fatal(err)
	}

	coins := "category_coins"
	updated, err := service.Update(ctx, "owner", room.ID, domain.CollectionRoomPatch{CategoryID: &coins})
	if err != nil {
		t.Fatalf("recategorise: %v", err)
	}
	if updated.CategoryID != coins {
		t.Fatalf("CategoryID = %q, want %q", updated.CategoryID, coins)
	}

	unknown := "category_stamps"
	if _, err := service.Update(ctx, "owner", room.ID, domain.CollectionRoomPatch{CategoryID: &unknown}); !errors.Is(err, domain.ErrUnknownCategory) {
		t.Fatalf("unknown category patch = %v, want ErrUnknownCategory", err)
	}

	empty := ""
	if _, err := service.Update(ctx, "owner", room.ID, domain.CollectionRoomPatch{CategoryID: &empty}); !errors.Is(err, domain.ErrCategoryRequired) {
		t.Fatalf("clearing the category = %v, want ErrCategoryRequired", err)
	}

	after, err := service.Find(ctx, "owner", room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CategoryID != coins {
		t.Fatalf("CategoryID is %q after two rejected patches, want %q", after.CategoryID, coins)
	}
}

func TestCreate_ValidatesInput(t *testing.T) {
	service, _ := newService()
	long := strings.Repeat("x", 1000)

	cases := []struct {
		name      string
		input     application.CreateInput
		wantError error
	}{
		{"blank name", application.CreateInput{Name: "  "}, domain.ErrNameRequired},
		{"long name", application.CreateInput{Name: long}, domain.ErrNameTooLong},
		{"bad category", application.CreateInput{Name: "ok", CategoryID: long}, domain.ErrInvalidCategoryReference},
		{"bad design", application.CreateInput{Name: "ok", CategoryID: "category_watches", DesignID: long}, domain.ErrInvalidDesignReference},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.Create(context.Background(), "acct-1", tc.input); !errors.Is(err, tc.wantError) {
				t.Fatalf("Create = %v, want %v", err, tc.wantError)
			}
		})
	}
}

func TestOwnership_ForeignRoomIsIndistinguishableFromNonexistent(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()

	mine, err := service.Create(ctx, "owner", validInput("Mine"))
	if err != nil {
		t.Fatal(err)
	}
	name := "Renamed by a stranger"

	operations := map[string]func(id domain.CollectionRoomID) error{
		"Find": func(id domain.CollectionRoomID) error {
			_, err := service.Find(ctx, "stranger", id)
			return err
		},
		"Update": func(id domain.CollectionRoomID) error {
			_, err := service.Update(ctx, "stranger", id, domain.CollectionRoomPatch{Name: &name})
			return err
		},
		"Delete": func(id domain.CollectionRoomID) error {
			return service.Delete(ctx, "stranger", id)
		},
	}
	for label, operate := range operations {
		foreign := operate(mine.ID)
		absent := operate("room-does-not-exist")
		if !errors.Is(foreign, domain.ErrCollectionRoomNotFound) {
			t.Fatalf("%s on a foreign room = %v, want ErrCollectionRoomNotFound", label, foreign)
		}
		if foreign.Error() != absent.Error() {
			t.Fatalf("%s: foreign (%v) and nonexistent (%v) must be indistinguishable", label, foreign, absent)
		}
	}

	after, err := service.Find(ctx, "owner", mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != "Mine" {
		t.Fatalf("owner's room was mutated by a stranger: name is now %q", after.Name)
	}
}

func TestList_ReturnsOnlyTheCallersRooms(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()

	for _, account := range []string{"a", "a", "b"} {
		if _, err := service.Create(ctx, account, validInput("Room")); err != nil {
			t.Fatal(err)
		}
	}

	a, err := service.List(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := service.List(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 || len(b) != 1 {
		t.Fatalf("List: account a saw %d rooms, account b saw %d; want 2 and 1", len(a), len(b))
	}

	empty, err := service.List(ctx, "never-created-anything")
	if err != nil {
		t.Fatalf("List for a fresh account: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("fresh account saw %d rooms", len(empty))
	}
}

func TestUpdate_IsTrulyPartial(t *testing.T) {
	service, repo := newService()
	ctx := context.Background()

	room, err := service.Create(ctx, "owner", application.CreateInput{
		Name: "Original", CategoryID: "category_watches", DesignID: "design_universal",
	})
	if err != nil {
		t.Fatal(err)
	}

	design := "design_watches_only"
	updated, err := service.Update(ctx, "owner", room.ID, domain.CollectionRoomPatch{DesignID: &design})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Original" || updated.CategoryID != "category_watches" {
		t.Fatalf("a design-only patch disturbed other fields: name %q category %q", updated.Name, updated.CategoryID)
	}
	if updated.DesignID != "design_watches_only" {
		t.Fatalf("DesignID = %q, want design_watches_only", updated.DesignID)
	}

	if len(repo.updates) != 1 {
		t.Fatalf("recorded %d patches, want 1", len(repo.updates))
	}
	patch := repo.updates[0]
	if patch.Name != nil || patch.CategoryID != nil {
		t.Fatalf("patch carried fields the caller did not set: %+v", patch)
	}

	if updated.CurrentTier != domain.BaseTier {
		t.Fatalf("CurrentTier moved to %d during an update", updated.CurrentTier)
	}
}

func TestUpdate_RejectsAnEmptyPatchAndInvalidValues(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()

	room, err := service.Create(ctx, "owner", validInput("Original"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Update(ctx, "owner", room.ID, domain.CollectionRoomPatch{}); !errors.Is(err, domain.ErrEmptyPatch) {
		t.Fatalf("empty patch = %v, want ErrEmptyPatch", err)
	}

	blank := "   "
	if _, err := service.Update(ctx, "owner", room.ID, domain.CollectionRoomPatch{Name: &blank}); !errors.Is(err, domain.ErrNameRequired) {
		t.Fatalf("blank rename = %v, want ErrNameRequired", err)
	}

	after, err := service.Find(ctx, "owner", room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != "Original" {
		t.Fatalf("name is %q after two rejected updates", after.Name)
	}
}

func TestDelete_RemovesTheRoomAndIsNotRepeatable(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()

	room, err := service.Create(ctx, "owner", validInput("Doomed"))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, "owner", room.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := service.Delete(ctx, "owner", room.ID); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("second Delete = %v, want ErrCollectionRoomNotFound", err)
	}
	if _, err := service.Find(ctx, "owner", room.ID); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("Find after Delete = %v, want ErrCollectionRoomNotFound", err)
	}
}

func TestCreate_PropagatesRepositoryFailure(t *testing.T) {
	service, repo := newService()
	repo.failOn["Create"] = errors.New("database exploded")

	if _, err := service.Create(context.Background(), "acct", validInput("Watches")); err == nil {
		t.Fatal("expected the repository failure to surface")
	}
}

// MARK: -: the tier ratchet

func roomWithDesign(t *testing.T, service *application.CollectionRoomService, designID string) domain.CollectionRoom {
	t.Helper()
	room, err := service.Create(context.Background(), "owner", application.CreateInput{
		Name: "Watches", CategoryID: "category_watches", DesignID: designID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return room
}

func TestRatchetTier_RisesAndNeverFalls(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	raised, err := service.RatchetTier(ctx, "owner", room.ID, 3)
	if err != nil {
		t.Fatalf("ratchet to 3: %v", err)
	}
	if raised.CurrentTier != 3 {
		t.Fatalf("CurrentTier = %d, want 3", raised.CurrentTier)
	}

	for _, lower := range []domain.Tier{3, 2, 1} {
		after, err := service.RatchetTier(ctx, "owner", room.ID, lower)
		if err != nil {
			t.Fatalf("request for tier %d errored: %v", lower, err)
		}
		if after.CurrentTier != 3 {
			t.Fatalf("the tier fell to %d after a request for %d", after.CurrentTier, lower)
		}
	}
}

func TestRatchetTier_RefusesATierTheDesignDoesNotAuthor(t *testing.T) {
	service, _, _, designs := newServiceWithDependencies()
	ctx := context.Background()
	designs.tiers = map[string]int{"design_universal": 3}
	room := roomWithDesign(t, service, "design_universal")

	for _, tooHigh := range []domain.Tier{4, 99} {
		if _, err := service.RatchetTier(ctx, "owner", room.ID, tooHigh); !errors.Is(err, domain.ErrTierNotAuthored) {
			t.Fatalf("tier %d = %v, want ErrTierNotAuthored", tooHigh, err)
		}
	}
	for _, invalid := range []domain.Tier{0, -1} {
		if _, err := service.RatchetTier(ctx, "owner", room.ID, invalid); !errors.Is(err, domain.ErrInvalidTier) {
			t.Fatalf("tier %d = %v, want ErrInvalidTier", invalid, err)
		}
	}

	after, err := service.Find(ctx, "owner", room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CurrentTier != domain.BaseTier {
		t.Fatalf("a refused request moved the tier to %d", after.CurrentTier)
	}
}

func TestRatchetTier_SingleTierDesignCannotExpand(t *testing.T) {
	service, _, _, designs := newServiceWithDependencies()
	ctx := context.Background()
	designs.tiers = map[string]int{"design_universal": 1}
	room := roomWithDesign(t, service, "design_universal")

	if _, err := service.RatchetTier(ctx, "owner", room.ID, 2); !errors.Is(err, domain.ErrTierNotAuthored) {
		t.Fatalf("tier 2 on a one-tier Design = %v, want ErrTierNotAuthored", err)
	}
	if _, err := service.RatchetTier(ctx, "owner", room.ID, 1); err != nil {
		t.Fatalf("tier 1 on a one-tier Design: %v", err)
	}
}

func TestRatchetTier_RequiresADesign(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()

	room, err := service.Create(ctx, "owner", application.CreateInput{
		Name: "No design yet", CategoryID: "category_watches",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.RatchetTier(ctx, "owner", room.ID, 2); !errors.Is(err, domain.ErrDesignRequiredForTier) {
		t.Fatalf("= %v, want ErrDesignRequiredForTier", err)
	}
}

func TestRatchetTier_RefusesWhenTheDesignIsNotServable(t *testing.T) {
	service, _, _, designs := newServiceWithDependencies()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	designs.tiers = map[string]int{}

	if _, err := service.RatchetTier(ctx, "owner", room.ID, 2); !errors.Is(err, domain.ErrTierNotAuthored) {
		t.Fatalf("= %v, want ErrTierNotAuthored", err)
	}
}

func TestRatchetTier_IsOwnerOnly(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	foreign := func() error {
		_, err := service.RatchetTier(ctx, "stranger", room.ID, 3)
		return err
	}()
	absent := func() error {
		_, err := service.RatchetTier(ctx, "stranger", "room-does-not-exist", 3)
		return err
	}()
	if !errors.Is(foreign, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("a stranger's ratchet = %v, want ErrCollectionRoomNotFound", foreign)
	}
	if foreign.Error() != absent.Error() {
		t.Fatalf("foreign (%v) and nonexistent (%v) must be indistinguishable", foreign, absent)
	}

	after, err := service.Find(ctx, "owner", room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CurrentTier != domain.BaseTier {
		t.Fatalf("a stranger moved the tier to %d", after.CurrentTier)
	}
}

func TestRatchetTier_PropagatesARegistryFailure(t *testing.T) {
	service, _, _, designs := newServiceWithDependencies()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	designs.err = errors.New("registry unavailable")

	_, err := service.RatchetTier(ctx, "owner", room.ID, 2)
	if err == nil {
		t.Fatal("expected the lookup failure to surface")
	}
	if errors.Is(err, domain.ErrTierNotAuthored) {
		t.Fatal("a registry outage must not be reported as an unauthored tier")
	}
}

// MARK: -: item placement and reordering

func TestAddItem_TakesTheLowestFreeSlotFromTheLockedView(t *testing.T) {
	service, repo, _, _, _, uow := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	for expected := 0; expected < 3; expected++ {
		updated, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1)
		if err != nil {
			t.Fatal(err)
		}
		if got := updated.Items[len(updated.Items)-1].SlotIndex; got != expected {
			t.Fatalf("item %d landed at slot %d, want %d", expected, got, expected)
		}
	}
	if uow.runs != 3 {
		t.Fatalf("%d transactions for 3 placements, want 3", uow.runs)
	}
	if len(repo.locks) != 3 {
		t.Fatalf("the Room was locked %d times, want 3 — the slot must come from the locked view", len(repo.locks))
	}
	if fmt.Sprint(repo.inserts) != "[0 1 2]" {
		t.Fatalf("inserted at slots %v, want [0 1 2]", repo.inserts)
	}
}

func TestAddItem_FillsAHoleLeftByAMove(t *testing.T) {
	service, repo, _, _, _, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	repo.seedItems(room.ID, 1, 2, 3)

	updated, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Items[0].SlotIndex != 0 {
		t.Fatalf("the new item did not take slot 0; slots are %v", slotIndices(updated.Items))
	}
}

func TestAddItem_RefusesAModelTheCatalogWillNotVouchFor(t *testing.T) {
	service, repo, _, _, models, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	for name, modelID := range map[string]string{
		"unknown":               "model-nope",
		"another category's":    "model-car",
		"empty":                 "",
		"malformed (over-long)": strings.Repeat("m", domain.InterimMaximumModelReferenceBytes+1),
	} {
		if _, err := service.AddItem(ctx, "owner", room.ID, modelID, 1); err == nil {
			t.Fatalf("%s model was accepted", name)
		}
	}
	if len(repo.inserts) != 0 {
		t.Fatalf("a refused placement still wrote: %v", repo.inserts)
	}
	if models.calls > 2 {
		t.Fatalf("the catalog was consulted %d times for 4 attempts — malformed and empty references must be refused first", models.calls)
	}
}

func TestAddItem_ChecksOwnershipBeforeTheCatalog(t *testing.T) {
	service, repo, _, _, models, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	if _, err := service.AddItem(ctx, "stranger", room.ID, "model-watch", 1); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("stranger placement = %v, want ErrCollectionRoomNotFound", err)
	}
	if models.calls != 0 {
		t.Fatal("the catalog was consulted for a Room the caller does not own")
	}
	if len(repo.locks) != 0 {
		t.Fatal("a lock was taken for a Room the caller does not own")
	}
}

func TestAddItem_PropagatesACatalogFailure(t *testing.T) {
	service, _, _, _, models, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	models.err = errors.New("catalog unavailable")

	_, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1)
	if err == nil {
		t.Fatal("expected the lookup failure to surface")
	}
	if errors.Is(err, domain.ErrModelNotAvailable) {
		t.Fatal("a catalog outage must not be reported as an unavailable Model")
	}
}

func TestItemMutation_RequiresATransactionBoundary(t *testing.T) {
	repo := newFakeRepo()
	service := application.NewCollectionRoomService(repo, newFakeCategories(), newFakeDesigns(), newFakeModels())
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")

	if _, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1); !errors.Is(err, domain.ErrTransactionsUnavailable) {
		t.Fatalf("AddItem without a unit of work = %v, want ErrTransactionsUnavailable", err)
	}
	if _, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, "item-1", 1, 1); !errors.Is(err, domain.ErrTransactionsUnavailable) {
		t.Fatalf("PlaceItemAtSlot without a unit of work = %v, want ErrTransactionsUnavailable", err)
	}
}

func TestPlaceItemAtSlot_AppliesTheThreeDropOutcomes(t *testing.T) {
	service, repo, _, _, _, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	seeded := repo.seedItems(room.ID, 0, 1, 2)
	first, third := seeded[0].ID, seeded[2].ID

	updated, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, first, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.swaps) != 1 || len(repo.moves) != 0 {
		t.Fatalf("swaps=%v moves=%v, want exactly one swap", repo.swaps, repo.moves)
	}
	if slotOf(updated.Items, first) != 2 || slotOf(updated.Items, third) != 0 {
		t.Fatalf("slots after swap: %v", slotIndices(updated.Items))
	}

	before := slotIndices(updated.Items)
	updated, err = service.PlaceItemAtSlot(ctx, "owner", room.ID, first, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.moves) != 1 || len(repo.swaps) != 1 {
		t.Fatalf("swaps=%v moves=%v, want one of each by now", repo.swaps, repo.moves)
	}
	if slotOf(updated.Items, first) != 3 {
		t.Fatalf("the moved item is at %d, want 3", slotOf(updated.Items, first))
	}
	if len(updated.Items) != len(before) {
		t.Fatalf("a move changed the item count")
	}

	if _, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, first, 3, 1); err != nil {
		t.Fatal(err)
	}
	if len(repo.moves) != 1 || len(repo.swaps) != 1 {
		t.Fatalf("a no-op placement wrote: swaps=%v moves=%v", repo.swaps, repo.moves)
	}
}

func TestItemMutation_NeverTouchesTheTier(t *testing.T) {
	service, repo, _, _, _, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	if _, err := service.RatchetTier(ctx, "owner", room.ID, 2); err != nil {
		t.Fatal(err)
	}
	seeded := repo.seedItems(room.ID, 0, 1, 2, 3, 4)

	for _, target := range []int{4, 0, 7, 2, 9} {
		if _, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, seeded[0].ID, target, 1); err != nil {
			t.Fatalf("placing at %d: %v", target, err)
		}
		current, err := service.Find(ctx, "owner", room.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.CurrentTier != domain.Tier(2) {
			t.Fatalf("the tier became %d after placing at slot %d", current.CurrentTier, target)
		}
	}
	before, err := service.Find(ctx, "owner", room.ID)
	if err != nil {
		t.Fatal(err)
	}
	movesBefore, swapsBefore := len(repo.moves), len(repo.swaps)
	for _, target := range []int{10, 17, 400} {
		if _, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, seeded[0].ID, target, 1); !errors.Is(err, domain.ErrSlotNotAvailable) {
			t.Fatalf("placing at unreached slot %d → %v, want ErrSlotNotAvailable", target, err)
		}
	}
	after, err := service.Find(ctx, "owner", room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CurrentTier != before.CurrentTier || fmt.Sprint(after.Items) != fmt.Sprint(before.Items) {
		t.Fatalf("a rejected drop changed the Room:\nbefore %+v\nafter  %+v", before, after)
	}
	if len(repo.moves) != movesBefore || len(repo.swaps) != swapsBefore {
		t.Fatal("a rejected drop reached the repository")
	}
	if _, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1); err != nil {
		t.Fatal(err)
	}
	current, err := service.Find(ctx, "owner", room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.CurrentTier != domain.Tier(2) {
		t.Fatalf("placement raised the tier to %d", current.CurrentTier)
	}
}

func TestPlaceItemAtSlot_IsOwnerOnlyAndIndistinguishable(t *testing.T) {
	service, repo, _, _, _, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	seeded := repo.seedItems(room.ID, 0, 1)

	real := func() error {
		_, err := service.PlaceItemAtSlot(ctx, "stranger", room.ID, seeded[0].ID, 1, 1)
		return err
	}()
	invented := func() error {
		_, err := service.PlaceItemAtSlot(ctx, "stranger", room.ID, "item-nope", 1, 1)
		return err
	}()
	if !errors.Is(real, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("stranger placement = %v, want ErrCollectionRoomNotFound", real)
	}
	if real.Error() != invented.Error() {
		t.Fatalf("a stranger can distinguish item ids: %v vs %v", real, invented)
	}
	if len(repo.moves) != 0 || len(repo.swaps) != 0 {
		t.Fatalf("a stranger's request wrote: moves=%v swaps=%v", repo.moves, repo.swaps)
	}

	if _, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, "item-nope", 1, 1); !errors.Is(err, domain.ErrItemNotInRoom) {
		t.Fatalf("owner, unknown item = %v, want ErrItemNotInRoom", err)
	}
}

func slotIndices(items []domain.CollectionItem) []int {
	out := make([]int, 0, len(items))
	for _, item := range items {
		out = append(out, item.SlotIndex)
	}
	return out
}

func slotOf(items []domain.CollectionItem, id domain.CollectionItemID) int {
	for _, item := range items {
		if item.ID == id {
			return item.SlotIndex
		}
	}
	return -1
}

// MARK: - close-out: the capacity ceiling, server-side

func TestAddItem_RefusesWhenTheReachedTierIsFullUntilRatcheted(t *testing.T) {
	service, repo, _, _, _, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	repo.seedItems(room.ID, 0, 1, 2, 3)

	_, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1)
	if !errors.Is(err, domain.ErrTierCapacityReached) {
		t.Fatalf("add into a full tier → %v, want ErrTierCapacityReached", err)
	}
	if len(repo.inserts) != 0 {
		t.Fatalf("a refused placement wrote: %v", repo.inserts)
	}
	current, err := service.Find(ctx, "owner", room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.CurrentTier != domain.BaseTier || len(current.Items) != 4 {
		t.Fatalf("a refused placement changed the Room: tier %d, %d items", current.CurrentTier, len(current.Items))
	}

	if _, err := service.RatchetTier(ctx, "owner", room.ID, 2); err != nil {
		t.Fatal(err)
	}
	updated, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1)
	if err != nil {
		t.Fatal(err)
	}
	if repo.inserts[len(repo.inserts)-1] != 4 || len(updated.Items) != 5 {
		t.Fatalf("after ratcheting the item landed at %v", repo.inserts)
	}
}

func TestAddItem_FillsAHoleBelowTheCapacityBeforeRefusing(t *testing.T) {
	service, repo, _, _, _, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	repo.seedItems(room.ID, 1, 2, 3)

	updated, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1)
	if err != nil {
		t.Fatal(err)
	}
	if slotIndices(updated.Items)[0] != 0 {
		t.Fatalf("the hole was not filled: %v", slotIndices(updated.Items))
	}
}

func TestPlaceItemAtSlot_RefusesAFutureTierSlotAndChangesNothing(t *testing.T) {
	service, repo, _, _, _, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	seeded := repo.seedItems(room.ID, 0, 1)
	before, _ := service.Find(ctx, "owner", room.ID)

	for _, target := range []int{4, 9, 17, 18, 4096} {
		_, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, seeded[0].ID, target, 1)
		if !errors.Is(err, domain.ErrSlotNotAvailable) {
			t.Fatalf("slot %d at tier 1 → %v, want ErrSlotNotAvailable", target, err)
		}
	}
	after, _ := service.Find(ctx, "owner", room.ID)
	if fmt.Sprint(after.Items) != fmt.Sprint(before.Items) || after.CurrentTier != before.CurrentTier {
		t.Fatalf("a rejected drop changed the Room:\nbefore %+v\nafter  %+v", before, after)
	}
	if len(repo.moves)+len(repo.swaps) != 0 {
		t.Fatalf("a rejected drop reached the repository: moves=%v swaps=%v", repo.moves, repo.swaps)
	}

	if _, err := service.RatchetTier(ctx, "owner", room.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, seeded[0].ID, 9, 1); err != nil {
		t.Fatalf("slot 9 at tier 2 → %v, want a move", err)
	}
}

func TestItemWrites_FailClosedWithoutADerivedCapacity(t *testing.T) {
	ctx := context.Background()

	service, repo, _, _, _, _ := newServiceWithItems()
	undesigned, err := service.Create(ctx, "owner", validInput("Watches"))
	if err != nil {
		t.Fatal(err)
	}
	seeded := repo.seedItems(undesigned.ID, 0, 1)
	if _, err := service.AddItem(ctx, "owner", undesigned.ID, "model-watch", 1); !errors.Is(err, domain.ErrDesignRequiredForItems) {
		t.Fatalf("add without a Design → %v, want ErrDesignRequiredForItems", err)
	}
	if _, err := service.PlaceItemAtSlot(ctx, "owner", undesigned.ID, seeded[0].ID, 1, 1); !errors.Is(err, domain.ErrDesignRequiredForItems) {
		t.Fatalf("place without a Design → %v, want ErrDesignRequiredForItems", err)
	}

	service, repo, _, designs, _, _ := newServiceWithItems()
	designs.capacities = map[string][]int{}
	room := roomWithDesign(t, service, "design_universal")
	seeded = repo.seedItems(room.ID, 0, 1)
	if _, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 1); !errors.Is(err, domain.ErrDesignLayoutUnavailable) {
		t.Fatalf("add with nothing published → %v, want ErrDesignLayoutUnavailable", err)
	}
	if _, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, seeded[0].ID, 1, 1); !errors.Is(err, domain.ErrDesignLayoutUnavailable) {
		t.Fatalf("place with nothing published → %v, want ErrDesignLayoutUnavailable", err)
	}
	if len(repo.inserts)+len(repo.moves)+len(repo.swaps) != 0 {
		t.Fatal("a fail-closed refusal reached the repository")
	}
}

func TestItemWrites_PassTheClientsAssetGenerationToTheCatalog(t *testing.T) {
	service, repo, _, designs, _, _ := newServiceWithItems()
	ctx := context.Background()
	room := roomWithDesign(t, service, "design_universal")
	seeded := repo.seedItems(room.ID, 0, 1)

	if _, err := service.AddItem(ctx, "owner", room.ID, "model-watch", 7); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PlaceItemAtSlot(ctx, "owner", room.ID, seeded[0].ID, 3, 3); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(designs.capacityLookups) != "[7 3]" {
		t.Fatalf("capacity lookups were asked for generations %v, want [7 3]", designs.capacityLookups)
	}
}
