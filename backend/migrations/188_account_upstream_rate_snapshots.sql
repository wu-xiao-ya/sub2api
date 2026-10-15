-- Keep historical upstream billing probe rates for time-aware cost reporting.
--
-- One probe result is copied for every group currently bound to the account.
-- group_id = 0 is reserved for an account without a group binding.
CREATE TABLE IF NOT EXISTS account_upstream_rate_snapshots (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    effective_rate_multiplier NUMERIC(10, 4) NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source VARCHAR(32) NOT NULL DEFAULT 'probe'
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_upstream_rate_snapshots_identity
    ON account_upstream_rate_snapshots (account_id, group_id, observed_at);

CREATE INDEX IF NOT EXISTS idx_account_upstream_rate_snapshots_lookup
    ON account_upstream_rate_snapshots (account_id, group_id, observed_at DESC);
