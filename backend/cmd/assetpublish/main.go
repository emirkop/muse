package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	catalogapp "muse-backend/internal/catalog/application"
	catalogdomain "muse-backend/internal/catalog/domain"
	catalinfra "muse-backend/internal/catalog/infrastructure"
	mediainfra "muse-backend/internal/media/infrastructure"
	"muse-backend/internal/platform/config"
	"muse-backend/internal/platform/database"
	"muse-backend/internal/platform/objectstore"
)

type descriptor struct {
	BundleID      string           `json:"bundle_id"`
	Version       int              `json:"version"`
	Kind          string           `json:"kind"`
	Format        string           `json:"format"`
	MinAppVersion int              `json:"min_app_version"`
	Files         []fileDescriptor `json:"files"`
	Dependencies  []dependencyRef  `json:"dependencies"`
}

type fileDescriptor struct {
	AssetID     string `json:"asset_id"`
	Role        string `json:"role"`
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
}

type dependencyRef struct {
	BundleID string `json:"bundle_id"`
	Version  int    `json:"version"`
}

func main() {
	source := flag.String("source", "", "directory containing bundle.json and the files it names")
	dryRun := flag.Bool("dry-run", false, "hash and validate everything, upload and register nothing")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if *source == "" {
		logger.Error("-source is required")
		os.Exit(2)
	}

	if err := run(context.Background(), logger, *source, *dryRun); err != nil {
		logger.Error("publish failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, source string, dryRun bool) error {
	spec, err := readDescriptor(source)
	if err != nil {
		return err
	}

	request, err := buildRequest(source, spec)
	if err != nil {
		return err
	}

	if dryRun {
		for _, file := range request.Files {
			logger.Info("would publish",
				"bundle", request.BundleID, "version", request.Version,
				"asset", file.AssetID, "role", string(file.Role),
				"bytes", file.ByteSize, "sha256", file.ChecksumSHA256,
				"key", catalogdomain.StorageKeyFor(request.BundleID, request.Version, file.AssetID))
		}
		logger.Info("dry run complete — nothing uploaded, nothing registered")
		return nil
	}

	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required to register a published bundle")
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	if err := pool.ApplyMigrations(ctx); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	writer, publicBaseURL, err := buildObjectWriter(cfg, logger)
	if err != nil {
		return err
	}
	store, err := catalinfra.NewBundleObjectStore(writer, publicBaseURL)
	if err != nil {
		return err
	}

	publisher := catalogapp.NewBundlePublisher(catalinfra.NewPostgresBundleRepository(pool.Pool()), store)
	result, err := publisher.Publish(ctx, request)
	if err != nil {
		return err
	}

	if result.AlreadyPublished {
		logger.Info("already published — nothing to do (a published version is immutable)",
			"bundle", result.Bundle.BundleID, "version", result.Bundle.Version)
		return nil
	}
	logger.Info("published",
		"bundle", result.Bundle.BundleID, "version", result.Bundle.Version,
		"kind", string(result.Bundle.Kind), "format", result.Bundle.Format,
		"files", strings.Join(result.UploadedAssetIDs, ","))
	for _, file := range result.Bundle.Files {
		logger.Info("file", "asset", file.AssetID, "url", store.PublicURL(file.StorageKey))
	}
	return nil
}

func readDescriptor(source string) (descriptor, error) {
	path := filepath.Join(source, "bundle.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return descriptor{}, fmt.Errorf("read %s: %w", path, err)
	}
	var spec descriptor
	if err := json.Unmarshal(raw, &spec); err != nil {
		return descriptor{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return spec, nil
}

func buildRequest(source string, spec descriptor) (catalogapp.PublishRequest, error) {
	request := catalogapp.PublishRequest{
		BundleID:      spec.BundleID,
		Version:       spec.Version,
		Kind:          catalogdomain.BundleKind(spec.Kind),
		Format:        spec.Format,
		MinAppVersion: spec.MinAppVersion,
	}
	if request.MinAppVersion == 0 {
		request.MinAppVersion = 1
	}
	for _, dependency := range spec.Dependencies {
		request.Dependencies = append(request.Dependencies, catalogdomain.BundleDependency{
			BundleID: dependency.BundleID,
			Version:  dependency.Version,
		})
	}

	for _, file := range spec.Files {
		if file.Path == "" || strings.Contains(file.Path, "..") || filepath.IsAbs(file.Path) {
			return catalogapp.PublishRequest{}, fmt.Errorf("asset %q has an unsafe path %q", file.AssetID, file.Path)
		}
		path := filepath.Join(source, file.Path)
		size, checksum, err := hashFile(path)
		if err != nil {
			return catalogapp.PublishRequest{}, err
		}
		request.Files = append(request.Files, catalogapp.PublishSource{
			AssetID:        file.AssetID,
			Role:           catalogdomain.AssetRole(file.Role),
			ContentType:    file.ContentType,
			ByteSize:       size,
			ChecksumSHA256: checksum,
			Open: func() (catalogapp.ReadSeekCloser, error) {
				return os.Open(path)
			},
		})
	}
	return request, nil
}

func hashFile(path string) (int64, string, error) {
	handle, err := os.Open(path)
	if err != nil {
		return 0, "", fmt.Errorf("open %s: %w", path, err)
	}
	defer handle.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, handle)
	if err != nil {
		return 0, "", fmt.Errorf("hash %s: %w", path, err)
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

func buildObjectWriter(cfg config.Config, logger *slog.Logger) (objectstore.PublicWriter, string, error) {
	switch cfg.ObjectStorageDriver {
	case "r2":
		r2, err := mediainfra.NewR2ObjectStorage(mediainfra.R2Config{
			AccountID:       cfg.R2AccountID,
			Endpoint:        cfg.R2Endpoint,
			Bucket:          cfg.R2Bucket,
			AccessKeyID:     cfg.R2AccessKeyID,
			SecretAccessKey: cfg.R2SecretAccessKey,
		})
		if err != nil {
			return nil, "", err
		}
		if cfg.AssetBundlePublicBaseURL == "" {
			return nil, "", errors.New("ASSET_BUNDLE_PUBLIC_BASE_URL is required with OBJECT_STORAGE_DRIVER=r2 — " +
				"a bundle published to a bucket with no public origin could never be fetched")
		}
		logger.Info("object storage: Cloudflare R2", "bucket", cfg.R2Bucket)
		return r2, cfg.AssetBundlePublicBaseURL, nil

	case "filesystem":
		if cfg.Environment == config.Production {
			return nil, "", errors.New("OBJECT_STORAGE_DRIVER=filesystem is not permitted in production")
		}
		root := cfg.DevStorageRoot
		if root == "" {
			root = "./.dev-storage"
		}
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, "", err
		}
		publicBase := cfg.PublicBaseURL
		if publicBase == "" {
			publicBase = "http://localhost:" + cfg.Port
		}
		fs, err := mediainfra.NewFilesystemObjectStorage(root, publicBase, secret)
		if err != nil {
			return nil, "", err
		}
		assetBase := cfg.AssetBundlePublicBaseURL
		if assetBase == "" {
			assetBase = strings.TrimRight(publicBase, "/") + strings.TrimSuffix(mediainfra.DevPublicAssetPathPrefix, "/")
		}
		logger.Warn("object storage: FILESYSTEM DEV ADAPTER — non-production", "root", root, "public_base_url", assetBase)
		return fs, assetBase, nil

	case "":
		return nil, "", errors.New("OBJECT_STORAGE_DRIVER is required (\"r2\" or, for local development, \"filesystem\")")
	default:
		return nil, "", fmt.Errorf("unknown OBJECT_STORAGE_DRIVER %q", cfg.ObjectStorageDriver)
	}
}
