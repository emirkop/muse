package domain

type Tier int

const BaseTier Tier = 1

type TierCapacities []int

func (c TierCapacities) Validate() error {
	if len(c) == 0 {
		return ErrNoTierCapacities
	}
	previous := 0
	for _, capacity := range c {
		if capacity <= previous {
			return ErrTierCapacitiesNotIncreasing
		}
		previous = capacity
	}
	return nil
}

func (c TierCapacities) HighestTier() Tier { return BaseTier + Tier(len(c)) - 1 }

func (c TierCapacities) CapacityOf(t Tier) (int, error) {
	if err := c.Validate(); err != nil {
		return 0, err
	}
	if t < BaseTier || t > c.HighestTier() {
		return 0, ErrUnknownTier
	}
	return c[t-BaseTier], nil
}

func RequiredTier(itemCount int, capacities TierCapacities) (Tier, error) {
	if err := capacities.Validate(); err != nil {
		return 0, err
	}
	if itemCount < 0 {
		return 0, ErrNegativeItemCount
	}
	for index, capacity := range capacities {
		if itemCount <= capacity {
			return BaseTier + Tier(index), nil
		}
	}
	return 0, ErrTierCapacityExhausted
}

func RatchetedTier(current, requested Tier) Tier {
	if requested > current {
		return requested
	}
	return current
}

func ValidateTierRequest(requested Tier, authoredTiers int) error {
	if requested < BaseTier {
		return ErrInvalidTier
	}
	if authoredTiers < 1 || int(requested) > authoredTiers {
		return ErrTierNotAuthored
	}
	return nil
}
