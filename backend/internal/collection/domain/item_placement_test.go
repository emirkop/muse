package domain_test

import (
	"errors"
	"strings"
	"testing"

	"muse-backend/internal/collection/domain"
)

const generousCapacity = 1 << 20

func items(slots ...int) []domain.CollectionItem {
	out := make([]domain.CollectionItem, 0, len(slots))
	for position, slot := range slots {
		out = append(out, domain.CollectionItem{
			ID:             domain.CollectionItemID(string(rune('a' + position))),
			SlotIndex:      slot,
			CatalogModelID: "model",
		})
	}
	return out
}

func TestValidateModelReference(t *testing.T) {
	if err := domain.ValidateModelReference("dev-fixture:model-chrono-one"); err != nil {
		t.Fatalf("a well-formed reference was refused: %v", err)
	}
	if err := domain.ValidateModelReference(""); !errors.Is(err, domain.ErrModelReferenceRequired) {
		t.Fatalf("empty → %v, want ErrModelReferenceRequired", err)
	}
	long := strings.Repeat("m", domain.InterimMaximumModelReferenceBytes+1)
	if err := domain.ValidateModelReference(long); !errors.Is(err, domain.ErrInvalidModelReference) {
		t.Fatalf("over-long → %v, want ErrInvalidModelReference", err)
	}
	if err := domain.ValidateModelReference("\xff\xfe"); !errors.Is(err, domain.ErrInvalidModelReference) {
		t.Fatalf("invalid UTF-8 → %v, want ErrInvalidModelReference", err)
	}
}

func TestValidateSlotIndex_IsOnlyTheFloor(t *testing.T) {
	if err := domain.ValidateSlotIndex(-1); !errors.Is(err, domain.ErrInvalidSlotIndex) {
		t.Fatalf("-1 → %v, want ErrInvalidSlotIndex", err)
	}
	for _, index := range []int{0, 1, 27, 28, 5000, 1 << 20} {
		if err := domain.ValidateSlotIndex(index); err != nil {
			t.Fatalf("slot %d refused by the format bound: %v — the ceiling is ResolveSlotChange's", index, err)
		}
	}
}

func TestLowestFreeSlotIndex(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		occupied []int
		want     int
	}{
		{"empty room", nil, 0},
		{"contiguous appends", []int{0, 1, 2}, 3},
		{"a hole at the start", []int{1, 2, 3}, 0},
		{"a hole in the middle", []int{0, 1, 3, 4}, 2},
		{"holes are filled lowest first", []int{0, 2, 4, 6}, 1},
		{"out of order input", []int{3, 0, 2, 1}, 4},
		{"far-flung indices do not raise the answer", []int{0, 1, 4096}, 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := domain.LowestFreeSlotIndex(items(testCase.occupied...)); got != testCase.want {
				t.Fatalf("LowestFreeSlotIndex(%v) = %d, want %d", testCase.occupied, got, testCase.want)
			}
		})
	}
}

func TestLowestFreeSlotIndex_NeverExceedsTheItemCount(t *testing.T) {
	for _, occupied := range [][]int{
		{}, {0}, {5}, {0, 1, 2}, {9, 8, 7}, {0, 3, 7, 11}, {100, 200, 300},
	} {
		got := domain.LowestFreeSlotIndex(items(occupied...))
		if got > len(occupied) {
			t.Fatalf("LowestFreeSlotIndex(%v) = %d, which exceeds the item count %d",
				occupied, got, len(occupied))
		}
	}
}

// MARK: - rule 4: the three drop outcomes

func TestResolveSlotChange_OccupiedTargetSwaps(t *testing.T) {
	room := items(0, 1, 2)

	change, err := domain.ResolveSlotChange(room, room[0].ID, 2, generousCapacity)
	if err != nil {
		t.Fatal(err)
	}
	if change.Kind != domain.SlotChangeSwap {
		t.Fatalf("kind = %v, want SlotChangeSwap", change.Kind)
	}
	if change.Item.ID != room[0].ID || change.Displaced.ID != room[2].ID {
		t.Fatalf("swapping %q with %q, want %q with %q",
			change.Item.ID, change.Displaced.ID, room[0].ID, room[2].ID)
	}
	if change.TargetSlotIndex != 2 {
		t.Fatalf("target = %d, want 2", change.TargetSlotIndex)
	}
}

func TestResolveSlotChange_EmptyTargetMoves(t *testing.T) {
	room := items(0, 1, 2)

	change, err := domain.ResolveSlotChange(room, room[1].ID, 7, generousCapacity)
	if err != nil {
		t.Fatal(err)
	}
	if change.Kind != domain.SlotChangeMove {
		t.Fatalf("kind = %v, want SlotChangeMove", change.Kind)
	}
	if change.Displaced.ID != "" {
		t.Fatalf("a move named a displaced item %q — nothing else may move", change.Displaced.ID)
	}
	if change.TargetSlotIndex != 7 {
		t.Fatalf("target = %d, want 7", change.TargetSlotIndex)
	}
}

func TestResolveSlotChange_SameSlotIsANoOp(t *testing.T) {
	room := items(0, 1, 2)

	change, err := domain.ResolveSlotChange(room, room[1].ID, 1, generousCapacity)
	if err != nil {
		t.Fatal(err)
	}
	if change.Kind != domain.SlotChangeNone {
		t.Fatalf("kind = %v, want SlotChangeNone", change.Kind)
	}
}

func TestResolveSlotChange_NeverNamesMoreThanTwoItems(t *testing.T) {
	room := items(0, 1, 2, 3, 4, 5)

	for _, target := range []int{0, 1, 2, 3, 4, 5, 6, 40} {
		change, err := domain.ResolveSlotChange(room, room[3].ID, target, generousCapacity)
		if err != nil {
			t.Fatal(err)
		}
		named := 1
		if change.Displaced.ID != "" {
			named = 2
		}
		if change.Kind == domain.SlotChangeNone {
			named = 0
		}
		if named > 2 {
			t.Fatalf("dropping onto %d named %d items", target, named)
		}
		if change.Kind == domain.SlotChangeSwap {
			occupant, _ := domain.ItemAtSlot(room, target)
			if change.Displaced.ID != occupant.ID {
				t.Fatalf("swap at %d displaced %q, want the occupant %q",
					target, change.Displaced.ID, occupant.ID)
			}
		}
	}
}

func TestResolveSlotChange_RefusesUnknownItemsAndNegativeSlots(t *testing.T) {
	room := items(0, 1)

	if _, err := domain.ResolveSlotChange(room, "not-in-this-room", 0, generousCapacity); !errors.Is(err, domain.ErrItemNotInRoom) {
		t.Fatalf("unknown item → %v, want ErrItemNotInRoom", err)
	}
	if _, err := domain.ResolveSlotChange(nil, room[0].ID, 0, generousCapacity); !errors.Is(err, domain.ErrItemNotInRoom) {
		t.Fatalf("empty room → %v, want ErrItemNotInRoom", err)
	}
	if _, err := domain.ResolveSlotChange(room, room[0].ID, -1, generousCapacity); !errors.Is(err, domain.ErrInvalidSlotIndex) {
		t.Fatalf("negative slot → %v, want ErrInvalidSlotIndex", err)
	}
}

func TestResolveSlotChange_IsIndifferentToHowFarApartTheSlotsAre(t *testing.T) {
	room := items(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)

	within, err := domain.ResolveSlotChange(room, room[0].ID, 1, generousCapacity)
	if err != nil {
		t.Fatal(err)
	}
	across, err := domain.ResolveSlotChange(room, room[0].ID, 9, generousCapacity)
	if err != nil {
		t.Fatal(err)
	}
	if within.Kind != across.Kind {
		t.Fatalf("a within-tier swap resolved as %v and a cross-tier one as %v", within.Kind, across.Kind)
	}
}

func TestSlotChange_CarriesNoTier(t *testing.T) {
	room := items(0, 1)
	change, err := domain.ResolveSlotChange(room, room[0].ID, 1, generousCapacity)
	if err != nil {
		t.Fatal(err)
	}
	_ = change
	if domain.BaseTier != 1 {
		t.Fatalf("BaseTier = %d — placement assumes tiers start at 1", domain.BaseTier)
	}
}

func TestItemAtSlotAndItemByID(t *testing.T) {
	room := items(0, 2, 5)

	if found, ok := domain.ItemAtSlot(room, 2); !ok || found.ID != room[1].ID {
		t.Fatalf("ItemAtSlot(2) = %v, %v", found.ID, ok)
	}
	if _, ok := domain.ItemAtSlot(room, 1); ok {
		t.Fatal("ItemAtSlot found something in a hole")
	}
	if found, ok := domain.ItemByID(room, room[2].ID); !ok || found.SlotIndex != 5 {
		t.Fatalf("ItemByID = slot %d, %v", found.SlotIndex, ok)
	}
	if _, ok := domain.ItemByID(room, "nope"); ok {
		t.Fatal("ItemByID found an item that does not exist")
	}
}

// MARK: - rule 4's rejection, now the server's

func TestResolveSlotChange_RefusesASlotTheReachedTierDoesNotAuthor(t *testing.T) {
	room := items(0, 1, 2)
	const tierOneCapacity = 4

	for _, future := range []int{4, 5, 9, 17, 18, 4096} {
		_, err := domain.ResolveSlotChange(room, room[0].ID, future, tierOneCapacity)
		if !errors.Is(err, domain.ErrSlotNotAvailable) {
			t.Fatalf("slot %d at capacity %d → %v, want ErrSlotNotAvailable", future, tierOneCapacity, err)
		}
	}
	change, err := domain.ResolveSlotChange(room, room[0].ID, 3, tierOneCapacity)
	if err != nil || change.Kind != domain.SlotChangeMove {
		t.Fatalf("slot 3 at capacity 4 → %v, %v; want a move", change.Kind, err)
	}
}

func TestResolveSlotChange_RefusesASwapOntoAnUnreachedSlotButAllowsMovingOffIt(t *testing.T) {
	stranded := []domain.CollectionItem{
		{ID: "a", SlotIndex: 0, CatalogModelID: "m"},
		{ID: "b", SlotIndex: 4096, CatalogModelID: "m"},
	}
	if _, err := domain.ResolveSlotChange(stranded, "a", 4096, 4); !errors.Is(err, domain.ErrSlotNotAvailable) {
		t.Fatalf("swap onto an unreached occupied slot → %v, want ErrSlotNotAvailable", err)
	}
	change, err := domain.ResolveSlotChange(stranded, "b", 1, 4)
	if err != nil || change.Kind != domain.SlotChangeMove || change.TargetSlotIndex != 1 {
		t.Fatalf("moving the stranded item back → %v, %v; want a move to 1", change.Kind, err)
	}
}

func TestResolveSlotChange_SameSlotIsANoOpRegardlessOfCapacity(t *testing.T) {
	stranded := []domain.CollectionItem{{ID: "b", SlotIndex: 4096, CatalogModelID: "m"}}
	change, err := domain.ResolveSlotChange(stranded, "b", 4096, 4)
	if err != nil || change.Kind != domain.SlotChangeNone {
		t.Fatalf("same slot → %v, %v; want no change", change.Kind, err)
	}
}

func TestResolveSlotChange_CrossTierBetweenReachedTiersIsAllowed(t *testing.T) {
	room := items(0, 1, 2, 3, 4, 5)
	const tierTwoCapacity = 10

	swap, err := domain.ResolveSlotChange(room, room[1].ID, 5, tierTwoCapacity)
	if err != nil || swap.Kind != domain.SlotChangeSwap {
		t.Fatalf("cross-tier swap → %v, %v", swap.Kind, err)
	}
	move, err := domain.ResolveSlotChange(room, room[1].ID, 9, tierTwoCapacity)
	if err != nil || move.Kind != domain.SlotChangeMove {
		t.Fatalf("cross-tier move → %v, %v", move.Kind, err)
	}
	if _, err := domain.ResolveSlotChange(room, room[1].ID, 9, 4); !errors.Is(err, domain.ErrSlotNotAvailable) {
		t.Fatalf("future-tier move → %v, want ErrSlotNotAvailable", err)
	}
}
