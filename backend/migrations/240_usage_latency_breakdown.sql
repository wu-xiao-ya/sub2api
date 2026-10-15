ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS latency_breakdown JSONB;

ALTER TABLE channel_monitor_histories
    ADD COLUMN IF NOT EXISTS latency_breakdown JSONB;
