package domain

type StyleID string

type VariantID string

type SculptureID string

type MuseumStyle struct {
	ID          StyleID
	DisplayName string
	AssetBundle AssetBundleRef
}

type RoomVariant struct {
	ID          VariantID
	StyleID     StyleID
	DisplayName string
	AssetBundle AssetBundleRef
}

type Sculpture struct {
	ID          SculptureID
	DisplayName string
	AssetBundle AssetBundleRef
}

type MusicTrackID string

type MusicLicensing string

const (
	LicensingDevTest  MusicLicensing = "dev_test"
	LicensingLicensed MusicLicensing = "licensed"
)

type MusicTrack struct {
	ID              MusicTrackID
	DisplayName     string
	Attribution     string
	Licensing       MusicLicensing
	StorageKey      string
	ContentType     string
	DurationSeconds int
}

func (t MusicTrack) IsLicensed() bool {
	return t.Licensing == LicensingLicensed
}

type CollectionCategoryID string

type CollectionCategory struct {
	ID          CollectionCategoryID
	DisplayName string
	SortOrder   int
}

type CollectionDesignID string

type CollectionDesignClassification string

const (
	DesignDevFixture CollectionDesignClassification = "dev_fixture"
	DesignProduction CollectionDesignClassification = "production"
)

type CollectionDesign struct {
	ID string

	CategoryID string

	DisplayName    string
	Classification CollectionDesignClassification

	AssetBundle AssetBundleRef

	SortOrder int

	TierCount int
}

func (d CollectionDesign) HighestTier() int {
	if d.TierCount < 1 {
		return 1
	}
	return d.TierCount
}

func (d CollectionDesign) AuthorsTier(tier int) bool {
	return tier >= 1 && tier <= d.HighestTier()
}

func (d CollectionDesign) IsUniversal() bool { return d.CategoryID == "" }

func (d CollectionDesign) IsDevelopmentFixture() bool {
	return d.Classification == DesignDevFixture
}

func (d CollectionDesign) AppliesTo(categoryID string) bool {
	return d.IsUniversal() || d.CategoryID == categoryID
}

type AssetBundleRef struct {
	ID      string
	Version int
}
