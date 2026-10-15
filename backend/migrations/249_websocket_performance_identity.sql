-- Logical WebSocket turns need identities even when no response ID was issued.
-- The optional native response ID joins existing usage, never changes billing.
ALTER TABLE channel_performance_facts
    ADD COLUMN IF NOT EXISTS usage_request_id TEXT;
CREATE INDEX IF NOT EXISTS channel_performance_facts_usage_idx
    ON channel_performance_facts (api_key_id, (COALESCE(NULLIF(usage_request_id,''),request_id)));
