-- — Photo Upload, Media Persistence & Room Assignment.
-- Two changes, one migration, because the second is meaningless without
-- the first:
-- 1. `assets` — §5's Asset entity:
-- metadata only, never bytes. The *only* table that ever holds an
-- object-storage key.
-- 2. `room_photo_slots.photo_asset_id` becomes a real foreign key to
-- `assets`, replacing placeholder TEXT column.
-- Change 2 alters an existing column's type. That is safe here for one
-- reason only: no deployment and no production data exist (
-- and the report). This window closes permanently at first
-- deployment, and a future change of this kind would need an
-- expand/migrate/contract sequence instead.

-- ---------------------------------------------------------------------
-- 1. Asset metadata.
-- ---------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS assets (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 -- NULL is permitted because §5 defines Asset ownership as split:
 -- User-owned for photographs, Platform-owned for catalog bundles.
 -- writes only user photographs (account_id always set);
 -- catalog bundles are not migrated onto this table here.
    account_id       UUID REFERENCES accounts(id) ON DELETE CASCADE,
    category         TEXT NOT NULL,
 -- The one place in the schema an object-storage key exists.
 -- Backend-generated, never client-supplied, never overwritten.
    storage_key      TEXT NOT NULL UNIQUE,
    content_type     TEXT NOT NULL,
    byte_size        BIGINT NOT NULL,
    pixel_width      INTEGER NOT NULL,
    pixel_height     INTEGER NOT NULL,
 -- Lowercase hex SHA-256 of the stored bytes, as declared by the
 -- client and verified server-side before commit.
    checksum_sha256  TEXT NOT NULL,
 -- pending: row exists, bytes may or may not; the asset does not yet
 -- "exist" from the product's perspective (§6).
 -- committed: verified and referenced by content, in one transaction.
 -- discarded: reclaimed; a tombstone so the key is never reissued.
    state            TEXT NOT NULL,
 -- The client's idempotency key for the upload (PickedPhoto.id).
    client_upload_id TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    committed_at     TIMESTAMPTZ,
    discarded_at     TIMESTAMPTZ,

    CONSTRAINT assets_state_known
        CHECK (state IN ('pending', 'committed', 'discarded')),
    CONSTRAINT assets_byte_size_positive  CHECK (byte_size > 0),
    CONSTRAINT assets_pixel_width_positive  CHECK (pixel_width > 0),
    CONSTRAINT assets_pixel_height_positive CHECK (pixel_height > 0),
    CONSTRAINT assets_checksum_is_sha256_hex
        CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT assets_committed_has_timestamp
        CHECK (state <> 'committed' OR committed_at IS NOT NULL),
    CONSTRAINT assets_discarded_has_timestamp
        CHECK (state <> 'discarded' OR discarded_at IS NOT NULL)
);

-- Idempotency: one live asset per (account, client upload id).
-- Partial on state so a discarded tombstone does not block a genuine
-- later retry of the same client upload id — e.g. a user resuming a
-- day after an abandoned upload was reclaimed.
CREATE UNIQUE INDEX IF NOT EXISTS assets_live_client_upload_unique
    ON assets (account_id, client_upload_id)
    WHERE state <> 'discarded';

-- The reclamation sweep's access path.
CREATE INDEX IF NOT EXISTS assets_pending_by_age
    ON assets (created_at)
    WHERE state = 'pending';

-- ---------------------------------------------------------------------
-- 2. room_photo_slots.photo_asset_id → real reference.
-- ---------------------------------------------------------------------

-- Before no application code path ever wrote photo_asset_id
-- (there was no endpoint), and no asset could have existed for it to
-- reference. The only values that can legitimately be present are the
-- column's own default, ''. Anything else is a state this migration was
-- not written for — refuse loudly rather than convert or discard it.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM room_photo_slots WHERE photo_asset_id <> '') THEN
        RAISE EXCEPTION
            'migration 0004: room_photo_slots.photo_asset_id holds non-empty values, '
            'which cannot be real asset references (no assets table existed). '
            'Inspect and clear them explicitly before applying this migration.';
    END IF;
END
$$;

ALTER TABLE room_photo_slots
    ALTER COLUMN photo_asset_id DROP DEFAULT;

ALTER TABLE room_photo_slots
    ALTER COLUMN photo_asset_id DROP NOT NULL;

ALTER TABLE room_photo_slots
    ALTER COLUMN photo_asset_id TYPE UUID
    USING NULLIF(photo_asset_id, '')::uuid;

-- RESTRICT, not CASCADE: an asset that content still references may not
-- be deleted. Content deletion removes the slot first; byte
-- reclamation follows asynchronously (§6/§9).
ALTER TABLE room_photo_slots
    ADD CONSTRAINT room_photo_slots_photo_asset_fk
    FOREIGN KEY (photo_asset_id) REFERENCES assets(id) ON DELETE RESTRICT;

-- One asset hangs in at most one slot. This is also what makes a retried
-- assignment idempotent: a second insert of the same asset is a
-- database rejection, not a duplicate.
CREATE UNIQUE INDEX IF NOT EXISTS room_photo_slots_photo_asset_unique
    ON room_photo_slots (photo_asset_id)
    WHERE photo_asset_id IS NOT NULL;

COMMENT ON COLUMN room_photo_slots.photo_asset_id IS
    'References assets(id). Nullable at the schema level only; the application '
    'always writes a committed asset — nullability exists for the '
    'type migration and for a possible future error-state slot (02: "slot shows a '
    'placeholder/error state"), not as a normal state.';
