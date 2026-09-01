package domain

import "errors"

var (
	ErrAssetNotFound = errors.New("media: asset not found")

	ErrAssetNotUploaded = errors.New("media: asset bytes have not been uploaded")

	ErrAssetInvalid = errors.New("media: stored object failed verification")

	ErrAssetDiscarded = errors.New("media: asset has been discarded")

	ErrAssetNotPending = errors.New("media: asset is no longer pending")

	ErrAssetNotCommitted = errors.New("media: asset is not committed")

	ErrDeclarationMismatch = errors.New("media: declaration differs from the existing upload")

	ErrInvalidClientUploadID  = errors.New("media: client_upload_id is required")
	ErrUnsupportedContentType = errors.New("media: only image/jpeg is accepted")
	ErrPhotoTooLarge          = errors.New("media: photo exceeds the size limit")
	ErrPhotoDimensions        = errors.New("media: photo dimensions are outside the accepted range")
	ErrInvalidChecksum        = errors.New("media: checksum_sha256 must be 64 lowercase hex characters")
)

type AssetError struct {
	AssetID AssetID
	Err     error
}

func (e *AssetError) Error() string { return string(e.AssetID) + ": " + e.Err.Error() }
func (e *AssetError) Unwrap() error { return e.Err }
