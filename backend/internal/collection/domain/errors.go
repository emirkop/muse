package domain

import "errors"

var (
	ErrCollectionRoomNotFound = errors.New("collection: collection room not found")

	ErrNotOwner = errors.New("collection: caller does not own this collection room")

	ErrEmptyPatch = errors.New("collection: update request changes nothing")

	ErrNameRequired = errors.New("collection: collection room name is required")

	ErrNameTooLong = errors.New("collection: collection room name is too long")

	ErrInvalidName = errors.New("collection: collection room name is not valid text")

	ErrInvalidCategoryReference = errors.New("collection: category reference is malformed")

	ErrCategoryRequired = errors.New("collection: a collection category is required")

	ErrUnknownCategory = errors.New("collection: category is not in the catalog")

	ErrInvalidDesignReference = errors.New("collection: design reference is malformed")

	ErrUnknownMusicTrack = errors.New("collection: music track is not in the catalog")

	ErrDesignNotApplicable = errors.New("collection: design is not applicable to this collection room")

	ErrNoTierCapacities = errors.New("collection: design declares no tier capacities")

	ErrTierCapacitiesNotIncreasing = errors.New("collection: tier capacities are not strictly increasing")

	ErrUnknownTier = errors.New("collection: tier is outside the design's authored range")

	ErrNegativeItemCount = errors.New("collection: item count cannot be negative")

	ErrTierCapacityExhausted = errors.New("collection: item count exceeds the design's highest authored tier")

	ErrInvalidTier = errors.New("collection: tier must be at least the base tier")

	ErrTierNotAuthored = errors.New("collection: design does not author that tier")

	ErrDesignRequiredForTier = errors.New("collection: a design must be selected before the room can expand")

	ErrModelReferenceRequired = errors.New("collection: a catalog model is required")

	ErrInvalidModelReference = errors.New("collection: catalog model reference is malformed")

	ErrModelNotAvailable = errors.New("collection: catalog model is not available for this collection room")

	ErrItemNotInRoom = errors.New("collection: item is not in this collection room")

	ErrItemSlotTaken = errors.New("collection: that display slot is already taken")

	ErrSlotNotAvailable = errors.New("collection: that slot is not available at the room's current tier")

	ErrItemCapacityReached = errors.New("collection: the account's item capacity is reached")

	ErrTierCapacityReached = errors.New("collection: the room's current tier has no free slot — expand it first")

	ErrDesignRequiredForItems = errors.New("collection: a design must be selected before items can be placed or arranged")

	ErrDesignLayoutUnavailable = errors.New("collection: the room's design has no published slot table for its current tier")

	ErrInvalidSlotIndex = errors.New("collection: slot index must not be negative")

	ErrTransactionsUnavailable = errors.New("collection: transactional storage is not configured")

	ErrStorageUnavailable = errors.New("collection: storage is not configured")
)
