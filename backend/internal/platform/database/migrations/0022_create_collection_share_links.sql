-- 0022_create_collection_share_links.sql
-- — Collection Room share links.
-- A SEPARATE table from `share_links` (, Museum), deliberately.
-- `01` §5.1 requires Collection Rooms to be independent of the Museum and
-- brief requires "Museum and Collection sharing domain
-- records/entities independent" — so this shares the *mechanism* with
-- Museum sharing (an opaque 22-character code, exactly one active link per
-- subject, revoke-and-replace rotation, one indistinguishable refusal) and
-- shares no row, column, foreign key or constraint with it. There is no
-- reference from this table to `share_links`, `museums`, or `rooms`, and
-- none from those to this. The two trees still meet only at `accounts`
-- (via collection_rooms.account_id).
-- WHAT A LINK IS, AND IS NOT ( as closed by the product owner)
-- * Collection Rooms are OWNER-ONLY by default and have NO public or
-- discoverable mode. There is no privacy column here or on
-- collection_rooms — the test asserting that absence still
-- holds — because there is no privacy state: an active link is the
-- whole of what makes a Room reachable by anyone but its owner.
-- * The link is an active, REVOCABLE CAPABILITY. Possessing a valid code
-- AND being authenticated grants access to exactly that one Collection
-- Room, and nothing else: not another Collection Room, not the owner's
-- Museum, not any owner-only mutation.
-- * A Collection Room UUID alone is NEVER visitor authority. No visitor
-- route accepts a Room id; the visitor route accepts a code, resolves
-- it to a Room server-side, and re-checks the link is active on every
-- request.
-- * The code is stored in the clear, like a Museum code: it is a
-- discovery handle meant to be pasted and shown, and it grants nothing
-- to an unauthenticated caller.
-- "Exactly one active link per Collection Room" is the partial unique
-- index below — a database fact, so two concurrent creations cannot both
-- succeed and a regeneration that failed to revoke cannot commit.
-- ON DELETE CASCADE: a link can never outlive its Collection Room.
-- (account deletion) remains OPEN; the cascade is what every answer to it
-- requires.

CREATE TABLE collection_share_links (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    collection_room_id uuid        NOT NULL REFERENCES collection_rooms(id) ON DELETE CASCADE,
    code               text        NOT NULL,
    status             text        NOT NULL CHECK (status IN ('active', 'revoked')),
    created_at         timestamptz NOT NULL,
    revoked_at         timestamptz,
    CONSTRAINT collection_share_links_code_unique UNIQUE (code),
    CONSTRAINT collection_share_links_revoked_at_matches_status
        CHECK ((status = 'active' AND revoked_at IS NULL) OR (status = 'revoked' AND revoked_at IS NOT NULL))
);

CREATE UNIQUE INDEX collection_share_links_one_active_per_room
    ON collection_share_links (collection_room_id)
    WHERE status = 'active';

CREATE INDEX collection_share_links_room_id_idx ON collection_share_links (collection_room_id);
