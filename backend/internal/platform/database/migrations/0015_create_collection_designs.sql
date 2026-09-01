-- 0015_create_collection_designs.sql
--: the Collection Design registry.
-- `01` §5.1 confirms a Collection Room "has its own design choices and
-- preview experience, **distinct from Museum styles/previews**", and
-- `04`'s Domain Layer lists "Collection Room Design definition" among
-- the Presentation Models that own "geometry references, materials,
-- lighting rigs... asset bundle identifiers and versions" and "carry no
-- per-user data".
-- So a Design is Platform-owned presentation living in the catalog
-- context, beside museum_styles / room_variants / sculptures /
-- music_tracks / collection_categories. It is NOT a Museum Style with a
-- different name: no column here references a Style or a Variant, and
-- the two are separate tables, separate Go types, and separate Swift
-- types with nothing shared but delivery vocabulary.
-- (CLOSED at, option (c)) decides the one relationship
-- this table has to express — see category_id below.

CREATE TABLE IF NOT EXISTS collection_designs (
    -- Stable, human-readable, following the catalog's convention
    -- (`style_modern`, `category_watches`, `track_…`). Stable because a
    -- Collection Room references it for its whole lifetime; display_name
    -- is what changes when wording changes.
    id           TEXT PRIMARY KEY,

    -- option (c), the whole decision in one nullable column:
    -- NULL → UNIVERSAL. Offered to a Collection Room of
    -- every category.
    -- <a real category> → offered ONLY to Collection Rooms of that
    -- category.
    -- Nullable rather than NOT NULL because that is what the product
    -- documents actually say: `02`'s Collection Room Design Selection
    -- asks for designs "category-appropriate **where relevant**", and
    -- `04` Collection Assets calls them "per-category display
    -- environments" — the first is optional scope, the second is strict,
    -- and (c) is the shape that satisfies both. It also degenerates
    -- cleanly: an authored catalog that turns out entirely
    -- category-specific behaves as strict scoping, one entirely neutral
    -- behaves as global, decided by DATA rather than by schema.
    -- Many-to-many is deliberately NOT modelled (no join table).
    -- records the standing instruction: if authored Designs
    -- genuinely need a subset of several categories, that is a NEW
    -- decision, never a silent schema extension.
    -- ON DELETE RESTRICT: removing a category that Designs are scoped to
    -- must be refused, not silently promote those Designs to universal.
    category_id  TEXT REFERENCES collection_categories(id) ON DELETE RESTRICT,

    display_name TEXT NOT NULL,

    -- DEV FIXTURE vs PRODUCTION, with exactly two values and no third.
    -- The same two-state discipline `music_tracks.licensing` uses
    --: there is deliberately no "unknown" or "pending" state
    -- that could be mistaken for production content. A row exists here
    -- because someone decided which of these it is.
    -- This is not merely a label. `catalog/application.CollectionDesignService`
    -- refuses to serve or accept a `dev_fixture` Design when
    -- APP_ENV=production, so fixture content cannot reach a production
    -- deployment even if a client knows its id — the same guard
    -- applies to `dev_test` audio.
    classification TEXT NOT NULL,

    -- The versioned bundle in object storage, through the EXISTING phase
    -- 53 delivery system — same two columns museum_styles and
    -- room_variants carry, so there is one asset pipeline and not two.
    -- Content stores only `design_id` and never a bundle reference
    --, which is why re-pointing a Design at new artwork later
    -- is an UPDATE of these two columns and touches no Collection Room
    -- row and no Collection Room schema.
    asset_bundle_id      TEXT NOT NULL DEFAULT '',
    asset_bundle_version INTEGER NOT NULL DEFAULT 1,

    -- Presentation order for `02`'s design cards. Presentation only.
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT collection_designs_classification_valid
        CHECK (classification IN ('dev_fixture', 'production'))
);

CREATE INDEX IF NOT EXISTS idx_collection_designs_category_id
    ON collection_designs (category_id);
CREATE INDEX IF NOT EXISTS idx_collection_designs_sort_order
    ON collection_designs (sort_order, id);

-- ---------------------------------------------------------------
-- Make collection_rooms.design_id a REAL reference
-- ---------------------------------------------------------------
-- recorded the Design as free text with no foreign key, because
-- there was no table to point at and `03` records the catalog of Design
-- options as undefined. gives it one, so "you cannot reference
-- a Design that does not exist" becomes a database fact — the same step
-- migration 0014 took for category_id, and took for
-- room_sculptures.catalog_id.
-- SAFETY FOR EXISTING ROWS, identical to 0014's reasoning: any
-- row may hold an arbitrary unvalidated string, and those values are
-- cleared to NULL rather than guessed at — mapping `'display-case'` onto
-- some real Design would be fabricating a choice for a row whose author
-- never picked from a real list. Migrations run before the application
-- seeds the registry, so the table is empty here and every pre-existing
-- design_id is cleared; on a fresh database there are no rows and this
-- is a no-op.
UPDATE collection_rooms
   SET design_id = NULL,
       updated_at = now()
 WHERE design_id IS NOT NULL
   AND NOT EXISTS (
       SELECT 1 FROM collection_designs d WHERE d.id = collection_rooms.design_id
   );

-- The column stays NULLABLE, and unlike category_id this is not only
-- about history: a Collection Room legitimately exists before a Design
-- is chosen (`02`'s alternate flow keeps a Room abandoned mid-creation,
-- and creates Rooms with no Design at all), and `03` records
-- that a Design is changeable later. Clearing a Design back to NULL
-- therefore remains a legal operation — the deliberate difference from
-- category, which `02` fixes at exactly one per Collection Room.
-- ON DELETE RESTRICT: retiring a Design that Rooms are using must be
-- refused rather than silently leaving those Rooms design-less.
ALTER TABLE collection_rooms
    ADD CONSTRAINT collection_rooms_design_fk
    FOREIGN KEY (design_id) REFERENCES collection_designs(id)
    ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_collection_rooms_design_id
    ON collection_rooms (design_id);
