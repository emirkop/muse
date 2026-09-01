-- — Free/Paid Capacity: persisted App Store entitlement state,
-- bound to a Muse account ( CLOSED:
-- account-wide aggregate, App Store IAP, ONE
-- non-consumable, Collection Item capacity ONLY, one free +
-- one paid tier, bound to the Muse account via appAccountToken).
-- Two tables, and note what is NOT here: no capacity number. `04` Part J
-- requires thresholds to be backend configuration read by the entitlement
-- provider, never compiled in and never a row — so the free and paid item
-- capacities live in ENTITLEMENT_FREE_ITEM_CAPACITY /
-- ENTITLEMENT_PAID_ITEM_CAPACITY (development uses a labelled placeholder
-- when unset; production refuses to start without them).

-- ---------------------------------------------------------------------
-- The account's app account token.
-- ---------------------------------------------------------------------
-- Apple's mechanism for associating a purchase with a customer on the
-- developer's own service: "The UUID that you generate to associate a
-- customer's In-App Purchase with its resulting App Store transaction."
-- Muse mints one UUID per account, server-side, hands it to StoreKit at
-- purchase time (`Product.PurchaseOption.appAccountToken`), and reads it
-- back from the signed transaction. A transaction whose token is not the
-- redeeming account's token is refused — that is how "the purchase is
-- associated with the Muse account used at purchase time" is enforced
-- rather than assumed, and how the same Apple ID cannot unlock a second
-- Muse account merely by being signed in on the device.
-- One token per account (PRIMARY KEY), globally unique (UNIQUE), so a token
-- resolves to exactly one account and an account has exactly one token.
CREATE TABLE IF NOT EXISTS account_app_account_tokens (
    account_id UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    token      UUID NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE account_app_account_tokens IS
 ': the server-minted appAccountToken that binds an App Store purchase to exactly one Muse account. '
    'Minted before purchase, passed to StoreKit, read back from the signed transaction.';

-- ---------------------------------------------------------------------
-- Verified App Store transactions (: one non-consumable).
-- ---------------------------------------------------------------------
-- One row per App Store transaction Muse has verified and bound. The
-- PRIMARY KEY on original_transaction_id is the load-bearing fact of this
-- table: **one verified App Store transaction unlocks exactly one Muse
-- account.** A second account presenting the same signed transaction
-- conflicts on this key, and the repository refuses to re-bind it.
-- Nothing here is trusted from the client. Every column is copied from an
-- Apple-signed JWS whose signature and certificate chain the server
-- verified against Apple's root, after the payload passed the bundle,
-- product, type, ownership and environment checks.
-- Revocation is App Store state, not Muse's: `revoked_at` is set from the
-- transaction's `revocationDate` or from an App Store Server Notification
-- (REFUND / REVOKE), and cleared by REFUND_REVERSED. The row is KEPT — a
-- revoked transaction is still bound to its account, so it can never be
-- redeemed by another one later.
CREATE TABLE IF NOT EXISTS app_store_transactions (
    original_transaction_id TEXT PRIMARY KEY,
    transaction_id          TEXT NOT NULL,
    account_id              UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    product_id              TEXT NOT NULL,
    bundle_id               TEXT NOT NULL,
    environment             TEXT NOT NULL,
    app_account_token       UUID NOT NULL,
    purchased_at            TIMESTAMPTZ NOT NULL,
    revoked_at              TIMESTAMPTZ,
    revocation_reason       TEXT NOT NULL DEFAULT '',
    first_verified_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_verified_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_app_store_transactions_account ON app_store_transactions (account_id);

COMMENT ON TABLE app_store_transactions IS
    ' (/): Apple-signed, server-verified App Store transactions, each bound to exactly one Muse account '
    '(PRIMARY KEY on original_transaction_id). revoked_at mirrors App Store refund/revocation state; rows are never deleted.';
