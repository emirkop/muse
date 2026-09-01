-- 0019_create_asset_bundle_tier_capacities.sql
-- close-out: a DERIVED, server-side projection of a
-- Collection Design bundle's authored per-tier slot capacities.
-- WHY THIS TABLE EXISTS
-- rule 4 requires the server to refuse a Collection Item slot
-- that the Room's currently reached tier does not author. The server could
-- not: the capacities are authored in the Design bundle's `layout.json`
-- (`tiers[].cumulative_capacity`), the server never reads bundle files,
-- and `collection_designs.tier_count` (migration 0016) is a *bound on
-- tiers*, deliberately not a capacity. So an owner bypassing the client
-- could persist an item onto an unauthored or future-tier slot.
-- WHAT THIS TABLE IS, AND IS NOT
-- * It IS a projection. `cmd/assetpublish` → `BundlePublisher.Publish`
-- parses the layout file of a `collection_design` bundle at publish
-- time and registers, in the SAME transaction as the bundle's files,
-- how many slots each tier makes legal. Derived from the bytes that
-- are uploaded, by one code path, and a published version's bytes are
-- immutable — so this cannot drift from the layout it came from. A
-- whole-stack test re-derives it from the stored bytes and compares.
-- * It is NOT a second authoring source. Nobody hand-edits these rows;
-- the only writer is the bundle registry's Register path. The numbers
-- for the development fixture (4 / 10 / 18) are ENGINEERING FIXTURE
-- VALUES that live in `assets/dev_fixtures/…/layout.json`, and
-- (the real per-Design capacities) remains OPEN.
-- * It is NOT content. It is keyed to the bundle's identity and version
-- — no account, museum, room, collection room or item appears here —
-- and `collection_designs` gains no capacity column (the test
-- asserting that absence still holds).
-- WHY KEYED BY (bundle_id, version) AND NOT BY DESIGN
-- Because that is the identity the delivery architecture resolves. A
-- client asks for the newest version compatible with its declared
-- bundle-format generation (`ResolveForApp`); the server validating a
-- placement resolves the SAME bundle id with the SAME declared generation
-- through the SAME function, and reads this table for the version that
-- returns. Two clients on different format generations may legitimately
-- render different versions of one Design, and each is validated against
-- the capacities of the version it actually holds.
-- ON DELETE CASCADE with the bundle version: a projection of something
-- that no longer exists is not worth keeping. (Bundle versions are never
-- deleted in practice — they are immutable — so this is hygiene, not a
-- path.)

CREATE TABLE IF NOT EXISTS asset_bundle_tier_capacities (
    bundle_id TEXT    NOT NULL,
    version   INTEGER NOT NULL,
 -- 1-based tier ordinal, matching collection_rooms.current_tier.
    tier      INTEGER NOT NULL,
 -- The TOTAL number of legal slots once this tier is reached, i.e. the
 -- legal slot indices are 0 .. cumulative_capacity - 1. Cumulative, not
 -- an increment — the same shape the client's table and `02`'s trigger
 -- ("adding an item would exceed the current layout's capacity") use.
    cumulative_capacity INTEGER NOT NULL,
    PRIMARY KEY (bundle_id, version, tier),
    CONSTRAINT asset_bundle_tier_capacities_bundle_fk
        FOREIGN KEY (bundle_id, version)
        REFERENCES asset_bundles (bundle_id, version) ON DELETE CASCADE,
    CONSTRAINT asset_bundle_tier_capacities_tier_positive CHECK (tier >= 1),
    CONSTRAINT asset_bundle_tier_capacities_capacity_positive CHECK (cumulative_capacity >= 1)
);

COMMENT ON TABLE asset_bundle_tier_capacities IS
    'DERIVED projection of a collection_design bundle''s layout.json tier capacities, written only by '
    'the bundle registry at publish time. Not an authoring source; remains open. '
    'Legal item slots at tier T are 0 .. cumulative_capacity(T) - 1.';
