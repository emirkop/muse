package domain

import "errors"

var (
	ErrMuseumNotFound = errors.New("museum: museum not found")

	ErrMuseumAlreadyExists = errors.New("museum: account already owns a museum")

	ErrRoomNotFound = errors.New("museum: room not found")

	ErrPhotoCapacityReached = errors.New("museum: room already holds the maximum number of photos")

	ErrSculptureCapacityReached = errors.New("museum: room already holds the maximum number of sculptures")

	ErrSlotOccupied = errors.New("museum: slot is already occupied")

	ErrInvalidSlotIndex = errors.New("museum: slot index is outside the permitted range")

	ErrInvalidPrivacy = errors.New("museum: privacy value is not recognised")

	ErrUnknownStyle = errors.New("museum: style is not in the catalog")

	ErrUnknownVariant = errors.New("museum: room variant is not in the catalog")

	ErrVariantStyleMismatch = errors.New("museum: room variant does not belong to the museum's style")

	ErrNotOwner          = errors.New("museum: caller does not own this resource")
	ErrNotVisible        = errors.New("museum: resource is not visible to the caller")
	ErrUnknownMusicTrack = errors.New("museum: unknown music track")

	ErrPhotosUnavailable = errors.New("museum: photo storage is not configured")

	ErrNoPhotosSupplied = errors.New("museum: no photo asset ids supplied")

	ErrDuplicatePhotoAssetIDs = errors.New("museum: duplicate photo asset ids in request")

	ErrPhotoAssetAlreadyAssigned = errors.New("museum: photo asset is already assigned to another room")

	ErrSlotLayoutInconsistent = errors.New("museum: existing photo slots are not contiguous")

	ErrInvalidPhotoOrder = errors.New("museum: photo order is malformed")

	ErrPhotoOrderMismatch = errors.New("museum: photo order does not match the room's photographs")

	ErrTransactionsUnavailable = errors.New("museum: transactional storage is not configured")

	ErrInvalidCaption = errors.New("museum: caption is not valid text")

	ErrCaptionTooLong = errors.New("museum: caption is too long")

	ErrPhotoNotInRoom = errors.New("museum: photo is not in this room")

	ErrUnknownSculpture = errors.New("museum: sculpture is not in the catalog")

	ErrSculptureNotInRoom = errors.New("museum: no sculpture in that slot")

	ErrInvalidSculptureSlot = errors.New("museum: sculpture slot index is outside the permitted range")

	ErrInvalidReplacement = errors.New("museum: replacement asset is missing or is the photograph itself")

	ErrPhotoAssetNotFound    = errors.New("museum: photo asset not found")
	ErrPhotoAssetNotUploaded = errors.New("museum: photo asset has not been uploaded")
	ErrPhotoAssetInvalid     = errors.New("museum: photo asset failed verification")
	ErrPhotoAssetDiscarded   = errors.New("museum: photo asset was discarded")
)

type PhotoAssetError struct {
	AssetID string
	Err     error
}

func (e *PhotoAssetError) Error() string { return e.AssetID + ": " + e.Err.Error() }
func (e *PhotoAssetError) Unwrap() error { return e.Err }
