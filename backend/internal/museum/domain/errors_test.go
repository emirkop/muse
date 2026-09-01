package domain

import (
	"errors"
	"testing"
)

func TestPhotoAssetError_CarriesTheAssetAndUnwrapsToTheSentinel(t *testing.T) {
	wrapped := &PhotoAssetError{AssetID: "asset-7", Err: ErrPhotoNotInRoom}

	if got := wrapped.Error(); got != "asset-7: "+ErrPhotoNotInRoom.Error() {
		t.Errorf("Error() = %q", got)
	}
	if !errors.Is(wrapped, ErrPhotoNotInRoom) {
		t.Fatal("errors.Is must see the wrapped sentinel, or a batch refusal becomes a 500")
	}
	var recovered *PhotoAssetError
	if !errors.As(error(wrapped), &recovered) || recovered.AssetID != "asset-7" {
		t.Fatalf("errors.As did not recover the asset id: %+v", recovered)
	}
	if errors.Is(wrapped, ErrRoomNotFound) {
		t.Error("the wrapper must not match an unrelated sentinel")
	}
}
