-- 0003_create_museum_and_catalog.sql
--: the Content/Presentation separation from
-- 's Domain Layer, expressed as schema.
-- Two independent families with only a *reference* relationship between
-- them, never embedding or inheritance:
-- Content (museums, rooms, room_photo_slots, room_sculptures) — what
-- the user created/chose. Never holds a transform, mesh,
-- material, or asset path.
-- Presentation (museum_styles, room_variants) — how things look.
-- Platform-owned, identical for every user, no per-user data.
-- This is the single highest architectural risk in `04`'s Risk Register
-- (premature coupling of content to presentation), so the separation is
-- enforced structurally here rather than by convention.

-- ---------------------------------------------------------------
-- Presentation catalog (Platform-owned; carries no per-user data)
-- ---------------------------------------------------------------

CREATE TABLE IF NOT EXISTS museum_styles (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    -- Reference to the versioned asset bundle in object storage
    -- (/Cloudflare R2). The backend serves metadata only, never
    -- bytes — §5.
    asset_bundle_id      TEXT NOT NULL DEFAULT '',
    asset_bundle_version INTEGER NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS room_variants (
    id           TEXT PRIMARY KEY,
    -- A Variant is scoped to exactly one Style: `04` Room Data says the
    -- active Variant is "scoped to the parent Museum's current Style."
    style_id     TEXT NOT NULL REFERENCES museum_styles(id),
    display_name TEXT NOT NULL,
    asset_bundle_id      TEXT NOT NULL DEFAULT '',
    asset_bundle_version INTEGER NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_room_variants_style_id ON room_variants (style_id);

-- ---------------------------------------------------------------
-- Content
-- ---------------------------------------------------------------

CREATE TABLE IF NOT EXISTS museums (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- One Museum per account, enforced at the data layer rather than in
    -- the UI, per `01` §8.1's confirmed 1:1 rule and explicit
    -- "as data-layer invariants, not UI limits."
    account_id UUID NOT NULL UNIQUE REFERENCES accounts(id),
    -- Style is a *reference only*. No geometry, material, or transform
    -- data ever lands in a content table — that is what makes
    -- `02`'s Museum Style Changing a pure reference swap.
    style_id   TEXT NOT NULL REFERENCES museum_styles(id),
    -- Public/Private. (an additional password-protected state)
    -- and (how Museum-level privacy interacts with Room-level)
    -- are both still OPEN — this column deliberately models only the two
    -- confirmed states and must not be treated as settling either.
    privacy    TEXT NOT NULL DEFAULT 'private',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT museums_privacy_valid CHECK (privacy IN ('public', 'private'))
);

CREATE TABLE IF NOT EXISTS rooms (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    museum_id  UUID NOT NULL REFERENCES museums(id) ON DELETE CASCADE,
    -- Free text. (length/uniqueness/profanity/renaming rules) is
    -- OPEN — no constraint is invented here.
    name       TEXT NOT NULL DEFAULT '',
    variant_id TEXT NOT NULL REFERENCES room_variants(id),
    privacy    TEXT NOT NULL DEFAULT 'private',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rooms_privacy_valid CHECK (privacy IN ('public', 'private'))
);

CREATE INDEX IF NOT EXISTS idx_rooms_museum_id ON rooms (museum_id);

-- The 28-photo cap, enforced structurally rather than by a trigger or a
-- count-check that races: slot_index is bounded 0..27 and unique per
-- room, so 28 rows is the arithmetic maximum. `01` §4.6's confirmed cap.
-- slot_index is a *logical* slot (placement order/role), never a
-- coordinate — `04`'s Logical Slot to Transform Mapping. The
-- slot→transform table lives in the Variant's asset bundle
-- (presentation), resolved at render time (+), never stored
-- here. The exact 1–28 sequence is /, still OPEN — and
-- deliberately does not need to be settled for this schema to be
-- correct, which is the separation working as intended.
CREATE TABLE IF NOT EXISTS room_photo_slots (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id    UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    slot_index INTEGER NOT NULL,
    -- Reference to the stored image (
    -- §6). Bytes live in object storage; this is metadata only. Upload
    -- itself is a later phase — this column exists so the schema is
    -- complete, not because populates it.
    photo_asset_id TEXT NOT NULL DEFAULT '',
    caption    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT room_photo_slots_index_within_cap CHECK (slot_index >= 0 AND slot_index < 28),
    CONSTRAINT room_photo_slots_unique_slot UNIQUE (room_id, slot_index)
);

CREATE INDEX IF NOT EXISTS idx_room_photo_slots_room_id ON room_photo_slots (room_id);

-- The 3-sculpture cap, enforced by the same structural mechanism.
-- `01` §4.8's confirmed cap.
CREATE TABLE IF NOT EXISTS room_sculptures (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id    UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    slot_index INTEGER NOT NULL,
    -- Reference into the sculpture catalog (presentation). Content
    -- stores the reference; the model itself is Platform-owned.
    catalog_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT room_sculptures_index_within_cap CHECK (slot_index >= 0 AND slot_index < 3),
    CONSTRAINT room_sculptures_unique_slot UNIQUE (room_id, slot_index)
);

CREATE INDEX IF NOT EXISTS idx_room_sculptures_room_id ON room_sculptures (room_id);
