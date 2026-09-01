package domain_test

import (
	"strings"
	"testing"

	"muse-backend/internal/catalog/domain"
)

func universal(id string) domain.CollectionDesign {
	return domain.CollectionDesign{ID: id, Classification: domain.DesignProduction}
}

func scoped(id, categoryID string) domain.CollectionDesign {
	return domain.CollectionDesign{ID: id, CategoryID: categoryID, Classification: domain.DesignProduction}
}

func TestAppliesTo_UniversalDesignAppliesEverywhere(t *testing.T) {
	design := universal("design_neutral")

	if !design.IsUniversal() {
		t.Fatal("a Design with no category must report itself universal")
	}
	for _, categoryID := range []string{
		"category_watches", "category_hot_wheels", "category_coins", "category_license_plates",
		"category_invented_next_year",
	} {
		if !design.AppliesTo(categoryID) {
			t.Fatalf("universal Design did not apply to %q", categoryID)
		}
	}
}

func TestAppliesTo_ScopedDesignAppliesOnlyToItsCategory(t *testing.T) {
	design := scoped("design_watch_case", "category_watches")

	if design.IsUniversal() {
		t.Fatal("a Design with a category must not report itself universal")
	}
	if !design.AppliesTo("category_watches") {
		t.Fatal("scoped Design did not apply to its own category")
	}
	for _, categoryID := range []string{
		"category_hot_wheels", "category_coins", "category_license_plates", "category_watches_extra", "",
	} {
		if design.AppliesTo(categoryID) {
			t.Fatalf("scoped Design applied to %q, which is not its category", categoryID)
		}
	}
}

func TestAppliesTo_MatchingIsExact(t *testing.T) {
	design := scoped("design_watch_case", "category_watches")

	for _, near := range []string{"CATEGORY_WATCHES", "Category_Watches", " category_watches", "category_watches "} {
		if design.AppliesTo(near) {
			t.Fatalf("scoped Design applied to %q — matching must be exact", near)
		}
	}
}

func TestAppliesTo_EmptyCategoryTakesOnlyUniversalDesigns(t *testing.T) {
	if !universal("design_neutral").AppliesTo("") {
		t.Fatal("a universal Design must apply to a Room with no category")
	}
	if scoped("design_watch_case", "category_watches").AppliesTo("") {
		t.Fatal("a scoped Design must not apply to a Room with no category")
	}
}

func TestClassification_HasNoStateThatCouldPassForProduction(t *testing.T) {
	fixture := domain.CollectionDesign{ID: "x", Classification: domain.DesignDevFixture}
	real := domain.CollectionDesign{ID: "y", Classification: domain.DesignProduction}

	if !fixture.IsDevelopmentFixture() {
		t.Fatal("a dev_fixture Design must report itself as one")
	}
	if real.IsDevelopmentFixture() {
		t.Fatal("a production Design must not report itself as a fixture")
	}
	var zero domain.CollectionDesign
	if zero.IsDevelopmentFixture() {
		t.Fatal("the zero value must not claim to be a fixture")
	}
}

func TestSeedCollectionDesigns_IsOneUniversalLabelledFixture(t *testing.T) {
	designs := domain.SeedCollectionDesigns()

	if len(designs) != 1 {
		t.Fatalf("seeded %d Designs, want exactly 1 — `03` records the design catalog as Open, so anything more is invented", len(designs))
	}
	design := designs[0]

	if design.ID != domain.DesignDevFixtureID {
		t.Fatalf("id = %q, want %q", design.ID, domain.DesignDevFixtureID)
	}
	if !design.IsUniversal() {
		t.Fatalf("the fixture is scoped to %q — requires it be universal so it implies no category affinity", design.CategoryID)
	}
	if !design.IsDevelopmentFixture() {
		t.Fatal("the fixture must be classified dev_fixture, not production")
	}
	if design.DisplayName == "" {
		t.Fatal("the fixture needs a display name that says what it is")
	}
	for _, marker := range []string{"Development", "not a real"} {
		if !strings.Contains(design.DisplayName, marker) {
			t.Fatalf("display name %q does not identify itself as a development placeholder", design.DisplayName)
		}
	}
	if design.AssetBundle.ID == "" || design.AssetBundle.Version <= 0 {
		t.Fatalf("the fixture must reference a real bundle identity, got %+v", design.AssetBundle)
	}
}

func TestBundleKind_CollectionDesignIsPublishable(t *testing.T) {
	if !domain.BundleKindCollectionDesign.IsValid() {
		t.Fatal("collection_design must be a publishable bundle kind")
	}
	for _, kind := range []domain.BundleKind{
		domain.BundleKindMuseumStyle, domain.BundleKindRoomVariant,
		domain.BundleKindSculpture, domain.BundleKindAvatar,
	} {
		if !kind.IsValid() {
			t.Fatalf("existing kind %q stopped being valid", kind)
		}
	}
	if !domain.BundleKindCollectionItem.IsValid() {
		t.Fatal("collection_item must be a publishable bundle kind as of ")
	}
	if domain.BundleKind("not_a_bundle_kind").IsValid() {
		t.Fatal("the set of publishable kinds must stay closed")
	}
}
