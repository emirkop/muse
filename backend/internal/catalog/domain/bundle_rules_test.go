package domain

import (
	"errors"
	"strings"
	"testing"
)

const bundleChecksum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func file(assetID string, role AssetRole) BundleFile {
	return BundleFile{
		AssetID:        assetID,
		Role:           role,
		StorageKey:     StorageKeyFor("bundle_style_modern", 1, assetID),
		ContentType:    "application/octet-stream",
		ByteSize:       1024,
		ChecksumSHA256: bundleChecksum,
	}
}

func bundle(files ...BundleFile) AssetBundle {
	if len(files) == 0 {
		files = []BundleFile{file("geometry", RoleGeometry)}
	}
	return AssetBundle{
		BundleID:      "bundle_style_modern",
		Version:       1,
		Kind:          BundleKindMuseumStyle,
		Format:        "usdz",
		MinAppVersion: 1,
		Files:         files,
	}
}

func TestAssetBundle_ValidateAcceptsAPublishableBundle(t *testing.T) {
	if err := bundle().Validate(); err != nil {
		t.Fatalf("a minimal valid bundle was refused: %v", err)
	}
	full := bundle(
		file("geometry", RoleGeometry),
		file("layout", RoleLayout),
		file("wall_material", RoleMaterial),
		file("wall_texture", RoleTexture),
	)
	if err := full.Validate(); err != nil {
		t.Fatalf("a four-file bundle was refused: %v", err)
	}
}

func TestAssetBundle_ExactlyOneGeometryFile(t *testing.T) {
	none := bundle(file("layout", RoleLayout))
	if err := none.Validate(); !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("a bundle with no geometry → %v, want a refusal", err)
	}

	two := bundle(file("geometry_a", RoleGeometry), file("geometry_b", RoleGeometry))
	if err := two.Validate(); !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("a bundle with two geometry files → %v; nothing decides which one loads", err)
	}

	empty := bundle()
	empty.Files = nil
	if err := empty.Validate(); !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("a bundle with no files delivers nothing → %v", err)
	}
}

func TestAssetBundle_ValidateRefusesEachMalformedIdentityFieldIndividually(t *testing.T) {
	mutations := map[string]func(*AssetBundle){
		"empty bundle id":        func(b *AssetBundle) { b.BundleID = "" },
		"bundle id with a path":  func(b *AssetBundle) { b.BundleID = "../etc/passwd" },
		"bundle id with a space": func(b *AssetBundle) { b.BundleID = "bundle style" },
		"zero version":           func(b *AssetBundle) { b.Version = 0 },
		"negative version":       func(b *AssetBundle) { b.Version = -1 },
		"unknown kind":           func(b *AssetBundle) { b.Kind = BundleKind("wallpaper") },
		"empty format":           func(b *AssetBundle) { b.Format = "" },
		"zero min app version":   func(b *AssetBundle) { b.MinAppVersion = 0 },
	}
	for name, mutate := range mutations {
		candidate := bundle()
		mutate(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrBundleInvalid) {
			t.Errorf("%s was accepted (err = %v)", name, err)
		}
	}
}

func TestAssetBundle_ValidateRefusesDuplicateAssetIDs(t *testing.T) {
	duplicated := bundle(file("geometry", RoleGeometry), file("geometry", RoleLayout))
	if err := duplicated.Validate(); !errors.Is(err, ErrBundleInvalid) {
		t.Fatalf("a duplicate asset id was accepted: %v", err)
	}
}

func TestBundleFile_ValidateRefusesEachMalformedFieldIndividually(t *testing.T) {
	mutations := map[string]func(*BundleFile){
		"empty asset id":        func(f *BundleFile) { f.AssetID = "" },
		"asset id with a slash": func(f *BundleFile) { f.AssetID = "a/b" },
		"unknown role":          func(f *BundleFile) { f.Role = AssetRole("lighting") },
		"empty storage key":     func(f *BundleFile) { f.StorageKey = "" },
		"no content type":       func(f *BundleFile) { f.ContentType = "   " },
		"zero byte size":        func(f *BundleFile) { f.ByteSize = 0 },
		"negative byte size":    func(f *BundleFile) { f.ByteSize = -1 },
		"short checksum":        func(f *BundleFile) { f.ChecksumSHA256 = bundleChecksum[:63] },
		"long checksum":         func(f *BundleFile) { f.ChecksumSHA256 = bundleChecksum + "a" },
		"uppercase checksum":    func(f *BundleFile) { f.ChecksumSHA256 = strings.ToUpper(bundleChecksum) },
		"non-hex checksum":      func(f *BundleFile) { f.ChecksumSHA256 = strings.Repeat("g", 64) },
	}
	for name, mutate := range mutations {
		candidate := file("geometry", RoleGeometry)
		mutate(&candidate)
		if err := bundle(candidate).Validate(); !errors.Is(err, ErrBundleInvalid) {
			t.Errorf("%s was accepted (err = %v)", name, err)
		}
	}
}

func TestAssetBundle_ValidateRefusesUnpinnedOrDuplicateDependencies(t *testing.T) {
	unpinned := bundle()
	unpinned.Dependencies = []BundleDependency{{BundleID: "bundle_shared_materials", Version: 0}}
	if err := unpinned.Validate(); !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("an unpinned dependency was accepted: %v", err)
	}

	duplicated := bundle()
	duplicated.Dependencies = []BundleDependency{
		{BundleID: "bundle_shared_materials", Version: 1},
		{BundleID: "bundle_shared_materials", Version: 2},
	}
	if err := duplicated.Validate(); !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("two versions of one dependency were accepted: %v", err)
	}
}

// MARK: - Delivery order (`02`'s progressive reveal)

func TestAssetBundle_OrderedFilesIsRoleThenAssetID(t *testing.T) {
	scrambled := bundle(
		file("z_texture", RoleTexture),
		file("a_material", RoleMaterial),
		file("layout", RoleLayout),
		file("geometry", RoleGeometry),
		file("b_material", RoleMaterial),
		file("a_texture", RoleTexture),
	)

	var order []string
	for _, f := range scrambled.OrderedFiles() {
		order = append(order, f.AssetID)
	}
	want := []string{"geometry", "layout", "a_material", "b_material", "a_texture", "z_texture"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("delivery order = %v, want %v", order, want)
	}
}

func TestAssetBundle_OrderedFilesIsDeterministicAndNonMutating(t *testing.T) {
	original := bundle(
		file("texture", RoleTexture),
		file("geometry", RoleGeometry),
		file("layout", RoleLayout),
	)
	before := append([]BundleFile(nil), original.Files...)

	first := original.OrderedFiles()
	second := original.OrderedFiles()
	for index := range first {
		if first[index].AssetID != second[index].AssetID {
			t.Fatalf("two orderings disagree at %d: %q vs %q", index, first[index].AssetID, second[index].AssetID)
		}
	}
	for index := range before {
		if original.Files[index].AssetID != before[index].AssetID {
			t.Fatalf("OrderedFiles mutated the bundle at %d", index)
		}
	}
	empty := bundle()
	empty.Files = nil
	if got := empty.OrderedFiles(); len(got) != 0 {
		t.Errorf("an empty bundle ordered to %d files", len(got))
	}
}

func TestGeometryFile_FindsItOrReportsAbsence(t *testing.T) {
	found, ok := bundle(file("layout", RoleLayout), file("geometry", RoleGeometry)).GeometryFile()
	if !ok || found.AssetID != "geometry" {
		t.Fatalf("GeometryFile() = %+v, %v", found, ok)
	}
	if _, ok := bundle(file("layout", RoleLayout)).GeometryFile(); ok {
		t.Error("a bundle with no geometry must report absence, not a zero value as present")
	}
}

func TestAssetRole_IsValidIsAClosedSet(t *testing.T) {
	for _, role := range []AssetRole{RoleGeometry, RoleLayout, RoleMaterial, RoleTexture} {
		if !role.IsValid() {
			t.Errorf("%q must be valid", role)
		}
	}
	for _, role := range []AssetRole{"", "GEOMETRY", "lighting", "collision", "audio"} {
		if role.IsValid() {
			t.Errorf("%q was accepted as a role", role)
		}
	}
}

// MARK: - Storage keys

func TestStorageKeyFor_IsVersionedAndUnderTheBundlePrefix(t *testing.T) {
	key := StorageKeyFor("bundle_style_modern", 3, "geometry")
	if key != "bundles/bundle_style_modern/v3/geometry" {
		t.Fatalf("key = %q", key)
	}
	if !strings.HasPrefix(key, BundleStorageKeyPrefix) {
		t.Fatal("every bundle object must live under the single public prefix")
	}
	if StorageKeyFor("b", 1, "geometry") == StorageKeyFor("b", 2, "geometry") {
		t.Fatal("v1 and v2 must not share a key")
	}
	if strings.HasPrefix(key, "photos/") {
		t.Fatal("a bundle key must not collide with the photo prefix")
	}
}
