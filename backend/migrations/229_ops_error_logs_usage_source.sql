ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS usage_source VARCHAR(32);
