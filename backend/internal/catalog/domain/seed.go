package domain

var (
	StyleModern  StyleID = "style_modern"
	StyleNatural StyleID = "style_natural"
	StyleGothic  StyleID = "style_gothic"
)

func SeedStyles() []MuseumStyle {
	return []MuseumStyle{
		{ID: StyleModern, DisplayName: "Modern", AssetBundle: AssetBundleRef{ID: "bundle_style_modern", Version: 1}},
		{ID: StyleNatural, DisplayName: "Natural", AssetBundle: AssetBundleRef{ID: "bundle_style_natural", Version: 1}},
		{ID: StyleGothic, DisplayName: "Gothic", AssetBundle: AssetBundleRef{ID: "bundle_style_gothic", Version: 1}},
	}
}

func SeedSculptures() []Sculpture {
	return nil
}

func SeedMusicTracks() []MusicTrack {
	return nil
}

var (
	CategoryWatches       CollectionCategoryID = "category_watches"
	CategoryHotWheels     CollectionCategoryID = "category_hot_wheels"
	CategoryCoins         CollectionCategoryID = "category_coins"
	CategoryLicensePlates CollectionCategoryID = "category_license_plates"
)

func SeedCollectionCategories() []CollectionCategory {
	return []CollectionCategory{
		{ID: CategoryWatches, DisplayName: "Watches", SortOrder: 10},
		{ID: CategoryHotWheels, DisplayName: "Hot Wheels", SortOrder: 20},
		{ID: CategoryCoins, DisplayName: "Coins", SortOrder: 30},
		{ID: CategoryLicensePlates, DisplayName: "License Plates", SortOrder: 40},
	}
}

const DesignDevFixtureID = "dev-fixture:collection-design"

func SeedCollectionDesigns() []CollectionDesign {
	return []CollectionDesign{
		{
			ID:             DesignDevFixtureID,
			CategoryID:     "",
			DisplayName:    "Development Fixture (not a real design)",
			Classification: DesignDevFixture,
			AssetBundle:    AssetBundleRef{ID: "dev_fixture_collection_design", Version: 1},
			SortOrder:      1000,
			TierCount:      3,
		},
	}
}

const (
	BrandDevFixtureA = "dev-fixture:brand-a"
	BrandDevFixtureB = "dev-fixture:brand-b"
)

func SeedCollectionBrands() []CollectionBrand {
	return []CollectionBrand{
		{ID: BrandDevFixtureA, DisplayName: "Devco (development fixture)", SortOrder: 9000, Classification: CatalogDevFixture},
		{ID: BrandDevFixtureB, DisplayName: "Testmark (development fixture)", SortOrder: 9010, Classification: CatalogDevFixture},
	}
}

func SeedCollectionModels() []CollectionModel {
	return []CollectionModel{
		{
			ID: "dev-fixture:model-chrono-one", BrandID: BrandDevFixtureA, CategoryID: CategoryWatches,
			DisplayName:    "Devco Chrono One (development fixture)",
			SearchText:     "devco chrono one development fixture watch",
			Metadata:       []byte(`{"_comment":"development fixture metadata - not a real product specification"}`),
			AssetBundle:    AssetBundleRef{ID: "dev_fixture_collection_model", Version: 1},
			Classification: CatalogDevFixture,
		},
		{
			ID: "dev-fixture:model-chrono-two", BrandID: BrandDevFixtureA, CategoryID: CategoryWatches,
			DisplayName:    "Devco Chrono Two (development fixture)",
			SearchText:     "devco chrono two development fixture watch",
			Metadata:       []byte(`{"_comment":"development fixture metadata - not a real product specification"}`),
			Classification: CatalogDevFixture,
		},
		{
			ID: "dev-fixture:model-diver", BrandID: BrandDevFixtureB, CategoryID: CategoryWatches,
			DisplayName:    "Testmark Diver (development fixture)",
			SearchText:     "testmark diver development fixture watch",
			Metadata:       []byte(`{"_comment":"development fixture metadata - not a real product specification"}`),
			Classification: CatalogDevFixture,
		},
		{
			ID: "dev-fixture:model-racer", BrandID: BrandDevFixtureA, CategoryID: CategoryHotWheels,
			DisplayName:    "Devco Racer (development fixture)",
			SearchText:     "devco racer development fixture car diecast",
			Metadata:       []byte(`{"_comment":"development fixture metadata - not a real product specification"}`),
			Classification: CatalogDevFixture,
		},
	}
}

func SeedVariants() []RoomVariant {
	var variants []RoomVariant
	for _, style := range SeedStyles() {
		for index, name := range []string{"Hall", "Gallery"} {
			variants = append(variants, RoomVariant{
				ID:          VariantID(string(style.ID) + "_variant_" + name),
				StyleID:     style.ID,
				DisplayName: name,
				AssetBundle: AssetBundleRef{
					ID:      "bundle_" + string(style.ID) + "_" + name,
					Version: 1,
				},
			})
			_ = index
		}
	}
	return variants
}
