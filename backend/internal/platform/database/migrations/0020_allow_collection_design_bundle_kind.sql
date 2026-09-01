-- 0020_allow_collection_design_bundle_kind.sql
-- close-out: let a `collection_design` bundle be registered.
-- A LATENT MUSE 59 DEFECT, found when the close-out first tried
-- to publish the development fixture Design through the real pipeline.
-- Migration 0012 created `asset_bundles.kind` with
-- `CHECK (kind IN ('museum_style', 'room_variant', 'sculpture', 'avatar'))`.
-- added `collection_design` to the Go `BundleKind` enum
-- and its `IsValid` — and to 's list
-- of kinds — but never widened this constraint. So the domain accepted a
-- Collection Design bundle and the database refused it, and no test or
-- development publish had ever registered one to notice (/60's
-- iOS suites decoded the committed layout file directly; the whole-stack
-- suites only ever published Room Variant bundles).
-- The fix is the one the constraint was always meant to have: the set of
-- kinds the backend will publish, as `domain.BundleKind.IsValid` defines
-- it. `collection_item` remains deliberately absent — recorded it
-- as unpublishable until per-Model assets exist, and a test
-- asserts that it still is.
-- Recorded in 's outcome and rather than
-- fixed silently.

ALTER TABLE asset_bundles DROP CONSTRAINT asset_bundles_kind_check;

ALTER TABLE asset_bundles
    ADD CONSTRAINT asset_bundles_kind_check
    CHECK (kind IN ('museum_style', 'room_variant', 'sculpture', 'avatar', 'collection_design'));
