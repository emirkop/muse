package domain

import (
	"errors"
	"strings"
	"testing"
)

const validChecksum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func declaration() PhotoDeclaration {
	return PhotoDeclaration{
		ClientUploadID: "picked-photo-1",
		ContentType:    PhotoContentType,
		ByteSize:       512 * 1024,
		PixelWidth:     2048,
		PixelHeight:    1536,
		ChecksumSHA256: validChecksum,
	}
}

func TestPhotoStorageKey_IsThePerAccountPrefixedShape(t *testing.T) {
	key := PhotoStorageKey("11111111-2222-4333-8444-555555555555", AssetID("aaaa-bbbb"))
	if key != "photos/11111111-2222-4333-8444-555555555555/aaaa-bbbb" {
		t.Fatalf("key = %q", key)
	}
	if strings.HasPrefix(key, "bundles/") {
		t.Fatal("a photo key must not be reachable through the public bundle prefix")
	}
}

func TestPhotoDeclaration_AcceptsAnOrdinaryPhotograph(t *testing.T) {
	if err := declaration().Validate(); err != nil {
		t.Fatalf("an ordinary declaration was refused: %v", err)
	}
}

func TestPhotoDeclaration_DimensionBoundariesAreExact(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		accepted      bool
	}{
		{"long edge exactly at the ceiling", MaxPhotoLongEdge, 1000, true},
		{"long edge one over", MaxPhotoLongEdge + 1, 1000, false},
		{"portrait long edge at the ceiling", 1000, MaxPhotoLongEdge, true},
		{"portrait long edge one over", 1000, MaxPhotoLongEdge + 1, false},
		{"short edge exactly at the floor", 3000, MinPhotoShortEdge, true},
		{"short edge one under", 3000, MinPhotoShortEdge - 1, false},
		{"square at the floor", MinPhotoShortEdge, MinPhotoShortEdge, true},
		{"zero width", 0, 1000, false},
		{"zero height", 1000, 0, false},
		{"negative", -1, -1, false},
	}
	for _, testCase := range cases {
		photo := declaration()
		photo.PixelWidth, photo.PixelHeight = testCase.width, testCase.height
		err := photo.Validate()
		if testCase.accepted && err != nil {
			t.Errorf("%s (%dx%d): %v", testCase.name, testCase.width, testCase.height, err)
		}
		if !testCase.accepted {
			if err == nil {
				t.Errorf("%s (%dx%d): expected a refusal", testCase.name, testCase.width, testCase.height)
			} else if !errors.Is(err, ErrPhotoDimensions) {
				t.Errorf("%s: refused with %v, want ErrPhotoDimensions", testCase.name, err)
			}
		}
	}
}

func TestPhotoDeclaration_IsOrientationIndependent(t *testing.T) {
	for _, pair := range [][2]int{{3072, 320}, {2048, 1536}, {3073, 400}, {2000, 100}} {
		landscape, portrait := declaration(), declaration()
		landscape.PixelWidth, landscape.PixelHeight = pair[0], pair[1]
		portrait.PixelWidth, portrait.PixelHeight = pair[1], pair[0]

		landscapeErr, portraitErr := landscape.Validate(), portrait.Validate()
		if (landscapeErr == nil) != (portraitErr == nil) {
			t.Errorf("%dx%d and %dx%d were judged differently: %v vs %v",
				pair[0], pair[1], pair[1], pair[0], landscapeErr, portraitErr)
		}
	}
}

func TestPhotoDeclaration_SizeAndTypeBoundaries(t *testing.T) {
	atLimit := declaration()
	atLimit.ByteSize = MaxPhotoBytes
	if err := atLimit.Validate(); err != nil {
		t.Errorf("a photograph exactly at the limit must be accepted: %v", err)
	}

	overLimit := declaration()
	overLimit.ByteSize = MaxPhotoBytes + 1
	if err := overLimit.Validate(); !errors.Is(err, ErrPhotoTooLarge) {
		t.Errorf("one byte over the limit → %v, want ErrPhotoTooLarge", err)
	}
	for _, size := range []int64{0, -1} {
		empty := declaration()
		empty.ByteSize = size
		if err := empty.Validate(); !errors.Is(err, ErrPhotoTooLarge) {
			t.Errorf("size %d → %v; a non-positive size is not a photograph", size, err)
		}
	}

	for _, contentType := range []string{"image/png", "image/heic", "image/jpg", "IMAGE/JPEG", ""} {
		wrong := declaration()
		wrong.ContentType = contentType
		if err := wrong.Validate(); !errors.Is(err, ErrUnsupportedContentType) {
			t.Errorf("content type %q → %v, want ErrUnsupportedContentType", contentType, err)
		}
	}
}

func TestPhotoDeclaration_ChecksumMustBeLowercaseHex64(t *testing.T) {
	cases := map[string]bool{
		validChecksum:                  true,
		strings.ToUpper(validChecksum): false,
		validChecksum[:63]:             false,
		validChecksum + "0":            false,
		"":                             false,
		strings.Repeat("g", 64):        false,
		strings.Repeat("0", 63) + " ":  false,
	}
	for checksum, accepted := range cases {
		photo := declaration()
		photo.ChecksumSHA256 = checksum
		err := photo.Validate()
		if accepted && err != nil {
			t.Errorf("checksum %q: %v", checksum, err)
		}
		if !accepted && !errors.Is(err, ErrInvalidChecksum) {
			t.Errorf("checksum %q → %v, want ErrInvalidChecksum", checksum, err)
		}
	}
}

func TestPhotoDeclaration_ClientUploadIDBounds(t *testing.T) {
	for _, id := range []string{"", "   ", strings.Repeat("x", 129)} {
		photo := declaration()
		photo.ClientUploadID = id
		if err := photo.Validate(); !errors.Is(err, ErrInvalidClientUploadID) {
			t.Errorf("client_upload_id %q → %v, want ErrInvalidClientUploadID", id, err)
		}
	}
	photo := declaration()
	photo.ClientUploadID = strings.Repeat("x", 128)
	if err := photo.Validate(); err != nil {
		t.Errorf("a 128-character upload id must be accepted: %v", err)
	}
}

// MARK: - Verification: what was actually stored

func asset() Asset {
	return Asset{
		ID:             AssetID("asset-1"),
		ContentType:    PhotoContentType,
		ByteSize:       512 * 1024,
		PixelWidth:     2048,
		PixelHeight:    1536,
		ChecksumSHA256: validChecksum,
	}
}

func stored() StoredObject {
	return StoredObject{
		ByteSize:       512 * 1024,
		ContentType:    PhotoContentType,
		Format:         "jpeg",
		PixelWidth:     2048,
		PixelHeight:    1536,
		ChecksumSHA256: validChecksum,
	}
}

func TestVerifyStored_AcceptsWhatMatchesTheDeclaration(t *testing.T) {
	if err := asset().VerifyStored(stored()); err != nil {
		t.Fatalf("matching bytes were refused: %v", err)
	}
}

func TestVerifyStored_RefusesEveryMismatchIndividually(t *testing.T) {
	mutations := map[string]func(*StoredObject){
		"a different size":          func(o *StoredObject) { o.ByteSize = 1024 },
		"a different content type":  func(o *StoredObject) { o.ContentType = "image/png" },
		"bytes that are not a JPEG": func(o *StoredObject) { o.Format = "png" },
		"a different width":         func(o *StoredObject) { o.PixelWidth = 1024 },
		"a different height":        func(o *StoredObject) { o.PixelHeight = 1024 },
		"a different checksum":      func(o *StoredObject) { o.ChecksumSHA256 = strings.Repeat("f", 64) },
		"transposed dimensions":     func(o *StoredObject) { o.PixelWidth, o.PixelHeight = o.PixelHeight, o.PixelWidth },
	}
	for name, mutate := range mutations {
		object := stored()
		mutate(&object)
		err := asset().VerifyStored(object)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if !errors.Is(err, ErrAssetInvalid) {
			t.Errorf("%s → %v, want ErrAssetInvalid", name, err)
		}
	}
}

func TestVerifyStored_ToleratesAMissingContentTypeButNeverAWrongFormat(t *testing.T) {
	noContentType := stored()
	noContentType.ContentType = ""
	if err := asset().VerifyStored(noContentType); err != nil {
		t.Errorf("a storage backend that reports no content type must not fail verification: %v", err)
	}

	noContentTypeWrongBytes := stored()
	noContentTypeWrongBytes.ContentType = ""
	noContentTypeWrongBytes.Format = "png"
	if err := asset().VerifyStored(noContentTypeWrongBytes); !errors.Is(err, ErrAssetInvalid) {
		t.Error("PNG bytes must be refused even when no content type was reported")
	}
}

func TestVerifyStored_ReAppliesThePolicyToObservedValues(t *testing.T) {
	oversized := asset()
	oversized.PixelWidth, oversized.PixelHeight = MaxPhotoLongEdge+500, 1000
	object := stored()
	object.PixelWidth, object.PixelHeight = oversized.PixelWidth, oversized.PixelHeight

	if err := oversized.VerifyStored(object); !errors.Is(err, ErrAssetInvalid) {
		t.Fatalf("stored dimensions outside the policy must be refused even when they match the declaration: %v", err)
	}

	tooBig := asset()
	tooBig.ByteSize = MaxPhotoBytes + 1
	bigObject := stored()
	bigObject.ByteSize = tooBig.ByteSize
	if err := tooBig.VerifyStored(bigObject); !errors.Is(err, ErrAssetInvalid) {
		t.Fatalf("a stored object over the size limit must be refused: %v", err)
	}
}

func TestAssetError_CarriesTheAssetAndUnwrapsToTheSentinel(t *testing.T) {
	wrapped := &AssetError{AssetID: AssetID("asset-3"), Err: ErrAssetNotUploaded}

	if got := wrapped.Error(); got != "asset-3: "+ErrAssetNotUploaded.Error() {
		t.Errorf("Error() = %q", got)
	}
	if !errors.Is(wrapped, ErrAssetNotUploaded) {
		t.Fatal("errors.Is must see the wrapped sentinel, or a per-photo refusal becomes a 500")
	}
	var recovered *AssetError
	if !errors.As(error(wrapped), &recovered) || recovered.AssetID != AssetID("asset-3") {
		t.Fatalf("errors.As did not recover the asset id: %+v", recovered)
	}
	if errors.Is(wrapped, ErrAssetDiscarded) {
		t.Error("the wrapper must not match an unrelated sentinel")
	}
}
