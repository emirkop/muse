package domain

type PresentationAssetMapping struct {
	ModelID CollectionModelID

	Bundle AssetBundleRef

	IsDevelopmentFixture bool
}

func (m PresentationAssetMapping) IsMapped() bool { return m.Bundle.ID != "" }

func PresentationAssetMappingFor(model CollectionModel) PresentationAssetMapping {
	return PresentationAssetMapping{
		ModelID:              model.ID,
		Bundle:               model.AssetBundle,
		IsDevelopmentFixture: model.IsDevelopmentFixture(),
	}
}
