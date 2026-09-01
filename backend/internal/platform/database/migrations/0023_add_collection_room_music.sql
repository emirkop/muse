-- — Collection Music: a Collection Room's assignment of one track
-- from the curated music catalog.
-- `01` §5.4: "Collection Rooms may have optional Room music, chosen by the
-- owner." `01` §4.8's rules apply identically — plays on entry, locally
-- toggleable by everyone present, assignable only by the owner — and `02`'s
-- Collection Room Music flow is "identical in shape to Room Music".
-- **The catalog is shared; the assignment is not.** `music_tracks` is
-- Platform-owned presentation data with no
-- per-user data and no reference to any content row, so both content trees
-- may reference it — exactly as both reference `collection_categories` or
-- `museum_styles` in their own way. What is NOT shared is the assignment:
-- this column lives on `collection_rooms`, `rooms.music_track_id` lives on
-- the Museum tree, and no row in either tree references the other
-- (`01` §5.1, proven by `cmd/api/collection_independence_test.go`).
-- NULL means "no music", a normal state (`01` §4.8; `03`: "Music is optional
-- — a Room/Collection Room may have none"), so this is a nullable column
-- rather than a table — the same shape as migration `0011`.
-- ON DELETE RESTRICT for the same reason as `rooms.music_track_id`: a
-- catalog track that content still references may not be deleted. If a
-- licence lapses, the correct operation is a deliberate migration that
-- decides what happens to the Rooms using it, not a silent cascade.
-- **The production catalog still ships EMPTY** (`SeedMusicTracks` returns
-- nothing), so on a production deployment every Collection Room assignment
-- is refused `unknown_music_track` until a licensed track exists — the
-- honest consequence of, not a defect.

ALTER TABLE collection_rooms
    ADD COLUMN IF NOT EXISTS music_track_id TEXT
        REFERENCES music_tracks(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_collection_rooms_music_track_id ON collection_rooms (music_track_id);

COMMENT ON COLUMN collection_rooms.music_track_id IS
 'Optional reference to a curated catalog track (; the catalog). NULL = no music, a normal state. '
    'A reference only — never a URL or key — and independent of rooms.music_track_id: the two trees share the catalog, not the assignment.';
