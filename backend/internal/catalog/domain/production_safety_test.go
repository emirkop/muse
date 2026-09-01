package domain

import "testing"

func TestClassifications_AgreeOnWhatIsAFixture(t *testing.T) {
	brandFixture := CollectionBrand{Classification: CatalogDevFixture}
	brandReal := CollectionBrand{Classification: CatalogProduction}
	if !brandFixture.IsDevelopmentFixture() || brandReal.IsDevelopmentFixture() {
		t.Error("CollectionBrand misclassifies")
	}

	modelFixture := CollectionModel{Classification: CatalogDevFixture}
	modelReal := CollectionModel{Classification: CatalogProduction}
	if !modelFixture.IsDevelopmentFixture() || modelReal.IsDevelopmentFixture() {
		t.Error("CollectionModel misclassifies")
	}

	if (CollectionModel{}).IsDevelopmentFixture() {
		t.Error("an unset classification must not report as a fixture")
	}
}

func TestMusicTrack_OnlyAConfirmedLicenceIsLicensed(t *testing.T) {
	if !(MusicTrack{Licensing: LicensingLicensed}).IsLicensed() {
		t.Error("a licensed track reports as licensed")
	}
	for _, licensing := range []MusicLicensing{LicensingDevTest, MusicLicensing(""), MusicLicensing("pending")} {
		if (MusicTrack{Licensing: licensing}).IsLicensed() {
			t.Errorf("licensing %q reported as licensed", licensing)
		}
	}
}

func TestSeeds_TheProductionCatalogShipsEmptyWhereNoContentIsConfirmed(t *testing.T) {
	if got := SeedSculptures(); len(got) != 0 {
		t.Errorf("SeedSculptures returned %d entries; — no sculpture is named in `01`–`04`, so seeding one invents product content", len(got))
	}
	if got := SeedMusicTracks(); len(got) != 0 {
		t.Errorf("SeedMusicTracks returned %d entries; — no track may exist until a licence is confirmed", len(got))
	}
}

func TestSeedCollectionCategories_AreExactlyTheFourTheVisionNames(t *testing.T) {
	categories := SeedCollectionCategories()
	if len(categories) != 4 {
		t.Fatalf("%d categories seeded; `01` names exactly four", len(categories))
	}
	names := map[string]bool{}
	previousSort := -1
	for _, category := range categories {
		names[category.DisplayName] = true
		if category.SortOrder <= previousSort {
			t.Errorf("sort order %d does not increase after %d", category.SortOrder, previousSort)
		}
		previousSort = category.SortOrder
		if category.ID == "" {
			t.Error("a category with no id cannot be referenced by a Collection Room")
		}
	}
	for _, expected := range []string{"Watches", "Hot Wheels", "Coins", "License Plates"} {
		if !names[expected] {
			t.Errorf("%q is missing; it is one of the four `01` names", expected)
		}
	}
}

func TestSeededCollectionContent_IsAllClassifiedAsFixture(t *testing.T) {
	for _, brand := range SeedCollectionBrands() {
		if !brand.IsDevelopmentFixture() {
			t.Errorf("seeded brand %q is not classified as a fixture — it would be servable in production", brand.ID)
		}
	}
	models := SeedCollectionModels()
	if len(models) == 0 {
		t.Fatal("the fixture models are what Manual Search is exercised against; none were seeded")
	}
	withoutAsset := 0
	for _, model := range models {
		if !model.IsDevelopmentFixture() {
			t.Errorf("seeded model %q is not classified as a fixture", model.ID)
		}
		if !model.HasAsset() {
			withoutAsset++
		}
	}
	if withoutAsset == 0 {
		t.Error("no fixture Model lacks an asset bundle; 'selection needs no 3D asset' case is unexercised")
	}
}

func TestCollectionModel_HasAssetReadsTheBundleID(t *testing.T) {
	if (CollectionModel{}).HasAsset() {
		t.Error("a Model with no bundle id has no asset")
	}
	if !(CollectionModel{AssetBundle: AssetBundleRef{ID: "dev_fixture_collection_model", Version: 1}}).HasAsset() {
		t.Error("a Model with a bundle id has an asset")
	}
	if (CollectionModel{AssetBundle: AssetBundleRef{Version: 3}}).HasAsset() {
		t.Error("a version alone is not an asset")
	}
}

// MARK: - Tier bounds

func TestCollectionDesign_AuthorsTierIsBoundedAtBothEnds(t *testing.T) {
	design := CollectionDesign{TierCount: 3}
	cases := map[int]bool{0: false, -1: false, 1: true, 2: true, 3: true, 4: false, 100: false}
	for tier, expected := range cases {
		if got := design.AuthorsTier(tier); got != expected {
			t.Errorf("AuthorsTier(%d) = %v, want %v", tier, got, expected)
		}
	}
}

func TestCollectionDesign_HighestTierFloorsAtOne(t *testing.T) {
	for _, count := range []int{0, -1, -100} {
		design := CollectionDesign{TierCount: count}
		if got := design.HighestTier(); got != 1 {
			t.Errorf("TierCount %d → HighestTier() = %d, want the floor of 1", count, got)
		}
		if !design.AuthorsTier(1) {
			t.Errorf("TierCount %d must still author tier 1", count)
		}
		if design.AuthorsTier(2) {
			t.Errorf("TierCount %d must not author tier 2", count)
		}
	}
}

// MARK: - The presentation-asset mapping

func TestPresentationAssetMapping_IsMappedConsultsTheIDNotTheVersion(t *testing.T) {
	cases := []struct {
		name   string
		bundle AssetBundleRef
		mapped bool
	}{
		{"a real reference", AssetBundleRef{ID: "dev_fixture_collection_model", Version: 1}, true},
		{"no id, version 0 (the seed's shape)", AssetBundleRef{Version: 0}, false},
		{"no id, version 1 (the column default)", AssetBundleRef{Version: 1}, false},
		{"nothing at all", AssetBundleRef{}, false},
		{"an id with version 0 is still mapped", AssetBundleRef{ID: "b", Version: 0}, true},
	}
	for _, testCase := range cases {
		mapping := PresentationAssetMapping{Bundle: testCase.bundle}
		if got := mapping.IsMapped(); got != testCase.mapped {
			t.Errorf("%s: IsMapped() = %v, want %v", testCase.name, got, testCase.mapped)
		}
	}
}

func TestPresentationAssetMappingFor_CarriesIdentityBundleAndClassificationOnly(t *testing.T) {
	model := CollectionModel{
		ID:             "dev-fixture:model-watch",
		BrandID:        "dev-fixture:brand-a",
		CategoryID:     "category_watches",
		DisplayName:    "A Watch (NOT a real product)",
		AssetBundle:    AssetBundleRef{ID: "dev_fixture_collection_model", Version: 2},
		Classification: CatalogDevFixture,
	}
	mapping := PresentationAssetMappingFor(model)

	if mapping.ModelID != model.ID {
		t.Errorf("ModelID = %q", mapping.ModelID)
	}
	if mapping.Bundle != model.AssetBundle {
		t.Errorf("Bundle = %+v", mapping.Bundle)
	}
	if !mapping.IsDevelopmentFixture {
		t.Error("the fixture classification must cross into the mapping — it is what production gating reads")
	}
	if !mapping.IsMapped() {
		t.Error("a Model with a bundle is mapped")
	}

	unmapped := PresentationAssetMappingFor(CollectionModel{ID: "m", Classification: CatalogProduction})
	if unmapped.IsMapped() {
		t.Error("a Model with no bundle must project as unmapped")
	}
	if unmapped.IsDevelopmentFixture {
		t.Error("an authored Model must not project as a fixture")
	}
}

// MARK: - Tier-capacity table equality

func TestTierCapacities_EqualIsExactTierForTier(t *testing.T) {
	base := TierCapacities{{Tier: 1, Cumulative: 4}, {Tier: 2, Cumulative: 10}, {Tier: 3, Cumulative: 18}}

	same := TierCapacities{{Tier: 1, Cumulative: 4}, {Tier: 2, Cumulative: 10}, {Tier: 3, Cumulative: 18}}
	if !base.Equal(same) {
		t.Error("identical tables must compare equal — this is what makes an idempotent re-publish possible")
	}

	differences := map[string]TierCapacities{
		"one fewer tier":     {{Tier: 1, Cumulative: 4}, {Tier: 2, Cumulative: 10}},
		"one more tier":      append(append(TierCapacities{}, base...), TierCapacity{Tier: 4, Cumulative: 30}),
		"a changed capacity": {{Tier: 1, Cumulative: 4}, {Tier: 2, Cumulative: 11}, {Tier: 3, Cumulative: 18}},
		"reordered":          {{Tier: 3, Cumulative: 18}, {Tier: 2, Cumulative: 10}, {Tier: 1, Cumulative: 4}},
		"empty":              {},
		"nil":                nil,
	}
	for name, other := range differences {
		if base.Equal(other) {
			t.Errorf("%s compared equal to the base table", name)
		}
	}
	if !(TierCapacities{}).Equal(nil) {
		t.Error("an empty table equals a nil one — both author nothing")
	}
}

// MARK: - Seeded presentation content

func TestSeedVariants_EveryVariantBelongsToASeededStyle(t *testing.T) {
	styles := SeedStyles()
	if len(styles) == 0 {
		t.Fatal("no Styles seeded; every Museum needs one")
	}
	styleIDs := map[StyleID]int{}
	for _, style := range styles {
		if style.ID == "" || style.DisplayName == "" {
			t.Errorf("a Style with no id or name cannot be offered: %+v", style)
		}
		styleIDs[style.ID] = 0
	}

	variants := SeedVariants()
	if len(variants) == 0 {
		t.Fatal("no Variants seeded; a Room could not be created")
	}
	seenVariantIDs := map[VariantID]bool{}
	for _, variant := range variants {
		if _, known := styleIDs[variant.StyleID]; !known {
			t.Errorf("variant %q belongs to unseeded style %q — would refuse it as a mismatch",
				variant.ID, variant.StyleID)
			continue
		}
		styleIDs[variant.StyleID]++
		if seenVariantIDs[variant.ID] {
			t.Errorf("duplicate variant id %q — a Room stores the id, so two Variants would be indistinguishable", variant.ID)
		}
		seenVariantIDs[variant.ID] = true
		if variant.AssetBundle.ID == "" || variant.AssetBundle.Version < 1 {
			t.Errorf("variant %q has no usable asset bundle reference: %+v", variant.ID, variant.AssetBundle)
		}
	}
	for styleID, count := range styleIDs {
		if count == 0 {
			t.Errorf("style %q has no Variants — a Room in it could not be created", styleID)
		}
	}
}
