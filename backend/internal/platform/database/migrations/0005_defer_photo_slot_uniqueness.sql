-- — photo reordering persistence.
-- room_photo_slots_unique_slot — UNIQUE (room_id, slot_index) — is the
-- data-layer half of the 28-photo cap and must stay. But PostgreSQL
-- checks a non-deferrable unique constraint **per row, as each row is
-- modified**, not at the end of the statement. Reordering therefore
-- fails even as a single set-based UPDATE: the first row to take an
-- index still held by an as-yet-unmodified row violates the constraint
-- mid-statement. (Verified against PostgreSQL 16 before this migration
-- was written for the probe.)
-- The fix keeps the invariant exactly as strong and moves only *when* it
-- is checked, and only for transactions that ask:
-- DEFERRABLE INITIALLY IMMEDIATE
-- INITIALLY IMMEDIATE means every statement in every transaction behaves
-- exactly as before — a naive two-UPDATE swap still fails, appends still
-- collide on a duplicate index. Only a transaction that explicitly runs
-- `SET CONSTRAINTS room_photo_slots_unique_slot DEFERRED` has the check
-- moved to COMMIT — where a genuinely duplicate final state is still
-- rejected and the whole transaction rolls back. The reorder path is the
-- only caller that defers (PostgresMuseumRepository.ReorderPhotoSlots).
-- Nothing references (room_id, slot_index) as a foreign key and no code
-- uses it for ON CONFLICT inference, which are the two things a
-- deferrable unique constraint cannot serve.

ALTER TABLE room_photo_slots
    DROP CONSTRAINT room_photo_slots_unique_slot;

ALTER TABLE room_photo_slots
    ADD CONSTRAINT room_photo_slots_unique_slot
    UNIQUE (room_id, slot_index) DEFERRABLE INITIALLY IMMEDIATE;

COMMENT ON CONSTRAINT room_photo_slots_unique_slot ON room_photo_slots IS
    'One photograph per logical slot per Room. DEFERRABLE INITIALLY IMMEDIATE so a '
    'reorder transaction can defer the check to COMMIT; every other statement '
    'is checked per row exactly as before.';
