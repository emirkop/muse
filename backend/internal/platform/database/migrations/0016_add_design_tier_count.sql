-- 0016_add_design_tier_count.sql
--: how many tiers a Collection Design authors.
-- **This is a BOUND, not a capacity table, and the difference is the whole
-- point of this migration being three lines instead of a new table.**
-- (the real per-Design capacities) is still OPEN, and the phase
-- 60 instruction is explicit that *the Design bundle supplies authored
-- cumulative capacity per tier* — matching `04`'s Collection Room
-- Expansion, where the presentation side "defines, per tier, the
-- additional geometry to reveal and the slot→transform table for the
-- newly available slots". So the capacities live in the Design's bundle
-- (`layout.json`'s `tiers` array) and the database holds none of them.
-- What the database does need is a ceiling. `current_tier` is content the
-- server persists, and a client asks the server to ratchet it (the client
-- is the only side that can read the bundle's capacities). Without a bound
-- the server would have nothing to check that request against. With one it
-- can refuse a tier the Design does not author, while still knowing
-- nothing about what any tier holds.
-- That is the same split made for the Museum: the server holds
-- the *rule* it can enforce and the bundle holds the table it cannot see.
-- The honest consequence, recorded in rather than hidden: the
-- server cannot verify that an item count actually justifies a requested
-- tier. What it does guarantee is monotonic, bounded, atomic and
-- owner-only — every property it is in a position to know.

ALTER TABLE collection_designs
    ADD COLUMN tier_count INTEGER NOT NULL DEFAULT 1;

ALTER TABLE collection_designs
    ADD CONSTRAINT collection_designs_tier_count_positive CHECK (tier_count >= 1);

-- A Design with one tier is legitimate: a Collection Room whose space
-- never grows is one with a single authored tier, and `1` is therefore the
-- right default for a row that predates this column. It also keeps
-- "one tier" from being a special case anywhere in the code.
COMMENT ON COLUMN collection_designs.tier_count IS
    'How many tiers this Design authors. A BOUND for the tier ratchet, never a capacity — '
    'capacities are authored in the Design''s asset bundle ( remains open).';
