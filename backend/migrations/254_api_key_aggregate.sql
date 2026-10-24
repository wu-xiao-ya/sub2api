-- Aggregate API keys: one external key that routes to member group keys.

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS key_kind VARCHAR(20) NOT NULL DEFAULT 'group';

UPDATE api_keys
SET key_kind = 'group'
WHERE key_kind IS NULL OR key_kind = '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'api_keys_key_kind_check'
    ) THEN
        ALTER TABLE api_keys
            ADD CONSTRAINT api_keys_key_kind_check
            CHECK (key_kind IN ('group', 'aggregate'));
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS api_keys_key_kind_idx ON api_keys (key_kind);

CREATE TABLE IF NOT EXISTS api_key_aggregate_members (
    aggregate_key_id BIGINT NOT NULL REFERENCES api_keys (id) ON DELETE CASCADE,
    member_key_id BIGINT NOT NULL REFERENCES api_keys (id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (aggregate_key_id, member_key_id)
);

CREATE INDEX IF NOT EXISTS api_key_aggregate_members_member_idx
    ON api_key_aggregate_members (member_key_id);

CREATE INDEX IF NOT EXISTS api_key_aggregate_members_order_idx
    ON api_key_aggregate_members (aggregate_key_id, sort_order, member_key_id);
