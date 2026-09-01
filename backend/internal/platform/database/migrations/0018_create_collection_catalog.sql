-- 0018_create_collection_catalog.sql
--: the Collection Catalog's Brand and Model levels, and Manual
-- Search's index.
-- `01` §5.3 fixes the structure as
-- `Category → Brand → Model → Metadata → Optimized 3D Asset`.
-- built the Category level (`collection_categories`, ); this adds
-- the two beneath it.
-- Platform-owned reference data throughout: `04`'s Collection Catalog
-- section is explicit that the catalog is "shared, non-per-user reference
-- data — not owned by any individual User, Museum, or Collection Room."
-- **No table here carries an account, museum, room, or collection-room
-- column**, and none ever may.
-- WHAT IS DELIBERATELY ABSENT, and why:
-- * No population/submission/versioning mechanism. (how the
-- catalog is populated, kept current, and versioned, and whether
-- users may request missing models) is **OPEN**, and its own note
-- says content authoring "starts manually". Its three candidate
-- options differ in *process*, not in these tables — only options (b)
-- and (c) would add anything schema-visible (a submission/request
-- table), and building that now would answer an open research
-- question. `02`'s "path to request/report a missing model" is
-- explicitly "(mechanism TBD)", so the empty state says so honestly
-- rather than offering a mechanism that does not exist.
-- * No category-specific columns. `01` §5.3's "Metadata" is one JSONB
-- column that nothing branches on — see below.
-- * No recognition/ML column of any kind. is OPEN and phase
-- 63 is its research gate.

-- ---------------------------------------------------------------
-- Brand
-- ---------------------------------------------------------------
-- **Not nested under Category**, which is the one structural point worth
-- pausing on. `01` §5.3 writes the chain as `Category → Brand → Model`,
-- which reads like nesting — but a brand can legitimately span verticals
-- (a manufacturer making both watches and die-cast cars), and nesting
-- would force a duplicate Brand row per category with no way to tell that
-- the two are the same company.
-- So Brand is its own top-level platform entity and the **Model** is the
-- join point, carrying both a Brand and a Category. That is the product
-- owner's instruction ("Brand as platform catalog data", "Model
-- belonging to Brand + Category") and it preserves `01`'s chain as a
-- *navigation* path rather than a containment hierarchy.
CREATE TABLE IF NOT EXISTS collection_brands (
    -- Stable and human-readable, following the catalog's convention
    -- (`category_watches`, `design_…`). Stable because a Model references
    -- it for its whole lifetime; display_name is what changes if wording
    -- does.
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    -- Presentation order for any future brand-browsing surface. Carries
    -- no meaning about the brand.
    sort_order   INTEGER NOT NULL DEFAULT 0,
    -- Development fixture vs. real authored content, with exactly two
    -- values and no third that could pass for production — the discipline
    -- `music_tracks.licensing` and `collection_designs.classification`
    -- already established. Enforced, not decorative: the
    -- catalog service refuses to serve `dev_fixture` rows when
    -- APP_ENV=production.
    classification TEXT NOT NULL DEFAULT 'production',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT collection_brands_classification_valid
        CHECK (classification IN ('dev_fixture', 'production'))
);

CREATE INDEX IF NOT EXISTS idx_collection_brands_sort_order
    ON collection_brands (sort_order, id);

-- ---------------------------------------------------------------
-- Model — the level a Collection Item actually references
-- ---------------------------------------------------------------

CREATE TABLE IF NOT EXISTS collection_models (
    id          TEXT PRIMARY KEY,

    -- The join point described above. Both are real foreign keys, so a
    -- Model cannot reference a brand or a vertical that does not exist.
    -- ON DELETE RESTRICT on each: retiring a brand or a category that
    -- Models reference is a content decision with data consequences, not
    -- a DELETE.
    brand_id    TEXT NOT NULL REFERENCES collection_brands(id) ON DELETE RESTRICT,
    category_id TEXT NOT NULL REFERENCES collection_categories(id) ON DELETE RESTRICT,

    display_name TEXT NOT NULL,

    -- The text Manual Search matches against, authored alongside the
    -- Model.
    -- Denormalised on purpose, and the reason is worth stating: a
    -- PostgreSQL generated column cannot read another table, so a tsvector
    -- derived from "brand name + model name" has to be fed from a column
    -- on this row. Making that column explicit — authored, not inferred —
    -- means the catalog author decides what a Model is findable by
    -- (`02`'s example is a user typing a brand and a model together), and
    -- it keeps the search index a pure function of one row.
    search_text TEXT NOT NULL,

    -- `01` §5.3's "Metadata" level, as one opaque document.
    -- JSONB rather than columns because the fields differ by vertical (a
    -- watch has a movement, a coin has a mint year) and **no
    -- category-specific behaviour is in scope** — the instruction
    -- excludes it explicitly. Nothing in the backend reads, validates, or
    -- branches on this: it is carried and served. That is what makes it
    -- "suitable for future category-specific fields" without presuming
    -- any of them.
    -- NOT to be confused with (owner-authored personal metadata
    -- on a Collection *Item*), which is OPEN and belongs to content, not
    -- here. This is the catalog's own description of a product; that would
    -- be a user's note about their copy of it.
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- `01` §5.3's "Optimized 3D Asset reference", through the existing
    -- delivery system.
    -- **Deliberately nullable-by-emptiness rather than required**: a Model
    -- may exist before its asset is authored. `02`'s Manual Search selects
    -- a Model and only *then* routes to "Downloading/Loading the Relevant
    -- 3D Asset", so selection cannot depend on the asset existing — which
    -- is exactly Verify item 7.
    asset_bundle_id      TEXT NOT NULL DEFAULT '',
    asset_bundle_version INTEGER NOT NULL DEFAULT 1,

    classification TEXT NOT NULL DEFAULT 'production',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT collection_models_classification_valid
        CHECK (classification IN ('dev_fixture', 'production')),

    -- The full-text index Manual Search filters on. `04` Part G requires
    -- the catalog support "full-text/structured search"; a stored
    -- generated tsvector plus a GIN index is that, indexed from the first
    -- row rather than after the table becomes the scale-sensitive one
    -- `04`'s Scalability Risks section warns it will be.
    -- `'simple'` rather than a language configuration on purpose: product
    -- names are proper nouns, and stemming "Nautilus" or dropping a
    -- model number as a stopword would lose matches. A catalog is not
    -- prose.
    search_document tsvector GENERATED ALWAYS AS
        (to_tsvector('simple', coalesce(search_text, ''))) STORED
);

CREATE INDEX IF NOT EXISTS idx_collection_models_search
    ON collection_models USING GIN (search_document);
-- The scoped browse/paginate path: every search is scoped to one category
-- (`02`: "Search interface scoped to the Collection Room's category"), and
-- results are ordered deterministically by (display_name, id).
CREATE INDEX IF NOT EXISTS idx_collection_models_category_order
    ON collection_models (category_id, display_name, id);
CREATE INDEX IF NOT EXISTS idx_collection_models_brand
    ON collection_models (brand_id);

-- ---------------------------------------------------------------
-- Make collection_items.catalog_model_id a REAL reference
-- ---------------------------------------------------------------
-- recorded the Model reference as free text with no foreign key,
-- because there was no Model table to point at, and every phase since has
-- deliberately withheld an item write path for exactly that reason. Now
-- there is a table, so **Verify item 4 ("unknown Model ids cannot become
-- valid catalog references") becomes a database fact** — the same step
-- migration 0014 took for category_id and 0015 for design_id, and the one
-- took for room_sculptures.catalog_id.
-- SAFETY FOR EXISTING ROWS, and why deletion is provably safe here rather
-- than merely convenient: `catalog_model_id` is NOT NULL, so unresolvable
-- values cannot be nulled the way 0014 and 0015 nulled theirs. They have
-- to go. What makes that safe is not an assumption but a structural fact —
-- **no HTTP route has ever accepted a `catalog_model_id`**. phases 57
-- through 60 each withheld the item write path on the grounds that the
-- reference could not be validated, so every row in this table was
-- inserted by a test or by hand. There is no user content to lose.
DELETE FROM collection_items
 WHERE NOT EXISTS (
     SELECT 1 FROM collection_models m WHERE m.id = collection_items.catalog_model_id
 );

-- ON DELETE RESTRICT: retiring a Model that people have in their
-- collections must be refused, not silently empty their rooms.
ALTER TABLE collection_items
    ADD CONSTRAINT collection_items_model_fk
    FOREIGN KEY (catalog_model_id) REFERENCES collection_models(id)
    ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_collection_items_model_id
    ON collection_items (catalog_model_id);
