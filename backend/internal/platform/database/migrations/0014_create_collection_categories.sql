-- 0014_create_collection_categories.sql
--: the Collection Category registry.
-- §5.3 fixes the Collection Catalog's shape as
-- `Category → Brand → Model → Metadata → Optimized 3D Asset`, and
-- 's Collection Catalog section says that
-- catalog is "shared, non-per-user reference data — not owned by any
-- individual User, Museum, or Collection Room."
-- So Category is **Platform-owned presentation/reference data and lives
-- in the catalog context**, beside museum_styles, room_variants,
-- sculptures and music_tracks — not in the Collection content tree. This
-- table is deliberately the FIRST LEVEL ONLY of that structure. Brand,
-- Model, metadata, assets and search are 's; nothing here
-- presumes their shape beyond leaving room for a `category_id` foreign
-- key to point at.
-- Why a table rather than an enum in code or on the client: `03`'s
-- Categories section warns that `01`'s illustrative examples "must never
-- be hardcoded as the complete or permanent category list", and the
-- product requirement for this phase is that adding a future category
-- must not require an iOS app release. A row is the answer to both.

CREATE TABLE IF NOT EXISTS collection_categories (
    -- A stable, human-readable id, following the convention the rest of
    -- the catalog uses (`style_modern`, `sculpture_…`, `track_…`). Stable
    -- because Collection Rooms reference it for their whole lifetime and
    -- a rename must never orphan one — display_name is what changes when
    -- wording changes.
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    -- The order the picker presents them in (`02`'s Collection Category
    -- Selection shows cards, which need a deterministic order that is not
    -- alphabetical-by-accident). Presentation, not meaning.
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_collection_categories_sort_order
    ON collection_categories (sort_order, id);

-- ---------------------------------------------------------------
-- Make collection_rooms.category_id a REAL reference
-- ---------------------------------------------------------------
-- recorded the category as free text with no foreign key,
-- because there was no table to point at and `03` forbade inventing the
-- vocabulary. gives it one, so "you cannot reference a category
-- that does not exist" becomes a database fact rather than an
-- application check — the same discipline applied when
-- `room_sculptures.catalog_id` became a real FK.
-- SAFETY FOR EXISTING ROWS. Any row may hold an arbitrary
-- string, since nothing validated it. Those values are cleared to NULL
-- below rather than guessed at: mapping `'watches'` onto
-- `'category_watches'` would be fabricating category data for a row
-- whose author never chose from a real list. Nothing is deployed, so the
-- only rows this can touch are development ones.
-- Note the ordering consequence, stated rather than hidden: this
-- migration runs BEFORE the application seeds the four categories
-- (seeding lives in catalog/domain.SeedCollectionCategories, applied by
-- PostgresCatalogRepository.EnsureSeeded at start-up, per the
-- convention that migrations own schema and Go owns catalog content).
-- The table is therefore empty at this point and EVERY pre-existing
-- category_id is cleared. On a fresh database there are no rows and this
-- is a no-op.
UPDATE collection_rooms
   SET category_id = NULL,
       updated_at = now()
 WHERE category_id IS NOT NULL
   AND NOT EXISTS (
       SELECT 1 FROM collection_categories c WHERE c.id = collection_rooms.category_id
   );

-- The column stays NULLABLE. A Room legitimately has no
-- category (its creation flow did not ask for one), and NOT NULL would
-- mean either failing this migration or inventing a value for it.
-- *API* requires a category on creation — which is where
-- `02`'s flow actually supplies one — so every Room created from now on
-- has a real category, and the nullable column carries only history.
-- ON DELETE RESTRICT: removing a category that Collection Rooms
-- reference must be refused, not silently cascade a Room into having no
-- category. Same posture as `rooms.music_track_id`.
ALTER TABLE collection_rooms
    ADD CONSTRAINT collection_rooms_category_fk
    FOREIGN KEY (category_id) REFERENCES collection_categories(id)
    ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_collection_rooms_category_id
    ON collection_rooms (category_id);
