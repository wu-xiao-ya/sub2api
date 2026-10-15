-- Publish GPT-6 Sol, GPT-6 Luna, and Claude Opus 5.5 on the channels that
-- already sell their predecessor. Existing Grok 4.7 channel prices stay as
-- configured. Rows are inserted only when the model is not already priced.

UPDATE groups
SET models_list_config = jsonb_set(
        COALESCE(models_list_config, '{}'::jsonb),
        '{models}',
        jsonb_build_array('gpt-6-sol', 'gpt-6-luna') ||
            CASE
                WHEN jsonb_typeof(models_list_config->'models') = 'array'
                    THEN models_list_config->'models'
                ELSE '[]'::jsonb
            END,
        true
    ),
    updated_at = NOW()
WHERE platform = 'openai'
  AND deleted_at IS NULL
  AND COALESCE(models_list_config->'models', '[]'::jsonb) @> '["gpt-6-astra"]'::jsonb
  AND NOT COALESCE(models_list_config->'models', '[]'::jsonb) @> '["gpt-6-sol"]'::jsonb;

UPDATE groups
SET models_list_config = jsonb_set(
        COALESCE(models_list_config, '{}'::jsonb),
        '{models}',
        jsonb_build_array('claude-opus-5-5') ||
            CASE
                WHEN jsonb_typeof(models_list_config->'models') = 'array'
                    THEN models_list_config->'models'
                ELSE '[]'::jsonb
            END,
        true
    ),
    updated_at = NOW()
WHERE platform = 'anthropic'
  AND deleted_at IS NULL
  AND (
      COALESCE(models_list_config->'models', '[]'::jsonb) @> '["claude-opus-5"]'::jsonb
      OR COALESCE(models_list_config->'models', '[]'::jsonb) @> '["claude-opus-4-8"]'::jsonb
  )
  AND NOT COALESCE(models_list_config->'models', '[]'::jsonb) @> '["claude-opus-5-5"]'::jsonb;

UPDATE groups
SET models_list_config = jsonb_set(
        COALESCE(models_list_config, '{}'::jsonb),
        '{models}',
        jsonb_build_array('grok-4.7', 'grok-4.7-build-fast') ||
            CASE
                WHEN jsonb_typeof(models_list_config->'models') = 'array'
                    THEN models_list_config->'models'
                ELSE '[]'::jsonb
            END,
        true
    ),
    updated_at = NOW()
WHERE platform = 'grok'
  AND deleted_at IS NULL
  AND COALESCE(models_list_config->'models', '[]'::jsonb) @> '["grok-4.6"]'::jsonb
  AND NOT COALESCE(models_list_config->'models', '[]'::jsonb) @> '["grok-4.7"]'::jsonb;

WITH target_channels AS (
    SELECT DISTINCT cg.channel_id
    FROM channel_groups cg
    JOIN groups g ON g.id = cg.group_id
    WHERE g.platform = 'openai'
      AND g.deleted_at IS NULL
      AND COALESCE(g.models_list_config->'models', '[]'::jsonb) @> '["gpt-6-sol"]'::jsonb
)
INSERT INTO channel_model_pricing (
    channel_id, platform, models, billing_mode,
    input_price, output_price, cache_write_price, cache_read_price,
    image_input_price, image_output_price, per_request_price,
    created_at, updated_at
)
SELECT
    target.channel_id, 'openai', model.models::jsonb, 'token',
    model.input_price, model.output_price, model.cache_write_price, model.cache_read_price,
    0, 0, NULL, NOW(), NOW()
FROM target_channels target
CROSS JOIN (
    VALUES
        ('["gpt-6-sol"]', 0.000002000000, 0.000010000000, 0.000002500000, 0.000000200000),
        ('["gpt-6-luna"]', 0.000000100000, 0.000000500000, 0.000000125000, 0.000000010000)
) AS model(models, input_price, output_price, cache_write_price, cache_read_price)
WHERE NOT EXISTS (
    SELECT 1
    FROM channel_model_pricing existing
    WHERE existing.channel_id = target.channel_id
      AND existing.platform = 'openai'
      AND existing.models @> model.models::jsonb
);

WITH target_channels AS (
    SELECT DISTINCT cg.channel_id
    FROM channel_groups cg
    JOIN groups g ON g.id = cg.group_id
    WHERE g.platform = 'anthropic'
      AND g.deleted_at IS NULL
      AND COALESCE(g.models_list_config->'models', '[]'::jsonb) @> '["claude-opus-5-5"]'::jsonb
)
INSERT INTO channel_model_pricing (
    channel_id, platform, models, billing_mode,
    input_price, output_price, cache_write_price, cache_read_price,
    image_input_price, image_output_price, per_request_price,
    created_at, updated_at
)
SELECT
    target.channel_id, 'anthropic', '["claude-opus-5-5"]'::jsonb, 'token',
    0.000004000000, 0.000020000000, 0.000005000000, 0.000000200000,
    0, 0, NULL, NOW(), NOW()
FROM target_channels target
WHERE NOT EXISTS (
    SELECT 1
    FROM channel_model_pricing existing
    WHERE existing.channel_id = target.channel_id
      AND existing.platform = 'anthropic'
      AND existing.models @> '["claude-opus-5-5"]'::jsonb
);

UPDATE accounts
SET credentials = jsonb_set(
        jsonb_set(credentials, '{model_mapping,gpt-6-sol}', '"gpt-6-sol"'::jsonb, true),
        '{model_mapping,gpt-6-luna}', '"gpt-6-luna"'::jsonb, true
    ),
    updated_at = NOW()
WHERE platform = 'openai'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND credentials->'model_mapping' ? 'gpt-6-astra'
  AND NOT (credentials->'model_mapping' ? 'gpt-6-sol');

UPDATE accounts
SET credentials = jsonb_set(
        credentials,
        '{model_mapping,claude-opus-5-5}',
        '"claude-opus-5-5"'::jsonb,
        true
    ),
    updated_at = NOW()
WHERE platform = 'anthropic'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND (
      credentials->'model_mapping' ? 'claude-opus-5'
      OR credentials->'model_mapping' ? 'claude-opus-4-8'
  )
  AND NOT (credentials->'model_mapping' ? 'claude-opus-5-5');

UPDATE accounts
SET credentials = jsonb_set(
        jsonb_set(credentials, '{model_mapping,grok-4.7}', '"grok-4.7"'::jsonb, true),
        '{model_mapping,grok-4.7-build-fast}', '"grok-4.7-build-fast"'::jsonb, true
    ),
    updated_at = NOW()
WHERE platform = 'grok'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND credentials->'model_mapping' ? 'grok-4.6'
  AND NOT (credentials->'model_mapping' ? 'grok-4.7');
