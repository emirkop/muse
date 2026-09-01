package domain

import (
	"encoding/json"
	"fmt"
)

type TierCapacity struct {
	Tier       int
	Cumulative int
}

type TierCapacities []TierCapacity

func (c TierCapacities) Validate() error {
	if len(c) == 0 {
		return fmt.Errorf("%w: design layout declares no tiers", ErrBundleInvalid)
	}
	previous := 0
	for index, tier := range c {
		if tier.Tier != index+1 {
			return fmt.Errorf("%w: design layout tiers must be 1, 2, 3… with no gaps or duplicates; position %d declares tier %d",
				ErrBundleInvalid, index+1, tier.Tier)
		}
		if tier.Cumulative <= previous {
			return fmt.Errorf("%w: design layout tier %d cumulative capacity %d must exceed tier %d's %d",
				ErrBundleInvalid, tier.Tier, tier.Cumulative, tier.Tier-1, previous)
		}
		previous = tier.Cumulative
	}
	return nil
}

func (c TierCapacities) SlotCapacityAt(tier int) (int, bool) {
	if tier < 1 || tier > len(c) {
		return 0, false
	}
	return c[tier-1].Cumulative, true
}

func (c TierCapacities) Equal(other TierCapacities) bool {
	if len(c) != len(other) {
		return false
	}
	for index := range c {
		if c[index] != other[index] {
			return false
		}
	}
	return true
}

const maxDesignLayoutBytes = 8 << 20

const MaxDesignLayoutBytes = maxDesignLayoutBytes

const designLayoutFormatVersion = 1

func ParseCollectionDesignTierCapacities(layout []byte) (designID string, table TierCapacities, err error) {
	if len(layout) > maxDesignLayoutBytes {
		return "", nil, fmt.Errorf("%w: design layout is %d bytes, over the %d-byte bound",
			ErrBundleInvalid, len(layout), maxDesignLayoutBytes)
	}
	var file designLayoutFile
	if err := json.Unmarshal(layout, &file); err != nil {
		return "", nil, fmt.Errorf("%w: design layout is not valid JSON: %v", ErrBundleInvalid, err)
	}
	if file.FormatVersion != designLayoutFormatVersion {
		return "", nil, fmt.Errorf("%w: design layout format_version %d is not %d",
			ErrBundleInvalid, file.FormatVersion, designLayoutFormatVersion)
	}
	if file.DesignID == "" {
		return "", nil, fmt.Errorf("%w: design layout names no design_id", ErrBundleInvalid)
	}
	if len(file.Tiers) == 0 {
		return "", nil, fmt.Errorf("%w: design layout declares no tiers", ErrBundleInvalid)
	}

	table = make(TierCapacities, 0, len(file.Tiers))
	expectedSlot := 0
	previousCapacity := 0
	for index, tier := range file.Tiers {
		if tier.Tier == nil || tier.CumulativeCapacity == nil {
			return "", nil, fmt.Errorf("%w: design layout tier at position %d is missing `tier` or `cumulative_capacity`",
				ErrBundleInvalid, index+1)
		}
		if *tier.CumulativeCapacity <= 0 {
			return "", nil, fmt.Errorf("%w: design layout tier %d has non-positive cumulative capacity %d",
				ErrBundleInvalid, *tier.Tier, *tier.CumulativeCapacity)
		}
		added := *tier.CumulativeCapacity - previousCapacity
		if len(tier.ItemTransforms) != added {
			return "", nil, fmt.Errorf("%w: design layout tier %d adds %d capacity but supplies %d slots",
				ErrBundleInvalid, *tier.Tier, added, len(tier.ItemTransforms))
		}
		for _, slot := range tier.ItemTransforms {
			if slot.SlotIndex == nil || *slot.SlotIndex != expectedSlot {
				return "", nil, fmt.Errorf("%w: design layout slot indices must be contiguous from 0; tier %d expected slot %d",
					ErrBundleInvalid, *tier.Tier, expectedSlot)
			}
			expectedSlot++
		}
		table = append(table, TierCapacity{Tier: *tier.Tier, Cumulative: *tier.CumulativeCapacity})
		previousCapacity = *tier.CumulativeCapacity
	}
	if err := table.Validate(); err != nil {
		return "", nil, err
	}
	return file.DesignID, table, nil
}

type designLayoutFile struct {
	FormatVersion int                `json:"format_version"`
	DesignID      string             `json:"design_id"`
	Tiers         []designLayoutTier `json:"tiers"`
}

type designLayoutTier struct {
	Tier               *int               `json:"tier"`
	CumulativeCapacity *int               `json:"cumulative_capacity"`
	ItemTransforms     []designLayoutSlot `json:"item_transforms"`
}

type designLayoutSlot struct {
	SlotIndex *int `json:"slot_index"`
}
