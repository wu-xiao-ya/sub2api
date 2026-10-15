-- Kimi reasoning models can consume a tiny monitor output budget internally
-- and return HTTP 2xx with no visible content. Keep the health probe cheap by
-- explicitly disabling thinking for the standard Kimi low-cost template.

UPDATE channel_monitor_request_templates
SET body_override = '{"max_tokens": 16, "thinking": {"type": "disabled"}}'::jsonb
WHERE provider = 'kimi'
  AND api_mode = 'chat_completions'
  AND body_override_mode = 'merge'
  AND body_override = '{"max_tokens": 16}'::jsonb;

UPDATE channel_monitors
SET body_override_mode = 'merge',
    body_override = '{"max_tokens": 16, "thinking": {"type": "disabled"}}'::jsonb
WHERE provider = 'kimi'
  AND api_mode = 'chat_completions'
  AND body_override_mode = 'merge'
  AND body_override = '{"max_tokens": 16}'::jsonb;
