-- 0008_create_password_credentials.sql
-- Email/password authentication — Muse's third identity
-- method, beside Apple and Google. See
-- §10 and the closed decisions / /.
-- Nothing in `accounts` or `external_identities` changes. A Muse password
-- is Muse's own credential, not an external provider's assertion, so it
-- gets its own table rather than being forced into the (provider,
-- subject) shape of `external_identities`.

-- ---------------------------------------------------------------
-- Password credentials
-- ---------------------------------------------------------------

CREATE TABLE IF NOT EXISTS password_credentials (
    -- One credential per account, so the account id *is* the key. An
    -- account may still have provider identities alongside it; what it
    -- may not have is two Muse passwords.
    account_id    UUID PRIMARY KEY REFERENCES accounts(id),
    -- Normalised (trimmed, lowercased) by domain.NormaliseEmail before it
    -- ever reaches here. UNIQUE so two password credentials can never
    -- claim the same address — enforced at the data layer, not only in
    -- application code.
    --: this uniqueness is scoped to *password credentials only*.
    -- It deliberately says nothing about `external_identities.email`, and
    -- there is no cross-table constraint between them: an address used
    -- for Sign in with Apple and an address used for a Muse password are
    -- allowed to be the same string on two different accounts. Adding a
    -- constraint here that spanned both tables would be the automatic
    -- email-linking behaviour forbids, expressed in DDL.
    email         TEXT NOT NULL UNIQUE,
    -- PHC-format Argon2id string: algorithm, version, parameters, salt,
    -- and digest in one value. Never a password, never anything
    -- reversible. Parameters live *inside* the hash so the work factor
    -- can be raised later and old hashes upgraded on next login without
    -- a mass reset.
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------
-- Pending sign-ups (: verify-first)
-- ---------------------------------------------------------------
-- A sign-up creates a row here, NOT an account. No `accounts` row, no
-- session, and nothing that can own a Museum exists until the
-- verification token is redeemed — which is exactly what "do not issue a
-- normal Muse session before email ownership is verified" means at the
-- storage layer. An abandoned sign-up therefore leaves no account behind.

CREATE TABLE IF NOT EXISTS pending_signups (
    -- Application-generated 128-bit hex id (not gen_random_uuid), so the
    -- row's identity is known before the insert.
    id            TEXT PRIMARY KEY,
    -- UNIQUE: signing up again for the same address replaces the
    -- outstanding attempt rather than queueing a second one. That is what
    -- makes "resending invalidates prior links" a property of the schema
    -- instead of a cleanup step someone can forget.
    email         TEXT NOT NULL UNIQUE,
    -- Already hashed. A pending sign-up is not a weaker holding pen than
    -- a live credential: it stores exactly what password_credentials
    -- stores.
    password_hash TEXT NOT NULL,
    -- SHA-256 digest of the verification token. The raw token exists only
    -- in the email — the same rule refresh tokens follow, so a database
    -- dump yields no usable token.
    token_digest  TEXT NOT NULL UNIQUE,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Single-use marker. Consumption is an UPDATE guarded on this being
    -- NULL, which is what makes two concurrent verifications of the same
    -- token resolve to exactly one winner.
    consumed_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_pending_signups_expires_at
    ON pending_signups (expires_at);

-- ---------------------------------------------------------------
-- Password resets
-- ---------------------------------------------------------------

CREATE TABLE IF NOT EXISTS password_resets (
    id           TEXT PRIMARY KEY,
    account_id   UUID NOT NULL REFERENCES accounts(id),
    token_digest TEXT NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    consumed_at  TIMESTAMPTZ
);

-- Supports "invalidate every outstanding reset for this account", which
-- runs after a successful reset so a second token mailed earlier cannot
-- be redeemed too.
CREATE INDEX IF NOT EXISTS idx_password_resets_account_id
    ON password_resets (account_id);

-- ---------------------------------------------------------------
-- Brute-force / abuse throttling
-- ---------------------------------------------------------------
-- Database-backed rather than in-process, deliberately: an in-memory
-- counter resets on every deploy and restart, which is a trivially
-- discoverable way to clear a lockout. This survives both.

CREATE TABLE IF NOT EXISTS auth_attempts (
    -- Which operation is being throttled (login, password_reset,
    -- verification_resend, signup). Separate scopes so a flood of reset
    -- requests cannot lock someone out of logging in.
    scope             TEXT NOT NULL,
    -- A DIGEST of whatever is being counted — an email address or a
    -- request source — never the value itself. The throttle table holds
    -- no plaintext addresses and no plaintext IPs, so it is not a
    -- secondary place a database dump reveals who uses Muse.
    key_digest        TEXT NOT NULL,
    window_started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    failure_count     INTEGER NOT NULL DEFAULT 0,
    -- Non-null while throttled.
    locked_until      TIMESTAMPTZ,
    PRIMARY KEY (scope, key_digest)
);

CREATE INDEX IF NOT EXISTS idx_auth_attempts_locked_until
    ON auth_attempts (locked_until);
