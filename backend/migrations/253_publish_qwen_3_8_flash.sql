-- Publish Qwen 3.8 Flash to existing Qwen groups and channels.
--
-- Official Beijing CNY card is carried over 1:1 as site USD:
-- input 0.8 / output 2.7 / cache hit 0.1 / explicit cache create 1.25 per 1M tokens.
-- Existing administrator pricing is preserved.

UPDATE groups
SET models_list_config = jsonb_set(
        COALESCE(models_list_config, '{}'::jsonb),
        '{models}',
        jsonb_build_array('qwen3.8-flash') ||
            CASE
                WHEN jsonb_typeof(models_list_config->'models') = 'array'
                    THEN models_list_config->'models'
                ELSE '[]'::jsonb
            END,
        true
    ),
    updated_at = NOW()
WHERE platform = 'qwen'
  AND deleted_at IS NULL
  AND NOT COALESCE(models_list_config->'models', '[]'::jsonb) @> '["qwen3.8-flash"]'::jsonb;

INSERT INTO channel_model_pricing (
    channel_id,
    platform,
    models,
    billing_mode,
    input_price,
    output_price,
    cache_write_price,
    cache_read_price,
    image_input_price,
    image_output_price,
    per_request_price,
    created_at,
    updated_at
)
SELECT DISTINCT
    cg.channel_id,
    'qwen',
    '["qwen3.8-flash"]'::jsonb,
    'token',
    0.000000800000,
    0.000002700000,
    0.000001250000,
    0.000000100000,
    0.000000000000,
    0.000000000000,
    NULL::numeric,
    NOW(),
    NOW()
FROM channel_groups cg
JOIN groups g ON g.id = cg.group_id
WHERE g.platform = 'qwen'
  AND g.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM channel_model_pricing cmp
      WHERE cmp.channel_id = cg.channel_id
        AND cmp.platform = 'qwen'
        AND cmp.models @> '["qwen3.8-flash"]'::jsonb
  );

UPDATE channel_monitors
SET extra_models = CASE
        WHEN jsonb_typeof(extra_models) = 'array'
            THEN extra_models || '["qwen3.8-flash"]'::jsonb
        ELSE '["qwen3.8-flash"]'::jsonb
    END,
    updated_at = NOW()
WHERE provider = 'qwen'
  AND NOT COALESCE(extra_models, '[]'::jsonb) @> '["qwen3.8-flash"]'::jsonb;
