-- — Museum share links.
-- A share link is a discovery handle for a Museum, stored in the clear:
-- it is meant to be pasted and shown, and it grants nothing on its own —
-- every read through it re-checks that the link is active AND the Museum
-- is Public on that request. Unlike refresh/reset tokens, there
-- is therefore nothing to digest.
-- "Exactly one active link per Museum" is the partial unique index below,
-- so it is a database fact rather than an application promise: two
-- concurrent creations cannot both succeed, and a regeneration that
-- failed to revoke the previous link cannot commit.
-- ON DELETE CASCADE: (what happens to links when an account is
-- deleted) is still OPEN; the cascade only guarantees a link can never
-- outlive its Museum row, which every answer to requires.

CREATE TABLE share_links (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    museum_id   uuid        NOT NULL REFERENCES museums(id) ON DELETE CASCADE,
    code        text        NOT NULL,
    status      text        NOT NULL CHECK (status IN ('active', 'revoked')),
    created_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    CONSTRAINT share_links_code_unique UNIQUE (code),
    CONSTRAINT share_links_revoked_at_matches_status
        CHECK ((status = 'active' AND revoked_at IS NULL) OR (status = 'revoked' AND revoked_at IS NOT NULL))
);

CREATE UNIQUE INDEX share_links_one_active_per_museum
    ON share_links (museum_id)
    WHERE status = 'active';

CREATE INDEX share_links_museum_id_idx ON share_links (museum_id);
