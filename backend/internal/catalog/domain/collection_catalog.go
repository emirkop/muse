package domain

type CollectionBrandID string

type CollectionModelID string

type CollectionBrand struct {
	ID             CollectionBrandID
	DisplayName    string
	SortOrder      int
	Classification CatalogContentClassification
}

func (b CollectionBrand) IsDevelopmentFixture() bool {
	return b.Classification == CatalogDevFixture
}

type CatalogContentClassification string

const (
	CatalogDevFixture CatalogContentClassification = "dev_fixture"
	CatalogProduction CatalogContentClassification = "production"
)

type CollectionModel struct {
	ID CollectionModelID

	BrandID    CollectionBrandID
	CategoryID CollectionCategoryID

	DisplayName string

	BrandDisplayName string

	SearchText string

	Metadata []byte

	AssetBundle AssetBundleRef

	Classification CatalogContentClassification
}

func (m CollectionModel) IsDevelopmentFixture() bool {
	return m.Classification == CatalogDevFixture
}

func (m CollectionModel) HasAsset() bool { return m.AssetBundle.ID != "" }

type ModelSearchQuery struct {
	CategoryID CollectionCategoryID

	Terms []string

	Limit int

	Cursor *ModelSearchCursor
}

type ModelSearchCursor struct {
	DisplayName string
	ID          CollectionModelID
}

type ModelSearchPage struct {
	Models []CollectionModel
	Next   *ModelSearchCursor
}
