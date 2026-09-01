-- 0009_create_email_outbox.sql
-- Transactional email outbox (, Option A).
-- Why this table exists: POST /auth/email/password-reset and
-- POST /auth/email/verification/resend answer an identical 202 whether or
-- not the address is known. But delivering the email *inside* the request
-- meant a known address also paid an outbound HTTP call to the email
-- provider before responding — tens to hundreds of milliseconds an unknown
-- address never paid. Response TIME leaked what the response BODY hid.
-- With this table both paths do the same thing on the request path: one
-- INSERT here, then 202. Existence is resolved later, by the drainer,
-- after the response has already gone out.
-- What this table deliberately does NOT contain: a token. Tokens are
-- generated at DEQUEUE, in the drainer, at the moment the email is built
-- — never at enqueue. That is what keeps the digest-only rule intact: the
-- raw token exists in drainer memory and in the email, and only its digest
-- ever reaches `password_resets` / `pending_signups`. A dump of this table
-- yields no usable link.

CREATE TABLE IF NOT EXISTS email_outbox (
    -- Application-generated 128-bit hex id, known before the insert.
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,
    -- The normalised recipient/lookup address. Already stored in plaintext
    -- for known addresses (`password_credentials.email`,
    -- `pending_signups.email`), so no new exposure class for them. For an
    -- UNKNOWN address this is the only place it is ever written, which is
    -- why a completed job is DELETED (not marked) and a dead-lettered job
    -- has this column scrubbed to '': a stranger's typed address is held
    -- only for the seconds between enqueue and drain.
    email           TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    -- Incremented on every claim, so it counts attempts *started*.
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A lease. A claimed job is invisible to other drainers until this
    -- passes; a drainer that crashes mid-job simply lets the lease lapse
    -- and the job becomes claimable again. Combined with FOR UPDATE SKIP
    -- LOCKED at claim time, this is what prevents two drainers processing
    -- one job concurrently — while still guaranteeing at-least-once.
    locked_until    TIMESTAMPTZ,
    -- A fixed classification only ("send_failed", "store_failed"...).
    -- Never a provider response body, never an error string that could
    -- quote the request: the request would contain the recipient.
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,
    CONSTRAINT email_outbox_kind_valid
        CHECK (kind IN ('password_reset', 'verification_resend')),
    CONSTRAINT email_outbox_status_valid
        CHECK (status IN ('pending', 'dead'))
);

-- The drainer's working set: due, unclaimed, pending rows.
CREATE INDEX IF NOT EXISTS idx_email_outbox_claimable
    ON email_outbox (next_attempt_at)
    WHERE status = 'pending';
