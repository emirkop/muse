-- close-out — CLOSED: Muse stops persisting the email
-- address its identity providers supply.
-- WHAT THIS AFFECTS, PROVEN BEFORE IT WAS WRITTEN
-- `external_identities.email` and `external_identities.email_verified`
-- (migration 0001) were written at exactly one site — the INSERT in
-- PostgresAccountRepository.CreateWithLinkedIdentity — and read NOWHERE.
-- The only SELECT from this table is FindByLinkedIdentity, which joins on
-- `provider` and `subject` alone. Identity resolution therefore continues
-- to be `(provider, stable subject)` and is untouched by this migration.
-- WHAT THIS DOES NOT AFFECT
-- Email/password authentication uses DIFFERENT TABLES and
-- different columns, every one of which is genuinely read:
-- password_credentials.email -- the login identifier; SELECTed by
-- FindByEmail on every email log-in
-- pending_signups.email -- verify-first sign-up
-- email_outbox.email -- the delivery address, transient
-- None of them is touched here. This migration is the reason
-- (never link by email) becomes structural rather than merely enforced:
-- after it, a provider account has no email column for anything to link
-- on, and migration 0008's comment that it "deliberately says nothing
-- about external_identities.email" is true of the schema itself.
-- ONE HONEST LIMITATION
-- The UPDATE overwrites the values in new row versions and the DROP
-- removes the columns logically; neither reclaims the bytes in existing
-- dead tuples. Physical erasure needs a VACUUM FULL (or a table rewrite),
-- which is an operational step on a real database and belongs to phase
-- 92 — rather than attempted
-- from a migration that must be safe to run on any size of table.
-- Applying this to a database with rows loses those addresses
-- irrecoverably: Apple sends the email claim only on a user's FIRST
-- authorization. That irreversibility is exactly why was raised
-- as a decision at instead of being fixed there, and it is now
-- the product owner's recorded answer.

UPDATE external_identities SET email = '', email_verified = false;

ALTER TABLE external_identities
    DROP COLUMN IF EXISTS email,
    DROP COLUMN IF EXISTS email_verified;
