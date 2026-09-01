-- 0001_create_accounts.sql
-- The first real Muse schema: accounts and the external identities
-- linked to them (/, — ).
-- No Museum, Room, or Collection table exists here or anywhere yet —
-- out of scope for this phase.

CREATE TABLE IF NOT EXISTS accounts (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Soft delete: a non-null deleted_at marks a deactivated account.
    -- Rows are never hard-deleted by this phase's code — real deletion
    -- semantics (data export, cascade behavior) remain an explicitly
    -- open product decision.
    deleted_at   TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS external_identities (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id     UUID NOT NULL REFERENCES accounts(id),
    provider       TEXT NOT NULL,
    subject        TEXT NOT NULL,
    email          TEXT NOT NULL DEFAULT '',
    email_verified BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Database-enforced external identity uniqueness: the same
    -- (provider, subject) pair can never be linked to two different
    -- accounts — enforced here, not only in application code, per
    -- §19.
    UNIQUE (provider, subject)
);

CREATE INDEX IF NOT EXISTS idx_external_identities_account_id
    ON external_identities (account_id);
