-- 0021_allow_collection_item_bundle_kind.sql
--: let a catalog Model's optimized 3D asset be registered.
-- WHAT THIS CLOSES
-- gave `collection_models` an `asset_bundle_id` /
-- `asset_bundle_version` pair — the Model → asset mapping. But
-- `asset_bundles.kind`'s CHECK (migration 0012, widened by 0020 for
-- `collection_design`) did not admit `collection_item`, and neither did
-- `domain.BundleKind.IsValid`. So the mapping named a bundle that could
-- never be published: the reference existed and its target could not.
-- is the phase that makes the mapping resolvable end to end, so
-- this is where the kind becomes registrable.
-- ONE BUNDLE PER MODEL, and why that shape matters later
-- `01` §5.3 puts the asset at the Model level, so a bundle is per-Model.
-- That is what makes "DEV placeholder today → authored 3D asset later" a
-- pure DATA change: publish a new version under the same bundle id, or
-- point the Model's row at a different id. Neither touches
-- `collection_items`, the Collection Room domain, placement
-- data, Manual Search, or the recognition contract.
-- WHAT THIS MIGRATION DELIBERATELY DOES NOT ADD
-- No geometry, material, transform, scale, pivot, lighting or mounting
-- column, anywhere. All final visual work is deferred by product-owner
-- decision at; the authored asset carries its own content inside
-- the bundle, and the slot envelope an item is fitted inside belongs to the
-- Collection Design's authored layout. A column here
-- would be visual direction encoded in a schema, and would have to be
-- unwound when real art arrives.
-- Nor does it add any presentation metadata to the mapping. The
-- instruction allows it "only where structurally necessary" and, on
-- inspection, none is (item labels are /'s).

ALTER TABLE asset_bundles DROP CONSTRAINT asset_bundles_kind_check;

ALTER TABLE asset_bundles
    ADD CONSTRAINT asset_bundles_kind_check
    CHECK (kind IN ('museum_style', 'room_variant', 'sculpture', 'avatar',
                    'collection_design', 'collection_item'));

COMMENT ON COLUMN collection_models.asset_bundle_id IS
 'The Model to Presentation Asset mapping ( stored it; made it resolvable). '
    'Empty string means NO asset is mapped yet — an ordinary state, and the production state of '
    'every Model today. Points at an asset_bundles row of kind collection_item.';
