-- 0017_backfill_fixture_tier_count.sql
--: give the development fixture Design the tier count
-- its bundle actually authors.
-- WHY THIS NEEDS ITS OWN MIGRATION, which is worth stating because the
-- reasoning generalises to every future catalog metadata change.
-- Migration `0016` added `collection_designs.tier_count` with a default of
-- 1. The fixture Design row already existed (seeded at ), so it
-- took that default — and `PostgresCatalogRepository.EnsureSeeded` uses
-- `ON CONFLICT (id) DO NOTHING`, deliberately, so seeding never rewrites a
-- row that is already there. That property is what lets an operator add a
-- category or a Design directly without a restart quietly reverting it
-- (see `SeedCollectionCategories`). The cost is that a *change* to
-- already-seeded metadata cannot arrive through the seed, and has to
-- arrive as a migration instead.
-- So: an explicit, targeted backfill. Not a change to the seeding
-- strategy, because the DO NOTHING behaviour is correct and the
-- alternative (DO UPDATE) would let a restart overwrite operator edits.
-- Scoped to one known fixture id. It touches no product data — there is
-- none: `03` records the catalog of Collection design options as Open, so
-- the only row in this table is the development fixture.
UPDATE collection_designs
   SET tier_count = 3
 WHERE id = 'dev-fixture:collection-design'
   AND tier_count < 3;
