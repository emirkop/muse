package domain

import "unicode/utf8"

const InterimMaximumModelReferenceBytes = 200

func ValidateModelReference(catalogModelID string) error {
	if catalogModelID == "" {
		return ErrModelReferenceRequired
	}
	if !utf8.ValidString(catalogModelID) || len(catalogModelID) > InterimMaximumModelReferenceBytes {
		return ErrInvalidModelReference
	}
	return nil
}

func ValidateSlotIndex(slotIndex int) error {
	if slotIndex < 0 {
		return ErrInvalidSlotIndex
	}
	return nil
}

func LowestFreeSlotIndex(items []CollectionItem) int {
	occupied := make(map[int]bool, len(items))
	for _, item := range items {
		occupied[item.SlotIndex] = true
	}
	for candidate := 0; ; candidate++ {
		if !occupied[candidate] {
			return candidate
		}
	}
}

func ItemAtSlot(items []CollectionItem, slotIndex int) (CollectionItem, bool) {
	for _, item := range items {
		if item.SlotIndex == slotIndex {
			return item, true
		}
	}
	return CollectionItem{}, false
}

func ItemByID(items []CollectionItem, id CollectionItemID) (CollectionItem, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return CollectionItem{}, false
}

type SlotChangeKind int

const (
	SlotChangeNone SlotChangeKind = iota
	SlotChangeMove
	SlotChangeSwap
)

type SlotChange struct {
	Kind            SlotChangeKind
	Item            CollectionItem
	Displaced       CollectionItem
	TargetSlotIndex int
}

func ResolveSlotChange(
	items []CollectionItem,
	itemID CollectionItemID,
	targetSlotIndex int,
	reachedCapacity int,
) (SlotChange, error) {
	if err := ValidateSlotIndex(targetSlotIndex); err != nil {
		return SlotChange{}, err
	}
	item, found := ItemByID(items, itemID)
	if !found {
		return SlotChange{}, ErrItemNotInRoom
	}
	if item.SlotIndex == targetSlotIndex {
		return SlotChange{Kind: SlotChangeNone, Item: item, TargetSlotIndex: targetSlotIndex}, nil
	}
	if targetSlotIndex >= reachedCapacity {
		return SlotChange{}, ErrSlotNotAvailable
	}
	if occupant, taken := ItemAtSlot(items, targetSlotIndex); taken {
		return SlotChange{
			Kind:            SlotChangeSwap,
			Item:            item,
			Displaced:       occupant,
			TargetSlotIndex: targetSlotIndex,
		}, nil
	}
	return SlotChange{Kind: SlotChangeMove, Item: item, TargetSlotIndex: targetSlotIndex}, nil
}
