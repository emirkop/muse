package infrastructure_test

import (
	"testing"

	"muse-backend/internal/sharing/domain"
	"muse-backend/internal/sharing/infrastructure"
)

func TestRandomCodeGenerator_ProducesPlausibleUniqueCodes(t *testing.T) {
	gen := infrastructure.RandomCodeGenerator{}
	seen := map[domain.Code]bool{}

	for i := 0; i < 1000; i++ {
		code, err := gen.NewCode()
		if err != nil {
			t.Fatal(err)
		}
		if !domain.IsPlausibleCode(string(code)) {
			t.Fatalf("%q is not a plausible code", code)
		}
		if seen[code] {
			t.Fatalf("duplicate code %q in 1000 draws", code)
		}
		seen[code] = true
	}
}
