-- User-contributed OpenAI OAuth shared account pool.
--
-- Contributed accounts are never schedulable before approval. Rewards are
-- recorded idempotently in contributor_reward_logs by the usage billing
-- transaction, so request retries cannot create duplicate balance credits.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS owner_user_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS contribution_status VARCHAR(20) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS contribution_submitted_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS contribution_approved_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS contribution_revoked_at TIMESTAMPTZ NULL;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS contributor_reward_multiplier DECIMAL(10,4) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS contributor_reward_logs (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(128) NOT NULL,
    api_key_id BIGINT NOT NULL,
    owner_user_id BIGINT NOT NULL,
    consumer_user_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    group_id BIGINT NULL,
    total_cost DECIMAL(20,10) NOT NULL DEFAULT 0,
    actual_cost DECIMAL(20,10) NOT NULL DEFAULT 0,
    reward_multiplier DECIMAL(10,4) NOT NULL DEFAULT 0,
    reward_amount DECIMAL(20,10) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT contributor_reward_logs_request_api_key_unique UNIQUE (request_id, api_key_id)
);

CREATE INDEX IF NOT EXISTS idx_accounts_owner_user_id
    ON accounts(owner_user_id)
    WHERE deleted_at IS NULL AND owner_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_accounts_contribution_status
    ON accounts(contribution_status)
    WHERE deleted_at IS NULL AND contribution_status <> '';

CREATE INDEX IF NOT EXISTS idx_contributor_reward_logs_owner_created
    ON contributor_reward_logs(owner_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_contributor_reward_logs_account_created
    ON contributor_reward_logs(account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_contributor_reward_logs_group_created
    ON contributor_reward_logs(group_id, created_at DESC)
    WHERE group_id IS NOT NULL;

-- The identity key is written to accounts.extra.contribution_identity_key.
-- Rejected and revoked contributions intentionally retain the reservation to
-- prevent the same upstream identity from being resubmitted under another user.
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_contribution_identity_unique
    ON accounts ((extra->>'contribution_identity_key'))
    WHERE deleted_at IS NULL
      AND owner_user_id IS NOT NULL
      AND contribution_status <> ''
      AND COALESCE(extra->>'contribution_identity_key', '') <> '';
