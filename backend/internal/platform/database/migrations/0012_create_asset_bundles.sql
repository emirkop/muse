-- — Remote Asset Delivery: the versioned asset-bundle registry.
-- (closed at ) fixed the delivery *pattern*: a small,
-- backend-served manifest describing a Presentation-Model bundle's
-- identity, version, constituent assets (each with its own identity,
-- version and storage location), inter-bundle dependencies, and minimum
-- compatible app version — containing no binary content itself. These
-- three tables are that manifest's storage. implements the
-- pattern; it does not re-decide it.
-- THE ONE PROPERTY THIS SCHEMA EXISTS TO GUARANTEE
-- ================================================
-- Not one column here references an account, a museum, a room, or a
-- photo, and nothing in the Museum content tree references these tables.
-- Asset identity and asset version are therefore structurally separate
-- from user content: publishing v2 of a Style's geometry inserts rows
-- here and touches no user's Museum, and a Room's photographs/captions/
-- ordering/privacy cannot be affected by an asset release. That is
-- `04`'s "content survives presentation" and its Risk Register's highest
-- named risk, enforced by the schema rather than by discipline. A schema
-- test asserts it (cmd/api/asset_delivery_test.go).
-- The bridge in the other direction already exists and stays as it is:
-- `museum_styles`/`room_variants`/`sculptures` each carry
-- `asset_bundle_id` + `asset_bundle_version`, i.e. presentation rows
-- point *at* a bundle. That is a presentation→presentation reference,
-- and it is deliberately not a foreign key: a Style may name the bundle
-- it expects long before that bundle is authored and published, which is
-- exactly the state every Style and Variant is in today ( has
-- produced no content). Making it an FK would force fake bundle rows to
-- exist so the catalog could be seeded — inventing the very thing this
-- project refuses to invent.

-- ---------------------------------------------------------------------
-- A published bundle version.
-- ---------------------------------------------------------------------
-- (bundle_id, version) is the primary key, so a version is a distinct
-- row rather than a mutated one: publishing is additive, matching the
-- "never a silent in-place overwrite" discipline already fixed for Rive
-- (`assets/rive/) and Blender source (
-- §3). A published version is immutable — the publisher refuses to
-- re-publish an existing version whose file checksums differ.
CREATE TABLE IF NOT EXISTS asset_bundles (
    bundle_id TEXT    NOT NULL,
    version   INTEGER NOT NULL CHECK (version > 0),

 -- What kind of Presentation Model this bundle carries. Constrained
 -- to the four categories `04` Part E actually names as versioned
 -- bundle categories, so an unrecognised kind cannot be published.
    kind TEXT NOT NULL CHECK (kind IN ('museum_style', 'room_variant', 'sculpture', 'avatar')),

 -- The runtime format of the bundle's geometry, e.g. 'usdz' for
 -- production art or 'usda' for a development fixture (:
 -- USD/USDZ). Compatibility metadata only — the delivery path never
 -- inspects it, which is what makes swapping a fixture for a real
 -- export a data change rather than a code change.
    format TEXT NOT NULL,

 -- `04`'s 3D Asset Storage requirement, already fixed: "so an old app
 -- build never receives an asset format it can't parse." A client
 -- declares the bundle-format version it understands and the manifest
 -- endpoint resolves the newest published version at or below it —
 -- never the newest version outright.
    min_app_version INTEGER NOT NULL DEFAULT 1 CHECK (min_app_version > 0),

    published_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (bundle_id, version)
);

COMMENT ON TABLE asset_bundles IS
    'Registry of published Presentation-Model asset bundle versions. '
    'Platform-owned; no account/museum/room reference exists or may be added. Additive: a '
    'published version is immutable and a new version is a new row.';

CREATE INDEX IF NOT EXISTS idx_asset_bundles_kind ON asset_bundles (kind);

-- ---------------------------------------------------------------------
-- The files that make up one bundle version.
-- ---------------------------------------------------------------------
-- Metadata only — the bytes live in object storage and are never in the
-- database ( §5's rule). `storage_key`
-- is a key, never a URL: the URL is derived at request time from the
-- configured public asset base, so moving buckets or putting a different
-- CDN hostname in front of the same bucket is configuration, not a
-- migration.
-- `checksum_sha256` is the integrity contract. The publisher computes it
-- over the local file, the store is asked to enforce it on write, the
-- publisher re-reads the stored object to confirm it, and the client
-- verifies it again over the assembled download. A resumable download
-- that stitched together bytes from two different versions of a file
-- fails here, which is the whole reason resume is safe to offer.
CREATE TABLE IF NOT EXISTS asset_bundle_files (
    bundle_id TEXT    NOT NULL,
    version   INTEGER NOT NULL,

 -- Stable identity of this asset *within* the bundle ('geometry',
 -- 'layout', 'material_wood', …). Stable across versions on purpose:
 -- it is the client's cache key together with (bundle_id, version),
 -- and it is what lets a future diffing client fetch only the files
 -- whose checksum changed.
    asset_id TEXT NOT NULL,

 -- What the client should do with the file. 'geometry' is downloaded
 -- first so `02`'s progressive reveal (geometry, then materials and
 -- detail) is the delivery order and not just a rendering intention.
    role TEXT NOT NULL CHECK (role IN ('geometry', 'layout', 'material', 'texture')),

    storage_key     TEXT   NOT NULL,
    content_type    TEXT   NOT NULL,
    byte_size       BIGINT NOT NULL CHECK (byte_size > 0),
    checksum_sha256 TEXT   NOT NULL CHECK (char_length(checksum_sha256) = 64),

    PRIMARY KEY (bundle_id, version, asset_id),
    FOREIGN KEY (bundle_id, version)
        REFERENCES asset_bundles (bundle_id, version) ON DELETE CASCADE
);

COMMENT ON TABLE asset_bundle_files IS
 'Per-file metadata for one published bundle version. Metadata only: bytes live '
    'in object storage. checksum_sha256 is verified on publish and again by the client after '
    'a completed (possibly resumed) download.';

-- ---------------------------------------------------------------------
-- Inter-bundle dependencies.
-- ---------------------------------------------------------------------
-- 's "dependency graph where relevant (e.g. a Room Variant's
-- manifest references its parent Museum Style's shared material library
-- rather than duplicating those references)" —
-- §1's shared-materials principle, made
-- resolvable. A dependency names an exact (bundle, version), so a
-- Variant is never silently re-pointed at a newer material library it
-- was not authored against.
-- The FK to the exact depended-on version is what makes that a database
-- fact: a bundle cannot be published declaring a dependency on a version
-- that does not exist.
CREATE TABLE IF NOT EXISTS asset_bundle_dependencies (
    bundle_id TEXT    NOT NULL,
    version   INTEGER NOT NULL,

    depends_on_bundle_id TEXT    NOT NULL,
    depends_on_version   INTEGER NOT NULL,

    PRIMARY KEY (bundle_id, version, depends_on_bundle_id),
    FOREIGN KEY (bundle_id, version)
        REFERENCES asset_bundles (bundle_id, version) ON DELETE CASCADE,
    FOREIGN KEY (depends_on_bundle_id, depends_on_version)
        REFERENCES asset_bundles (bundle_id, version) ON DELETE RESTRICT,
 -- A bundle depending on itself would make manifest resolution
 -- non-terminating; refuse it in the schema rather than in a loop.
    CHECK (depends_on_bundle_id <> bundle_id)
);

COMMENT ON TABLE asset_bundle_dependencies IS
 'Exact (bundle, version) dependencies between published bundles — e.g. a '
    'Room Variant on its Style''s shared material library. Pinned to a version so a bundle is '
    'never silently re-pointed at content it was not authored against.';
