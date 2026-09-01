package domain_test

import (
	"errors"
	"strings"
	"testing"

	"muse-backend/internal/collection/domain"
)

func TestValidateName(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantError error
	}{
		{"ordinary", "Watches", nil},
		{"non-ascii", "Saat Koleksiyonum", nil},
		{"emoji", "⌚️ Watches", nil},
		{"at the interim bound", strings.Repeat("a", domain.InterimMaximumNameBytes), nil},
		{"empty", "", domain.ErrNameRequired},
		{"whitespace only", "   \t\n ", domain.ErrNameRequired},
		{"one byte past the bound", strings.Repeat("a", domain.InterimMaximumNameBytes+1), domain.ErrNameTooLong},
		{"invalid utf-8", "\xff\xfe", domain.ErrInvalidName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateName(tc.input)
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("ValidateName(%q) = %v, want %v", tc.input, err, tc.wantError)
			}
		})
	}
}

func TestNamingRules_AreLabelledInterimAndPermissive(t *testing.T) {
	if domain.InterimEnforcesNameUniqueness {
		t.Fatal("uniqueness is being enforced — has not decided that; `02` says duplicates are allowed")
	}
	if domain.InterimAppliesProfanityFilter {
		t.Fatal("a profanity filter is being applied — that is unowned content policy")
	}
}

func TestValidateReferences_FormatOnly(t *testing.T) {
	if err := domain.ValidateCategoryReference(""); err != nil {
		t.Fatalf("empty category: %v", err)
	}
	if err := domain.ValidateDesignReference(""); err != nil {
		t.Fatalf("empty design: %v", err)
	}

	for _, reference := range []string{"watches", "hot-wheels", "anything-at-all", "a"} {
		if err := domain.ValidateCategoryReference(reference); err != nil {
			t.Fatalf("ValidateCategoryReference(%q) = %v, want nil", reference, err)
		}
	}

	long := strings.Repeat("x", domain.InterimMaximumReferenceBytes+1)
	if err := domain.ValidateCategoryReference(long); !errors.Is(err, domain.ErrInvalidCategoryReference) {
		t.Fatalf("over-long category = %v, want ErrInvalidCategoryReference", err)
	}
	if err := domain.ValidateDesignReference(long); !errors.Is(err, domain.ErrInvalidDesignReference) {
		t.Fatalf("over-long design = %v, want ErrInvalidDesignReference", err)
	}
	if err := domain.ValidateCategoryReference("\xff"); !errors.Is(err, domain.ErrInvalidCategoryReference) {
		t.Fatalf("invalid utf-8 category = %v, want ErrInvalidCategoryReference", err)
	}
	if err := domain.ValidateDesignReference("\xff"); !errors.Is(err, domain.ErrInvalidDesignReference) {
		t.Fatalf("invalid utf-8 design = %v, want ErrInvalidDesignReference", err)
	}
}
