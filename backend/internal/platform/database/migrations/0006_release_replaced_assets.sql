-- — Photo Replacement: the `released` Asset state.
-- Replacing a photograph dereferences its old Asset: the slot row now
-- points at the new one, and the old object in storage has no content
-- referring to it. §6/§9 fix how such
-- bytes go away — never inside the database transaction (a storage
-- round-trip must not run under the Room row lock, ), but as an
-- asynchronous follow-up "for the same deletion-cleanup process to
-- reclaim". That process (`ReclaimAbandonedUploads`, ) finds
-- its work by *state*, so a dereferenced-but-committed Asset needs a
-- state of its own or it is invisible to the sweep forever.
-- pending → committed → released → discarded
-- ↘ (abandoned) ↗
-- `released`: committed bytes that no content references any more. They
-- stay in storage until the sweep deletes them (bytes first, then the
-- row becomes `discarded` — the same order uses, so a failed
-- delete leaves a retryable row rather than an unreferenced object).
-- A released Asset is never served (download tickets require
-- `committed`), never re-committed, and never re-uploaded to.
-- Additive only: a new state value, a new nullable timestamp, and a
-- partial index for the sweep. No existing row changes meaning.

ALTER TABLE assets
    DROP CONSTRAINT assets_state_known;

ALTER TABLE assets
    ADD CONSTRAINT assets_state_known
    CHECK (state IN ('pending', 'committed', 'released', 'discarded'));

ALTER TABLE assets
    ADD COLUMN released_at TIMESTAMPTZ;

ALTER TABLE assets
    ADD CONSTRAINT assets_released_has_timestamp
    CHECK (state <> 'released' OR released_at IS NOT NULL);

-- The reclamation sweep's access path for released assets, mirroring
-- assets_pending_by_age.
CREATE INDEX IF NOT EXISTS assets_released_by_age
    ON assets (released_at)
    WHERE state = 'released';

COMMENT ON COLUMN assets.released_at IS
 'When content stopped referencing this committed asset ( replacement; '
    'deletion). Bytes remain until the reclamation sweep deletes them and the row becomes '
    'discarded. The (account_id, client_upload_id) live-uniqueness index still counts a released '
    'row as live: the storage key is not free until the row is discarded.';
