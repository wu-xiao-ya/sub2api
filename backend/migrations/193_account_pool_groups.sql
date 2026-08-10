-- Account pool groups are an admin-side grouping layer for accounts that share
-- the same upstream source. They are intentionally separate from routing/sales
-- groups (`groups` + `account_groups`) and do not affect scheduler behavior.

CREATE TABLE IF NOT EXISTS account_pool_groups (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    upstream_key VARCHAR(100) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

ALTER TABLE accounts ADD COLUMN IF NOT EXISTS pool_group_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'accounts_pool_group_id_fkey'
    ) THEN
        ALTER TABLE accounts
            ADD CONSTRAINT accounts_pool_group_id_fkey
            FOREIGN KEY (pool_group_id) REFERENCES account_pool_groups(id)
            ON DELETE SET NULL;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_account_pool_groups_upstream_name_active
    ON account_pool_groups (LOWER(upstream_key), LOWER(name))
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_account_pool_groups_status_sort
    ON account_pool_groups (status, sort_order, id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_accounts_pool_group_id_active
    ON accounts (pool_group_id)
    WHERE deleted_at IS NULL;
