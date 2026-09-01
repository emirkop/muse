-- 0002_add_avatar_id_to_accounts.sql
-- (Profile System, 's Profiles
-- domain): adds the account's Avatar reference. Avatar selection itself
-- (the five predefined options, the picker UI) is work — this
-- column always holds '' until then; it exists now only so the Profile
-- read/edit surface has a real, persisted field to serve.

ALTER TABLE accounts ADD COLUMN IF NOT EXISTS avatar_id TEXT NOT NULL DEFAULT '';
