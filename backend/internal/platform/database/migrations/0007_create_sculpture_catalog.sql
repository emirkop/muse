-- — Sculptures: the sculpture catalog (Presentation).
-- created the *content* side (`room_sculptures`, with the
-- confirmed 3-per-Room cap as a data-layer invariant) but left
-- `catalog_id` a bare TEXT column, because no sculpture catalog existed
-- to reference. This migration creates it and makes the reference real.
-- `sculptures` is a Presentation table in exactly the sense
-- `0003_create_museum_and_catalog.sql` establishes: Platform-owned,
-- identical for every user, carrying no per-user data and no account /
-- museum / room reference in either direction. Its shape mirrors
-- `museum_styles` and `room_variants` deliberately — same primary-key
-- style (a stable TEXT id, not a UUID, because catalog ids are authored
-- constants), same asset-bundle reference pair, same "metadata only,
-- never bytes" rule.
-- **The production catalog is deliberately EMPTY**. `01` §4.7
-- confirms "predefined sculptures, max 3 per Room" but never names a
-- single one, and `03`'s Sculptures section lists catalog scope/content
-- as Open. (the Blender pipeline) has never run, so no sculpture
-- model exists either. Seeding invented entries would pre-empt a product
-- decision and claim assets that do not exist, so this table ships with
-- zero rows and `SeedSculptures` returns none. That is a truthful empty
-- catalog, not a missing feature: every mechanism around it is real.

CREATE TABLE IF NOT EXISTS sculptures (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
 -- Reference to the versioned bundle holding this sculpture's model.
 -- The backend serves the reference; bytes come from the CDN.
    asset_bundle_id      TEXT NOT NULL DEFAULT '',
    asset_bundle_version INTEGER NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------
-- Make room_sculptures.catalog_id a real reference.
-- ---------------------------------------------------------------------
-- Every other presentation reference in the schema is a real foreign key
-- (`museums.style_id`, `rooms.variant_id`), so a sculpture instance
-- pointing at a catalog entry that does not exist should be impossible
-- for the same reason. With the catalog empty this also makes the honest
-- consequence structural: today no sculpture can be placed in any Room,
-- because there is genuinely nothing to place.
-- Before no application code path ever wrote catalog_id (there
-- was no endpoint), so the only rows that can exist are test fixtures.
-- Refuse loudly rather than deleting or rewriting anything.
DO $$
DECLARE
    orphans BIGINT;
BEGIN
    SELECT count(*) INTO orphans
    FROM room_sculptures rs
    WHERE NOT EXISTS (SELECT 1 FROM sculptures s WHERE s.id = rs.catalog_id);

    IF orphans > 0 THEN
        RAISE EXCEPTION
            'migration 0007: % room_sculptures row(s) reference a catalog_id with no sculptures row. '
            'No application path wrote this column before, so these are fixtures. '
            'Inspect and remove them explicitly before applying this migration.', orphans;
    END IF;
END
$$;

-- RESTRICT, not CASCADE: a catalog entry that content still references
-- may not be deleted. Catalog entries are authored constants and are not
-- expected to be deleted at all; this makes an accidental one impossible
-- rather than silently destructive.
ALTER TABLE room_sculptures
    ADD CONSTRAINT room_sculptures_catalog_fk
    FOREIGN KEY (catalog_id) REFERENCES sculptures(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_room_sculptures_catalog_id ON room_sculptures (catalog_id);

-- The cap and slot rules already in place from 0003 need no change and
-- are exactly right for (removal leaves the slot empty):
-- CHECK (slot_index >= 0 AND slot_index < 3) — the confirmed cap
-- UNIQUE (room_id, slot_index) — one sculpture per slot
-- Neither requires contiguity, which is precisely what lets a removed
-- sculpture's slot stay empty while the others do not move.
COMMENT ON TABLE sculptures IS
 'Presentation catalog of predefined sculptures. Platform-owned, no per-user data. '
 'Deliberately empty until real content exists: no sculpture is named in product '
 'knowledge and has produced no model.';
