-- Migration: 197_add_usage_log_usage_source
-- Mark internal usage rows such as channel-monitor probes so admin views can
-- hide them by default while still keeping exact billing records.

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS usage_source VARCHAR(32);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.table_constraints
        WHERE constraint_name = 'usage_logs_usage_source_check'
          AND table_name = 'usage_logs'
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_usage_source_check
            CHECK (
                usage_source IS NULL
                OR usage_source IN ('channel_monitor')
            );
    END IF;
END $$;
