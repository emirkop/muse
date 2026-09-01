package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type BundleKind string

const (
	BundleKindMuseumStyle      BundleKind = "museum_style"
	BundleKindRoomVariant      BundleKind = "room_variant"
	BundleKindSculpture        BundleKind = "sculpture"
	BundleKindAvatar           BundleKind = "avatar"
	BundleKindCollectionDesign BundleKind = "collection_design"
	BundleKindCollectionItem   BundleKind = "collection_item"
)

func (k BundleKind) IsValid() bool {
	switch k {
	case BundleKindMuseumStyle, BundleKindRoomVariant, BundleKindSculpture,
		BundleKindAvatar, BundleKindCollectionDesign, BundleKindCollectionItem:
		return true
	default:
		return false
	}
}

type AssetRole string

const (
	RoleGeometry AssetRole = "geometry"
	RoleLayout   AssetRole = "layout"
	RoleMaterial AssetRole = "material"
	RoleTexture  AssetRole = "texture"
)

func (r AssetRole) IsValid() bool {
	switch r {
	case RoleGeometry, RoleLayout, RoleMaterial, RoleTexture:
		return true
	default:
		return false
	}
}

func (r AssetRole) deliveryPriority() int {
	switch r {
	case RoleGeometry:
		return 0
	case RoleLayout:
		return 1
	case RoleMaterial:
		return 2
	default:
		return 3
	}
}

type BundleFile struct {
	AssetID        string
	Role           AssetRole
	StorageKey     string
	ContentType    string
	ByteSize       int64
	ChecksumSHA256 string
}

type BundleDependency struct {
	BundleID string
	Version  int
}

type AssetBundle struct {
	BundleID      string
	Version       int
	Kind          BundleKind
	Format        string
	MinAppVersion int
	Files         []BundleFile
	Dependencies  []BundleDependency

	TierCapacities TierCapacities
}

func (b AssetBundle) GeometryFile() (BundleFile, bool) {
	for _, file := range b.Files {
		if file.Role == RoleGeometry {
			return file, true
		}
	}
	return BundleFile{}, false
}

func (b AssetBundle) OrderedFiles() []BundleFile {
	ordered := make([]BundleFile, len(b.Files))
	copy(ordered, b.Files)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && lessInDeliveryOrder(ordered[j], ordered[j-1]); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	return ordered
}

func lessInDeliveryOrder(a, b BundleFile) bool {
	if a.Role.deliveryPriority() != b.Role.deliveryPriority() {
		return a.Role.deliveryPriority() < b.Role.deliveryPriority()
	}
	return a.AssetID < b.AssetID
}

// MARK: - Validation

var (
	ErrBundleNotFound = errors.New("catalog: no compatible published asset bundle")

	ErrBundleInvalid = errors.New("catalog: invalid asset bundle")

	ErrBundleVersionImmutable = errors.New("catalog: a published bundle version is immutable")
)

var storageKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)*$`)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

const checksumHexLength = 64

var hexPattern = regexp.MustCompile(`^[0-9a-f]+$`)

func (b AssetBundle) Validate() error {
	if !identifierPattern.MatchString(b.BundleID) {
		return fmt.Errorf("%w: bundle id %q", ErrBundleInvalid, b.BundleID)
	}
	if b.Version <= 0 {
		return fmt.Errorf("%w: version must be positive, got %d", ErrBundleInvalid, b.Version)
	}
	if !b.Kind.IsValid() {
		return fmt.Errorf("%w: unknown kind %q", ErrBundleInvalid, b.Kind)
	}
	if !identifierPattern.MatchString(b.Format) {
		return fmt.Errorf("%w: format %q", ErrBundleInvalid, b.Format)
	}
	if b.MinAppVersion <= 0 {
		return fmt.Errorf("%w: min app version must be positive, got %d", ErrBundleInvalid, b.MinAppVersion)
	}
	if len(b.Files) == 0 {
		return fmt.Errorf("%w: a bundle with no files delivers nothing", ErrBundleInvalid)
	}

	if _, ok := b.GeometryFile(); !ok {
		return fmt.Errorf("%w: no file has role %q", ErrBundleInvalid, RoleGeometry)
	}

	seenAssetIDs := make(map[string]struct{}, len(b.Files))
	geometryCount := 0
	for _, file := range b.Files {
		if err := file.validate(); err != nil {
			return err
		}
		if _, duplicate := seenAssetIDs[file.AssetID]; duplicate {
			return fmt.Errorf("%w: duplicate asset id %q", ErrBundleInvalid, file.AssetID)
		}
		seenAssetIDs[file.AssetID] = struct{}{}
		if file.Role == RoleGeometry {
			geometryCount++
		}
	}
	if geometryCount != 1 {
		return fmt.Errorf("%w: expected exactly one geometry file, got %d", ErrBundleInvalid, geometryCount)
	}

	if len(b.TierCapacities) > 0 {
		if b.Kind != BundleKindCollectionDesign {
			return fmt.Errorf("%w: only a %q bundle may carry tier capacities, this is %q",
				ErrBundleInvalid, BundleKindCollectionDesign, b.Kind)
		}
		if err := b.TierCapacities.Validate(); err != nil {
			return err
		}
	}

	seenDependencies := make(map[string]struct{}, len(b.Dependencies))
	for _, dependency := range b.Dependencies {
		if !identifierPattern.MatchString(dependency.BundleID) {
			return fmt.Errorf("%w: dependency bundle id %q", ErrBundleInvalid, dependency.BundleID)
		}
		if dependency.BundleID == b.BundleID {
			return fmt.Errorf("%w: bundle depends on itself", ErrBundleInvalid)
		}
		if dependency.Version <= 0 {
			return fmt.Errorf("%w: dependency %q version must be positive", ErrBundleInvalid, dependency.BundleID)
		}
		if _, duplicate := seenDependencies[dependency.BundleID]; duplicate {
			return fmt.Errorf("%w: duplicate dependency %q", ErrBundleInvalid, dependency.BundleID)
		}
		seenDependencies[dependency.BundleID] = struct{}{}
	}
	return nil
}

func (f BundleFile) validate() error {
	if !identifierPattern.MatchString(f.AssetID) {
		return fmt.Errorf("%w: asset id %q", ErrBundleInvalid, f.AssetID)
	}
	if !f.Role.IsValid() {
		return fmt.Errorf("%w: asset %q has unknown role %q", ErrBundleInvalid, f.AssetID, f.Role)
	}
	if !storageKeyPattern.MatchString(f.StorageKey) {
		return fmt.Errorf("%w: asset %q storage key %q", ErrBundleInvalid, f.AssetID, f.StorageKey)
	}
	if strings.TrimSpace(f.ContentType) == "" {
		return fmt.Errorf("%w: asset %q has no content type", ErrBundleInvalid, f.AssetID)
	}
	if f.ByteSize <= 0 {
		return fmt.Errorf("%w: asset %q byte size must be positive, got %d", ErrBundleInvalid, f.AssetID, f.ByteSize)
	}
	if len(f.ChecksumSHA256) != checksumHexLength || !hexPattern.MatchString(f.ChecksumSHA256) {
		return fmt.Errorf("%w: asset %q checksum must be %d lowercase hex characters", ErrBundleInvalid, f.AssetID, checksumHexLength)
	}
	return nil
}

func StorageKeyFor(bundleID string, version int, assetID string) string {
	return fmt.Sprintf("bundles/%s/v%d/%s", bundleID, version, assetID)
}

const BundleStorageKeyPrefix = "bundles/"
