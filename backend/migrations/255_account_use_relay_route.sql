-- Preserve the customized account-level opt-in for the system traffic relay.
-- The relay is resolved at request time and falls back to the account proxy.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS use_relay_route BOOLEAN NOT NULL DEFAULT FALSE;
