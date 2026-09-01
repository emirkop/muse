-- — Room Music: the curated music catalog (Presentation) and the
-- Room's assignment of one track from it.
-- closed as option (b): Room Music comes from a **Muse-curated,
-- properly licensed library**. Apple Music was not selected; Spotify was
-- not selected; owner-uploaded audio is not part of the current product.
-- research
-- records why: both streaming platforms' policies prohibit exactly the two
-- properties `01` §4.8 confirms — playback that starts on entry, and other
-- people hearing it.
-- `music_tracks` is a Presentation table in the same sense as
-- `museum_styles`, `room_variants`, and `sculptures`: Platform-owned,
-- identical for every user, no per-user data, no account/museum/room
-- reference in either direction, metadata only and never bytes
--.
-- **The production catalog ships EMPTY, and that is deliberate.** No track
-- may be represented as licensed unless a licence is actually confirmed,
-- and none is. `SeedMusicTracks` therefore returns nothing, exactly as
-- `SeedSculptures` does for. Everything around it is real — the
-- table, the endpoints, the foreign key, assignment/removal, delivery —
-- and populating the seed is the only change needed when licensed content
-- exists.

CREATE TABLE IF NOT EXISTS music_tracks (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
 -- Attribution/credit line a client must be able to display. Empty is
 -- allowed only for dev/test audio; a licensed track's licence will
 -- normally require a credit, and that text belongs with the track.
    attribution TEXT NOT NULL DEFAULT '',

 -- The licensing state of THIS track. Two values, and the distinction
 -- is a production-safety guard, not decoration:
 -- 'dev_test' Clearly-labelled non-production audio (e.g. a
 -- generated tone) used to exercise the pipeline. Safe
 -- because Muse produced it, and NEVER servable from a
 -- production deployment.
 -- 'licensed' A track whose licence has actually been confirmed by
 -- the business. Nothing may be inserted with this value
 -- on the strength of an assumption.
 -- There is deliberately no 'unknown' or 'pending': a row exists only
 -- once someone has decided which of these two it is.
    licensing TEXT NOT NULL CHECK (licensing IN ('dev_test', 'licensed')),

 -- Where the audio lives in object storage. A key, not a URL: the
 -- backend mints a short-lived presigned URL per request, exactly as
 -- it does for a photograph's bytes, so nothing durable or
 -- guessable is ever handed out.
    storage_key  TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'audio/mpeg',
 -- Duration in whole seconds, for a client that wants to show length
 -- or plan a loop. 0 means unknown.
    duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE music_tracks IS
    'Presentation catalog of curated, licensed music tracks (, option b). '
    'Platform-owned, no per-user data. Ships EMPTY: no track may be marked licensed until a '
    'licence is actually confirmed. Owner-uploaded audio is not part of the product.';

-- ---------------------------------------------------------------------
-- A Room's assigned track.
-- ---------------------------------------------------------------------
-- NULL means "no music", which `01` §4.8 confirms is a normal state
-- ("Music is optional — a Room/Collection Room may have none"), so this
-- is a nullable column rather than a separate table: a Room has at most
-- one track, and the assignment carries no data of its own.
-- ON DELETE RESTRICT for the same reason as `room_sculptures`: a catalog
-- entry that content still references may not be deleted. If a licence
-- ever lapses, the correct operation is a deliberate migration that
-- decides what happens to Rooms using it — not a silent cascade that
-- empties them.
ALTER TABLE rooms
    ADD COLUMN IF NOT EXISTS music_track_id TEXT
        REFERENCES music_tracks(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_rooms_music_track_id ON rooms (music_track_id);

COMMENT ON COLUMN rooms.music_track_id IS
 'Optional reference to a curated catalog track. NULL = no music, a normal state. '
 'A reference only — never a URL or key, so a Style/Variant change cannot disturb it.';
