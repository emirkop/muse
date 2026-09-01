package interfaces

import (
	"muse-backend/internal/catalog/application"
)

type manifestFileResponse struct {
	AssetID        string `json:"asset_id"`
	Role           string `json:"role"`
	URL            string `json:"url"`
	ContentType    string `json:"content_type"`
	ByteSize       int64  `json:"byte_size"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

type manifestDependencyResponse struct {
	BundleID string `json:"bundle_id"`
	Version  int    `json:"version"`
}

type bundleManifestResponse struct {
	BundleID      string                       `json:"bundle_id"`
	Version       int                          `json:"version"`
	Kind          string                       `json:"kind"`
	Format        string                       `json:"format"`
	MinAppVersion int                          `json:"min_app_version"`
	Files         []manifestFileResponse       `json:"files"`
	Dependencies  []manifestDependencyResponse `json:"dependencies"`
}

func newBundleManifestResponse(manifest application.BundleManifest) bundleManifestResponse {
	files := make([]manifestFileResponse, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		files = append(files, manifestFileResponse{
			AssetID:        file.AssetID,
			Role:           string(file.Role),
			URL:            file.URL,
			ContentType:    file.ContentType,
			ByteSize:       file.ByteSize,
			ChecksumSHA256: file.ChecksumSHA256,
		})
	}
	dependencies := make([]manifestDependencyResponse, 0, len(manifest.Dependencies))
	for _, dependency := range manifest.Dependencies {
		dependencies = append(dependencies, manifestDependencyResponse{
			BundleID: dependency.BundleID,
			Version:  dependency.Version,
		})
	}
	return bundleManifestResponse{
		BundleID:      manifest.BundleID,
		Version:       manifest.Version,
		Kind:          string(manifest.Kind),
		Format:        manifest.Format,
		MinAppVersion: manifest.MinAppVersion,
		Files:         files,
		Dependencies:  dependencies,
	}
}
