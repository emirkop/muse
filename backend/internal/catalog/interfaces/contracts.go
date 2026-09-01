package interfaces

import (
	"encoding/json"
	"time"

	"muse-backend/internal/catalog/application"
	"muse-backend/internal/catalog/domain"
)

type styleResponse struct {
	ID                 string `json:"id"`
	DisplayName        string `json:"display_name"`
	AssetBundleID      string `json:"asset_bundle_id"`
	AssetBundleVersion int    `json:"asset_bundle_version"`
}

type variantResponse struct {
	ID                 string `json:"id"`
	StyleID            string `json:"style_id"`
	DisplayName        string `json:"display_name"`
	AssetBundleID      string `json:"asset_bundle_id"`
	AssetBundleVersion int    `json:"asset_bundle_version"`
}

type sculptureCatalogResponse struct {
	ID                 string `json:"id"`
	DisplayName        string `json:"display_name"`
	AssetBundleID      string `json:"asset_bundle_id"`
	AssetBundleVersion int    `json:"asset_bundle_version"`
}

type sculptureCatalogListResponse struct {
	Sculptures []sculptureCatalogResponse `json:"sculptures"`
}

type collectionCategoryResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	SortOrder   int    `json:"sort_order"`
}

type collectionCategoryListResponse struct {
	CollectionCategories []collectionCategoryResponse `json:"collection_categories"`
}

func newCollectionCategoryResponse(category domain.CollectionCategory) collectionCategoryResponse {
	return collectionCategoryResponse{
		ID:          string(category.ID),
		DisplayName: category.DisplayName,
		SortOrder:   category.SortOrder,
	}
}

type collectionDesignResponse struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"display_name"`
	CategoryID           string `json:"category_id,omitempty"`
	IsDevelopmentFixture bool   `json:"is_development_fixture"`
	AssetBundleID        string `json:"asset_bundle_id"`
	AssetBundleVersion   int    `json:"asset_bundle_version"`
	SortOrder            int    `json:"sort_order"`
	TierCount            int    `json:"tier_count"`
}

type collectionDesignListResponse struct {
	CollectionDesigns []collectionDesignResponse `json:"collection_designs"`
}

func newCollectionDesignResponse(design domain.CollectionDesign) collectionDesignResponse {
	return collectionDesignResponse{
		ID:                   design.ID,
		DisplayName:          design.DisplayName,
		CategoryID:           design.CategoryID,
		IsDevelopmentFixture: design.IsDevelopmentFixture(),
		AssetBundleID:        design.AssetBundle.ID,
		AssetBundleVersion:   design.AssetBundle.Version,
		SortOrder:            design.SortOrder,
		TierCount:            design.HighestTier(),
	}
}

type collectionModelResponse struct {
	ID                   string          `json:"id"`
	BrandID              string          `json:"brand_id"`
	BrandDisplayName     string          `json:"brand_display_name"`
	CategoryID           string          `json:"category_id"`
	DisplayName          string          `json:"display_name"`
	Metadata             json.RawMessage `json:"metadata"`
	HasAsset             bool            `json:"has_asset"`
	AssetBundleID        string          `json:"asset_bundle_id,omitempty"`
	AssetBundleVersion   int             `json:"asset_bundle_version,omitempty"`
	IsDevelopmentFixture bool            `json:"is_development_fixture"`
}

type modelSearchResponse struct {
	Models         []collectionModelResponse `json:"models"`
	NextCursorName string                    `json:"next_cursor_name,omitempty"`
	NextCursorID   string                    `json:"next_cursor_id,omitempty"`
}

type presentationAssetResponse struct {
	ModelID              string `json:"model_id"`
	HasPresentationAsset bool   `json:"has_presentation_asset"`
	AssetBundleID        string `json:"asset_bundle_id,omitempty"`
	AssetBundleVersion   int    `json:"asset_bundle_version,omitempty"`
	IsDevelopmentFixture bool   `json:"is_development_fixture"`
}

type presentationAssetsResponse struct {
	Assets []presentationAssetResponse `json:"assets"`
}

func newPresentationAssetsResponse(mappings []domain.PresentationAssetMapping) presentationAssetsResponse {
	assets := make([]presentationAssetResponse, 0, len(mappings))
	for _, mapping := range mappings {
		asset := presentationAssetResponse{
			ModelID:              string(mapping.ModelID),
			HasPresentationAsset: mapping.IsMapped(),
			IsDevelopmentFixture: mapping.IsDevelopmentFixture,
		}
		if mapping.IsMapped() {
			asset.AssetBundleID = mapping.Bundle.ID
			asset.AssetBundleVersion = mapping.Bundle.Version
		}
		assets = append(assets, asset)
	}
	return presentationAssetsResponse{Assets: assets}
}

func newCollectionModelResponse(model domain.CollectionModel) collectionModelResponse {
	metadata := json.RawMessage(model.Metadata)
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}
	response := collectionModelResponse{
		ID:                   string(model.ID),
		BrandID:              string(model.BrandID),
		BrandDisplayName:     model.BrandDisplayName,
		CategoryID:           string(model.CategoryID),
		DisplayName:          model.DisplayName,
		Metadata:             metadata,
		HasAsset:             model.HasAsset(),
		IsDevelopmentFixture: model.IsDevelopmentFixture(),
	}
	if model.HasAsset() {
		response.AssetBundleID = model.AssetBundle.ID
		response.AssetBundleVersion = model.AssetBundle.Version
	}
	return response
}

func newModelSearchResponse(page domain.ModelSearchPage) modelSearchResponse {
	models := make([]collectionModelResponse, 0, len(page.Models))
	for _, model := range page.Models {
		models = append(models, newCollectionModelResponse(model))
	}
	response := modelSearchResponse{Models: models}
	if next := page.Next; next != nil {
		response.NextCursorName = next.DisplayName
		response.NextCursorID = string(next.ID)
	}
	return response
}

type styleListResponse struct {
	Styles []styleResponse `json:"styles"`
}

type variantListResponse struct {
	Variants []variantResponse `json:"variants"`
}

type musicTrackResponse struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	Attribution     string `json:"attribution"`
	Licensing       string `json:"licensing"`
	DurationSeconds int    `json:"duration_seconds"`
}

type musicTrackListResponse struct {
	Tracks []musicTrackResponse `json:"tracks"`
}

type audioURLResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func newMusicTrackResponse(track domain.MusicTrack) musicTrackResponse {
	return musicTrackResponse{
		ID:              string(track.ID),
		DisplayName:     track.DisplayName,
		Attribution:     track.Attribution,
		Licensing:       string(track.Licensing),
		DurationSeconds: track.DurationSeconds,
	}
}

func newAudioURLResponse(audio application.AudioURL) audioURLResponse {
	return audioURLResponse{URL: audio.URL, ExpiresAt: audio.ExpiresAt}
}

func newStyleResponse(style domain.MuseumStyle) styleResponse {
	return styleResponse{
		ID:                 string(style.ID),
		DisplayName:        style.DisplayName,
		AssetBundleID:      style.AssetBundle.ID,
		AssetBundleVersion: style.AssetBundle.Version,
	}
}

func newSculptureCatalogResponse(sculpture domain.Sculpture) sculptureCatalogResponse {
	return sculptureCatalogResponse{
		ID:                 string(sculpture.ID),
		DisplayName:        sculpture.DisplayName,
		AssetBundleID:      sculpture.AssetBundle.ID,
		AssetBundleVersion: sculpture.AssetBundle.Version,
	}
}

func newVariantResponse(variant domain.RoomVariant) variantResponse {
	return variantResponse{
		ID:                 string(variant.ID),
		StyleID:            string(variant.StyleID),
		DisplayName:        variant.DisplayName,
		AssetBundleID:      variant.AssetBundle.ID,
		AssetBundleVersion: variant.AssetBundle.Version,
	}
}
