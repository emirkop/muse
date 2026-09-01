package domain_test

import (
	"errors"
	"testing"

	"muse-backend/internal/collection/domain"
)

func TestTierCapacities_RejectsIncoherentTables(t *testing.T) {
	cases := []struct {
		name      string
		table     domain.TierCapacities
		wantError error
	}{
		{"empty", domain.TierCapacities{}, domain.ErrNoTierCapacities},
		{"nil", nil, domain.ErrNoTierCapacities},
		{"zero capacity", domain.TierCapacities{0}, domain.ErrTierCapacitiesNotIncreasing},
		{"negative", domain.TierCapacities{-4}, domain.ErrTierCapacitiesNotIncreasing},
		{"flat", domain.TierCapacities{10, 10}, domain.ErrTierCapacitiesNotIncreasing},
		{"shrinking", domain.TierCapacities{10, 6}, domain.ErrTierCapacitiesNotIncreasing},
		{"shrinks later", domain.TierCapacities{10, 20, 15}, domain.ErrTierCapacitiesNotIncreasing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.table.Validate(); !errors.Is(err, tc.wantError) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantError)
			}
		})
	}
}

func TestTierCapacities_AcceptsAnyStrictlyGrowingTable(t *testing.T) {
	for _, table := range []domain.TierCapacities{
		{1},
		{10, 24, 48},
		{3, 4, 5, 6, 7},
		{1, 1000, 100000},
	} {
		if err := table.Validate(); err != nil {
			t.Fatalf("Validate(%v) = %v, want nil", table, err)
		}
	}
}

func TestTierCapacities_HighestTierAndCapacityOf(t *testing.T) {
	table := domain.TierCapacities{10, 24, 48}

	if got := table.HighestTier(); got != 3 {
		t.Fatalf("HighestTier() = %d, want 3", got)
	}

	for tier, want := range map[domain.Tier]int{1: 10, 2: 24, 3: 48} {
		got, err := table.CapacityOf(tier)
		if err != nil {
			t.Fatalf("CapacityOf(%d) error: %v", tier, err)
		}
		if got != want {
			t.Fatalf("CapacityOf(%d) = %d, want %d", tier, got, want)
		}
	}

	for _, tier := range []domain.Tier{0, -1, 4, 99} {
		if _, err := table.CapacityOf(tier); !errors.Is(err, domain.ErrUnknownTier) {
			t.Fatalf("CapacityOf(%d) = %v, want ErrUnknownTier", tier, err)
		}
	}
}

func TestRequiredTier_AtEveryBoundary(t *testing.T) {
	table := domain.TierCapacities{10, 24, 48}

	cases := []struct {
		itemCount int
		wantTier  domain.Tier
	}{
		{0, 1},
		{1, 1},
		{9, 1},
		{10, 1},
		{11, 2},
		{23, 2},
		{24, 2},
		{25, 3},
		{47, 3},
		{48, 3},
	}
	for _, tc := range cases {
		got, err := domain.RequiredTier(tc.itemCount, table)
		if err != nil {
			t.Fatalf("RequiredTier(%d) error: %v", tc.itemCount, err)
		}
		if got != tc.wantTier {
			t.Fatalf("RequiredTier(%d) = tier %d, want tier %d", tc.itemCount, got, tc.wantTier)
		}
	}
}

func TestRequiredTier_PastTheHighestAuthoredTier_RefusesRatherThanExtrapolating(t *testing.T) {
	table := domain.TierCapacities{10, 24, 48}

	if _, err := domain.RequiredTier(49, table); !errors.Is(err, domain.ErrTierCapacityExhausted) {
		t.Fatalf("RequiredTier(49) = %v, want ErrTierCapacityExhausted", err)
	}
	if _, err := domain.RequiredTier(10_000, table); !errors.Is(err, domain.ErrTierCapacityExhausted) {
		t.Fatalf("RequiredTier(10000) = %v, want ErrTierCapacityExhausted", err)
	}
}

func TestRequiredTier_RejectsBadInput(t *testing.T) {
	if _, err := domain.RequiredTier(-1, domain.TierCapacities{10}); !errors.Is(err, domain.ErrNegativeItemCount) {
		t.Fatalf("negative count = %v, want ErrNegativeItemCount", err)
	}
	if _, err := domain.RequiredTier(3, domain.TierCapacities{10, 5}); !errors.Is(err, domain.ErrTierCapacitiesNotIncreasing) {
		t.Fatalf("bad table = %v, want ErrTierCapacitiesNotIncreasing", err)
	}
}

func TestRequiredTier_SingleTierDesign(t *testing.T) {
	table := domain.TierCapacities{5}
	for count := 0; count <= 5; count++ {
		got, err := domain.RequiredTier(count, table)
		if err != nil {
			t.Fatalf("RequiredTier(%d) error: %v", count, err)
		}
		if got != domain.BaseTier {
			t.Fatalf("RequiredTier(%d) = %d, want BaseTier", count, got)
		}
	}
	if _, err := domain.RequiredTier(6, table); !errors.Is(err, domain.ErrTierCapacityExhausted) {
		t.Fatalf("RequiredTier(6) = %v, want ErrTierCapacityExhausted", err)
	}
}

func TestRequiredTier_IsSymmetric_SoTheRatchetPolicyStaysOpen(t *testing.T) {
	table := domain.TierCapacities{10, 24, 48}

	grown, err := domain.RequiredTier(30, table)
	if err != nil {
		t.Fatal(err)
	}
	shrunk, err := domain.RequiredTier(8, table)
	if err != nil {
		t.Fatal(err)
	}
	if grown != 3 || shrunk != 1 {
		t.Fatalf("got tiers %d and %d, want 3 and 1", grown, shrunk)
	}
}

func TestCollectionRoom_HasNoItemCap(t *testing.T) {
	items := make([]domain.CollectionItem, 0, 500)
	for index := 0; index < 500; index++ {
		items = append(items, domain.CollectionItem{SlotIndex: index})
	}
	room := domain.CollectionRoom{Items: items}

	if room.ItemCount() != 500 {
		t.Fatalf("ItemCount = %d, want 500", room.ItemCount())
	}
	if got := domain.LowestFreeSlotIndex(room.Items); got != 500 {
		t.Fatalf("LowestFreeSlotIndex = %d, want 500 — no ceiling may exist", got)
	}
}

// MARK: -: the ratchet

func TestRatchetedTier_OnlyEverRises(t *testing.T) {
	cases := []struct {
		current, requested, want domain.Tier
	}{
		{1, 1, 1},
		{1, 2, 2},
		{1, 3, 3},
		{3, 1, 3},
		{3, 2, 3},
		{3, 3, 3},
		{5, 0, 5},
		{5, -1, 5},
	}
	for _, tc := range cases {
		if got := domain.RatchetedTier(tc.current, tc.requested); got != tc.want {
			t.Fatalf("RatchetedTier(current %d, requested %d) = %d, want %d",
				tc.current, tc.requested, got, tc.want)
		}
	}
}

func TestRatchet_KeepsTheHighWaterMarkAsItemsAreRemoved(t *testing.T) {
	table := domain.TierCapacities{4, 10, 18}
	stored := domain.BaseTier

	required, err := domain.RequiredTier(15, table)
	if err != nil {
		t.Fatal(err)
	}
	stored = domain.RatchetedTier(stored, required)
	if stored != 3 {
		t.Fatalf("after 15 items the room is tier %d, want 3", stored)
	}

	required, err = domain.RequiredTier(1, table)
	if err != nil {
		t.Fatal(err)
	}
	if required != 1 {
		t.Fatalf("1 item requires tier %d, want 1", required)
	}
	stored = domain.RatchetedTier(stored, required)
	if stored != 3 {
		t.Fatalf("the tier retracted to %d — says it never shrinks", stored)
	}
}

func TestValidateTierRequest(t *testing.T) {
	const authored = 3

	for _, tier := range []domain.Tier{1, 2, 3} {
		if err := domain.ValidateTierRequest(tier, authored); err != nil {
			t.Fatalf("tier %d should be authored: %v", tier, err)
		}
	}
	for _, tier := range []domain.Tier{4, 99} {
		if err := domain.ValidateTierRequest(tier, authored); !errors.Is(err, domain.ErrTierNotAuthored) {
			t.Fatalf("tier %d = %v, want ErrTierNotAuthored", tier, err)
		}
	}
	for _, tier := range []domain.Tier{0, -1} {
		if err := domain.ValidateTierRequest(tier, authored); !errors.Is(err, domain.ErrInvalidTier) {
			t.Fatalf("tier %d = %v, want ErrInvalidTier", tier, err)
		}
	}
	if err := domain.ValidateTierRequest(1, 0); !errors.Is(err, domain.ErrTierNotAuthored) {
		t.Fatalf("zero authored tiers = %v, want ErrTierNotAuthored", err)
	}
}

func TestFixtureTierTable_BoundariesAndExhaustion(t *testing.T) {
	table := domain.TierCapacities{4, 10, 18}

	expected := map[int]domain.Tier{
		0: 1, 1: 1, 4: 1,
		5: 2, 10: 2,
		11: 3, 18: 3,
	}
	for itemCount, wantTier := range expected {
		got, err := domain.RequiredTier(itemCount, table)
		if err != nil {
			t.Fatalf("%d items: %v", itemCount, err)
		}
		if got != wantTier {
			t.Fatalf("%d items requires tier %d, want %d", itemCount, got, wantTier)
		}
	}

	if _, err := domain.RequiredTier(19, table); !errors.Is(err, domain.ErrTierCapacityExhausted) {
		t.Fatalf("19 items = %v, want ErrTierCapacityExhausted", err)
	}
}

func TestCapacityOf_ValidatesTheTableBeforeAnsweringAndBoundsTheTier(t *testing.T) {
	incoherent := domain.TierCapacities{4, 4, 18}
	if _, err := incoherent.CapacityOf(domain.BaseTier); err == nil {
		t.Fatal("a non-increasing table must not answer a capacity")
	}

	valid := domain.TierCapacities{4, 10, 18}
	for tier, want := range map[domain.Tier]int{domain.BaseTier: 4, domain.BaseTier + 1: 10, domain.BaseTier + 2: 18} {
		got, err := valid.CapacityOf(tier)
		if err != nil {
			t.Errorf("tier %d: %v", tier, err)
		}
		if got != want {
			t.Errorf("tier %d capacity = %d, want %d", tier, got, want)
		}
	}
	for _, tier := range []domain.Tier{domain.BaseTier - 1, 0, valid.HighestTier() + 1, 99} {
		if _, err := valid.CapacityOf(tier); !errors.Is(err, domain.ErrUnknownTier) {
			t.Errorf("tier %d → %v, want domain.ErrUnknownTier", tier, err)
		}
	}
}

func TestCollectionRoomPatch_IsEmptyOnlyWhenNothingIsSupplied(t *testing.T) {
	if !(domain.CollectionRoomPatch{}).IsEmpty() {
		t.Error("a patch with no fields is empty")
	}
	name, category, design := "Watches", "category_watches", "dev-fixture:collection-design"
	for label, patch := range map[string]domain.CollectionRoomPatch{
		"name only":     {Name: &name},
		"category only": {CategoryID: &category},
		"design only":   {DesignID: &design},
		"all three":     {Name: &name, CategoryID: &category, DesignID: &design},
	} {
		if patch.IsEmpty() {
			t.Errorf("%s: a patch supplying a field is not empty", label)
		}
	}
	blank := ""
	if (domain.CollectionRoomPatch{DesignID: &blank}).IsEmpty() {
		t.Error("clearing a Design is a change, not an empty patch")
	}
}
