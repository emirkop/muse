-- 0026_create_analytics_events.sql
-- (Analytics Requirements Definition & Implementation), after
--, and closed.
-- Two tables, and the split between them IS the retention decision:
-- analytics_events raw, account-linked, pruned after 7 days
-- analytics_daily_counts aggregate, NO account identifier, kept
-- closed as: Muse-owned analytics, no third-party SDK, raw
-- retention 7 days, and longer-lived data may be aggregate counts only
-- with no account identifier. Pruning rolls raw rows up into the counts
-- table and deletes them in one transaction, so the only thing that
-- survives the window is a number.
-- **Every property is a typed column.** There is deliberately no JSONB and
-- no key/value table: the schema itself enumerates what may be stored, so
-- "no arbitrary/free-text property bags" is a fact about the database
-- rather than a rule the application has to keep. Adding a property means
-- a migration and a contract change, which is the point.
-- What is absent, by decision and not by omission: no client timestamp (the
-- server stamps received_at, so no device clock is recorded), no IP
-- address, no user agent, no device identifier of any kind, no
-- session id, no message or error text, no free text at all.

CREATE TABLE IF NOT EXISTS analytics_events (
 -- Client- or server-generated, random, per event. A **deduplication
 -- key only**: never persisted on the device, never reused, never
 -- derived from the device. Two events from one device share nothing.
 -- PRIMARY KEY is what makes a duplicate delivery a no-op.
    event_uuid          UUID PRIMARY KEY,

    name                TEXT NOT NULL,

 -- Every event is authenticated (: no pre-sign-in analytics),
 -- so this is NOT NULL. ON DELETE CASCADE so that if an account
 -- deletion flow is ever built, its analytics go with it
 -- without that flow having to know this table exists.
    account_id          UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,

 -- The server's clock. No client-supplied time is accepted.
    received_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

 -- The complete typed property set. All nullable: each event carries
 -- only the properties its contract permits.
    step                TEXT,
    category_id         TEXT,
    result_bucket       TEXT,
    outcome             TEXT,
    reason              TEXT,
    surface             TEXT,
    classification      TEXT,
    retried             BOOLEAN,
    retry_succeeded     BOOLEAN,

 -- Defence in depth behind domain.Registry. The application refuses an
 -- unknown name first; this makes an unknown name unstorable even if a
 -- future caller bypassed it.
    CONSTRAINT analytics_events_known_name CHECK (name IN (
        'museum_creation_step',
        'room_creation_step',
        'collection_room_creation_step',
        'catalog_search_performed',
        'catalog_search_outcome',
        'item_add_refused',
        'capacity_upgrade_step',
        'failure_shown'
    ))
);

-- The pruning scan and every aggregate query are time-ordered.
CREATE INDEX IF NOT EXISTS idx_analytics_events_received_at
    ON analytics_events (received_at);

COMMENT ON TABLE analytics_events IS
 ' raw analytics. Account-linked and pruned after 7 days. '
    'Every property is a typed column: no JSONB, no property bag, no free text. '
 'No client timestamp, no IP, no user agent, no device identifier.';
COMMENT ON COLUMN analytics_events.event_uuid IS
    'Random per-event deduplication key. NOT a device identifier: never stored '
 'on the device, never reused, never derived from it.';

-- What may outlive the 7-day window: counts, with no account identifier.
CREATE TABLE IF NOT EXISTS analytics_daily_counts (
    day                 DATE NOT NULL,
    name                TEXT NOT NULL,
 -- The dimensions, empty-string rather than NULL so the primary key is
 -- usable (NULLs would not deduplicate in a unique index).
    step                TEXT NOT NULL DEFAULT '',
    category_id         TEXT NOT NULL DEFAULT '',
    result_bucket       TEXT NOT NULL DEFAULT '',
    outcome             TEXT NOT NULL DEFAULT '',
    reason              TEXT NOT NULL DEFAULT '',
    surface             TEXT NOT NULL DEFAULT '',
    classification      TEXT NOT NULL DEFAULT '',
    retried             TEXT NOT NULL DEFAULT '',
    retry_succeeded     TEXT NOT NULL DEFAULT '',
    event_count         BIGINT NOT NULL DEFAULT 0,

    PRIMARY KEY (day, name, step, category_id, result_bucket, outcome,
                 reason, surface, classification, retried, retry_succeeded)
);

COMMENT ON TABLE analytics_daily_counts IS
 ' aggregates. Deliberately carries NO account identifier and no '
 'event id, so nothing here can be attributed to a person. This is '
    'the only analytics data permitted to outlive the 7-day raw window.';
