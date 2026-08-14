-- Migration: 194_channel_monitor_request_timeout
-- Persist an upstream wait limit on every channel monitor. Existing image
-- monitors receive a five-minute default while text monitors retain 45s.

ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS request_timeout_seconds INTEGER NOT NULL DEFAULT 45;

UPDATE channel_monitors
SET request_timeout_seconds = 300
WHERE api_mode = 'images'
  AND request_timeout_seconds = 45;

-- Existing image monitors that still use the standard text cadence should
-- switch to the lower-cost image default. Customized intervals are preserved.
UPDATE channel_monitors
SET interval_seconds = 1800
WHERE api_mode = 'images'
  AND interval_seconds = 300;

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_request_timeout_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_request_timeout_check
    CHECK (request_timeout_seconds BETWEEN 15 AND 900);
