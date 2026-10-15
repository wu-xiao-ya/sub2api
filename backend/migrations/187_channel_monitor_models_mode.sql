-- Migration: 187_channel_monitor_models_mode
-- Add an OpenAI /v1/models monitor mode. It validates upstream authentication
-- and confirms the configured model is still advertised without generating content.

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_api_mode_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_api_mode_check
    CHECK (api_mode IN ('chat_completions', 'responses', 'models'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_api_mode_check;

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_api_mode_check
    CHECK (api_mode IN ('chat_completions', 'responses', 'models'));
