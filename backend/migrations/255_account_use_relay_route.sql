-- Account-level opt-in for the system traffic relay outbound.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS use_relay_route BOOLEAN NOT NULL DEFAULT FALSE;

-- Routing reads accounts by ID and carries this flag in the scheduler cache.
-- No boolean index is needed; avoid an unnecessary production index build.
