-- 0013_create_collection_rooms.sql
--: the Collection content tree.
-- §5.1 forbids treating a Collection Room as a
-- Room type inside the Museum, and 's
-- Domain Layer states the two trees are "structurally independent...
-- They share identity (User, Avatar), sharing mechanics, and music
-- mechanics, but not data models, layout systems, or growth logic."
-- That independence is expressed here as three concrete facts, not as a
-- convention:
-- 1. `collection_rooms.account_id` references accounts(id) DIRECTLY.
-- There is no museum_id column and no path from a Collection Room
-- to a Museum. A Collection Room is a top-level, User-owned space.
-- 2. No table in this file references, or is referenced by, `museums`,
-- `rooms`, `room_photo_slots`, or `room_sculptures`. The two trees
-- meet only at `accounts`.
-- 3. Every shape that looks superficially similar to the Museum tree
-- is deliberately DIFFERENT where the product differs — see the
-- three "the inverse of" comments below. Where the Museum caps,
-- Collection grows.
-- Deliberately ABSENT columns, each because a decision is open. Adding
-- any of them now would be choosing an answer silently:
-- * privacy — (do Collection Rooms have Public/Private
-- states at all, or is link possession the only
-- access control?) is OPEN and blocks. `03`
-- Collection Privacy: "must not be assumed to mirror
-- Museum Room privacy without confirmation."
-- * music_track_id — Collection Room music is + ('s
-- entry names it), and `01` §5.4 mirrors the Museum
-- model. Not this phase's to add.
-- * owner notes on an item — (owner-authored personal
-- metadata) is OPEN and blocks.

-- ---------------------------------------------------------------
-- Collection Room — content, owned directly by an Account
-- ---------------------------------------------------------------

CREATE TABLE IF NOT EXISTS collection_rooms (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- THE INVERSE OF `museums.account_id`, which is UNIQUE because the
    -- 1:1 rule is confirmed. Here there is deliberately **no UNIQUE
    -- constraint**: `01` §5.1/§8.1 and `03` Unlimited Collection Rooms
    -- confirm unlimited Collection Rooms per account, and 's
    -- brief is explicit that no artificial per-user count limit may be
    -- introduced. Capacity gating, if any, is /and
    -- concerns ITEMS, not room count (`02` Collection Room Creation:
    -- "Collection Room creation itself should not be blocked by
    -- capacity limits").
    account_id UUID NOT NULL REFERENCES accounts(id),
    -- Free text, same lightweight pattern as Room naming (`02`
    -- Collection Room Creation step 2 says exactly that). is
    -- OPEN for Museum Room names and no rule is invented here either.
    name       TEXT NOT NULL DEFAULT '',
    -- The single category this Collection Room is scoped to (`01` §5.3,
    -- `02` Collection Category Selection: "one category per Collection
    -- Room"). NULLABLE, and no foreign key — on purpose:
    -- The category vocabulary is the top level of the Collection Catalog
    -- (`Category → Brand → Model`), which builds. `03`
    -- Categories warns the four illustrative examples "must never be
    -- hardcoded as the complete or permanent category list", so no
    -- enumeration is created here, and with no categories table there is
    -- nothing to reference. The reference is therefore recorded but NOT
    -- referentially validated until — a real, named gap, not a
    -- silent one.
    category_id TEXT,
    -- The active Collection Room Design (its own design system, distinct
    -- from Museum Styles — `01` §5.1, `03` Collection Room Designs).
    -- NULLABLE with no foreign key for the same reason: the Design
    -- catalog is 's, and `03` records that the catalog of
    -- available Designs is not defined anywhere in `01`.
    -- A *reference only*. No geometry, material, or transform ever lands
    -- in a content table — the same rule that makes `02`'s Museum Style
    -- Changing a pure reference swap.
    design_id  TEXT,
    -- The expansion tier this Collection Room has grown to (`04`
    -- Collection Room Expansion: "Content side: a Collection Room record
    -- tracks its current tier"). A plain ordinal with a floor and NO
    -- ceiling: how many tiers exist, how much each adds, and at what
    -- item count the next one unlocks are all properties of the active
    -- Design's authored geometry (, OPEN — per-Design tier
    -- tables, "not a single global answer"), delivered in its asset
    -- bundle. Putting a threshold here would be inventing that answer.
    current_tier INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT collection_rooms_tier_positive CHECK (current_tier >= 1)
);

CREATE INDEX IF NOT EXISTS idx_collection_rooms_account_id ON collection_rooms (account_id);

-- ---------------------------------------------------------------
-- Collection Item — content, owned by a Collection Room
-- ---------------------------------------------------------------

-- `04` Collection Room Expansion: the ordered item slot assignments are
-- "same shape as Room photo slots, just **without a fixed 28-slot
-- ceiling**." That sentence is the whole design of this table.
CREATE TABLE IF NOT EXISTS collection_items (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    collection_room_id UUID NOT NULL REFERENCES collection_rooms(id) ON DELETE CASCADE,
    -- A *logical* display slot — placement order, never a coordinate
    -- (`04`'s Logical Slot to Transform Mapping, which `04` says
    -- Collection Item placement follows). `02`'s 3D Collection Item
    -- Display places each item in "the next available deterministic
    -- display slot", so these are contiguous from 0.
    -- THE INVERSE OF `room_photo_slots_index_within_cap`, which bounds
    -- the index 0..27 to make 28 the arithmetic maximum. Here the CHECK
    -- has a floor and no ceiling: Collection Rooms "grow with the
    -- collection" (`01` §5.2) and there is no confirmed item cap.
    -- Whatever capacity gating monetization eventually applies is
    -- /'s, enforced where entitlements are known — not
    -- frozen into this schema.
    slot_index         INTEGER NOT NULL,
    -- Reference into the Collection Catalog's Model level (`04`
    -- Collection Catalog: "A Collection Item... references a specific
    -- catalog Model by ID; it does not duplicate the model's metadata or
    -- geometry"). No foreign key yet, for the same reason as
    -- category_id: the catalog is 's.
    -- Because it cannot be validated referentially, exposes NO
    -- endpoint that accepts one. See internal/collection/doc.go.
    catalog_model_id   TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT collection_items_slot_index_non_negative CHECK (slot_index >= 0),
    -- DEFERRABLE from the outset, unlike room_photo_slots, which needed
    -- migration 0005 to fix this after the fact. `02`'s
    -- Collection Item Reordering is an explicit swap-on-drop, so phase
    -- 61 will need to move two items onto indices each other currently
    -- holds.
    -- The exact semantics, re-probed against this database at
    -- rather than assumed from 's prose, because the difference
    -- decides how has to be written:
    -- * NON-deferrable UNIQUE is checked PER ROW as each row is
    -- written. Even a single set-based UPDATE swapping two indices
    -- fails, because the first row written takes an index the
    -- not-yet-written row still holds. This is 's finding
    -- and it is correct.
    -- * DEFERRABLE INITIALLY IMMEDIATE is checked at END OF STATEMENT.
    -- So a swap written as ONE statement succeeds with no
    -- SET CONSTRAINTS at all, while a swap split across two
    -- statements in one transaction still fails.
    -- * SET CONSTRAINTS ... DEFERRED moves the check to COMMIT, which
    -- is what a multi-statement swap needs.
    -- The invariant is not weakened either way: any statement (or, when
    -- deferred, any transaction) that ends with two items on one slot is
    -- still rejected and rolled back. What changes is only WHEN the
    -- check runs, never whether it does. All four behaviours are pinned
    -- by tests in postgres_collection_room_repository_test.go.
    CONSTRAINT collection_items_unique_slot UNIQUE (collection_room_id, slot_index)
        DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX IF NOT EXISTS idx_collection_items_room_id ON collection_items (collection_room_id);
