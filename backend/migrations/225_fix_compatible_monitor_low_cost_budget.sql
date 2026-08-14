-- Compatible reasoning providers can return HTTP 2xx with no visible output
-- when the monitor allows only one output token. Keep the probe low-cost while
-- allowing enough room for a stable response.

UPDATE channel_monitor_request_templates
SET body_override = '{"max_tokens": 16}'::jsonb
WHERE provider IN ('deepseek', 'kimi', 'glm')
  AND api_mode = 'chat_completions'
  AND body_override_mode = 'merge'
  AND body_override = '{"max_tokens": 1}'::jsonb;

UPDATE channel_monitors
SET body_override_mode = 'merge',
    body_override = '{"max_tokens": 16}'::jsonb
WHERE provider IN ('deepseek', 'kimi', 'glm')
  AND api_mode = 'chat_completions'
  AND body_override_mode = 'merge'
  AND body_override = '{"max_tokens": 1}'::jsonb;
