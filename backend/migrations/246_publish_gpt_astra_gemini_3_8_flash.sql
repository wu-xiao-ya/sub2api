-- Publish GPT Astra and Gemini 3.8 Flash without overwriting administrator
-- pricing. Astra inherits GPT-5.6 Sol pricing; Gemini 3.8 Flash inherits the
-- existing Gemini 3.7 Flash channel price and falls back to the current
-- production defaults when a channel has no 3.7 row.

UPDATE groups
SET models_list_config = jsonb_set(
        COALESCE(models_list_config, '{}'::jsonb),
        '{models}',
        jsonb_build_array('gpt-6-astra') ||
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
  AND COALESCE(models_list_config->'models', '[]'::jsonb) @> '["gpt-5.6-sol"]'::jsonb
  AND NOT COALESCE(models_list_config->'models', '[]'::jsonb) @> '["gpt-6-astra"]'::jsonb;

UPDATE groups
SET models_list_config = jsonb_set(
        COALESCE(models_list_config, '{}'::jsonb),
        '{models}',
        jsonb_build_array('gemini-3.8-flash') ||
            CASE
                WHEN jsonb_typeof(models_list_config->'models') = 'array'
                    THEN models_list_config->'models'
                ELSE '[]'::jsonb
            END,
        true
    ),
    updated_at = NOW()
WHERE platform = 'gemini'
  AND deleted_at IS NULL
  AND NOT COALESCE(models_list_config->'models', '[]'::jsonb) @> '["gemini-3.8-flash"]'::jsonb;

WITH target_channels AS (
    SELECT DISTINCT cg.channel_id
    FROM channel_groups cg
    JOIN groups g ON g.id = cg.group_id
    WHERE g.platform = 'openai'
      AND g.deleted_at IS NULL
      AND COALESCE(g.models_list_config->'models', '[]'::jsonb) @> '["gpt-6-astra"]'::jsonb
),
source_prices AS (
    SELECT DISTINCT ON (cmp.channel_id)
        cmp.channel_id,
        cmp.billing_mode,
        cmp.input_price,
        cmp.output_price,
        cmp.cache_write_price,
        cmp.cache_read_price,
        cmp.image_input_price,
        cmp.image_output_price,
        cmp.per_request_price
    FROM channel_model_pricing cmp
    WHERE cmp.platform = 'openai'
      AND cmp.models @> '["gpt-5.6-sol"]'::jsonb
    ORDER BY cmp.channel_id, cmp.id
)
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
SELECT
    target.channel_id,
    'openai',
    '["gpt-6-astra"]'::jsonb,
    COALESCE(source.billing_mode, 'token'),
    COALESCE(source.input_price, 0.000005000000),
    COALESCE(source.output_price, 0.000030000000),
    COALESCE(source.cache_write_price, 0.000006250000),
    COALESCE(source.cache_read_price, 0.000000500000),
    COALESCE(source.image_input_price, 0),
    COALESCE(source.image_output_price, 0),
    source.per_request_price,
    NOW(),
    NOW()
FROM target_channels target
LEFT JOIN source_prices source ON source.channel_id = target.channel_id
WHERE NOT EXISTS (
    SELECT 1
    FROM channel_model_pricing existing
    WHERE existing.channel_id = target.channel_id
      AND existing.platform = 'openai'
      AND existing.models @> '["gpt-6-astra"]'::jsonb
);

WITH target_channels AS (
    SELECT DISTINCT cg.channel_id
    FROM channel_groups cg
    JOIN groups g ON g.id = cg.group_id
    WHERE g.platform = 'gemini'
      AND g.deleted_at IS NULL
),
source_prices AS (
    SELECT DISTINCT ON (cmp.channel_id)
        cmp.channel_id,
        cmp.billing_mode,
        cmp.input_price,
        cmp.output_price,
        cmp.cache_write_price,
        cmp.cache_read_price,
        cmp.image_input_price,
        cmp.image_output_price,
        cmp.per_request_price
    FROM channel_model_pricing cmp
    WHERE cmp.platform = 'gemini'
      AND cmp.models @> '["gemini-3.7-flash"]'::jsonb
    ORDER BY cmp.channel_id, cmp.id
)
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
SELECT
    target.channel_id,
    'gemini',
    '["gemini-3.8-flash"]'::jsonb,
    COALESCE(source.billing_mode, 'per_request'),
    COALESCE(source.input_price, 0.000000750000),
    COALESCE(source.output_price, 0.000003750000),
    COALESCE(source.cache_write_price, 0),
    COALESCE(source.cache_read_price, 0.000000075000),
    COALESCE(source.image_input_price, 0),
    COALESCE(source.image_output_price, 0),
    COALESCE(source.per_request_price, 0.0400000000),
    NOW(),
    NOW()
FROM target_channels target
LEFT JOIN source_prices source ON source.channel_id = target.channel_id
WHERE NOT EXISTS (
    SELECT 1
    FROM channel_model_pricing existing
    WHERE existing.channel_id = target.channel_id
      AND existing.platform = 'gemini'
      AND existing.models @> '["gemini-3.8-flash"]'::jsonb
);

-- Preserve explicit account mappings while making the two new model IDs
-- selectable on accounts that already advertise their predecessor.
UPDATE accounts
SET credentials = jsonb_set(
        credentials,
        '{model_mapping,gpt-6-astra}',
        '"gpt-6-astra"'::jsonb,
        true
    ),
    updated_at = NOW()
WHERE platform = 'openai'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND credentials->'model_mapping' ? 'gpt-5.6-sol'
  AND NOT (credentials->'model_mapping' ? 'gpt-6-astra');

UPDATE accounts
SET credentials = jsonb_set(
        credentials,
        '{model_mapping,gemini-3.8-flash}',
        '"gemini-3.8-flash"'::jsonb,
        true
    ),
    updated_at = NOW()
WHERE platform = 'gemini'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND credentials->'model_mapping' ? 'gemini-3.7-flash'
  AND NOT (credentials->'model_mapping' ? 'gemini-3.8-flash');

-- Add both models to the Channel Monitor v2 presentation allow-list without
-- replacing any administrator custom platform or enabled/disabled settings.
UPDATE channel_monitor_v2_config config
SET platforms = (
        SELECT jsonb_agg(
            CASE
                WHEN entry->>'platform' = 'openai'
                     AND NOT COALESCE(entry->'models', '[]'::jsonb) @> '["gpt-6-astra"]'::jsonb
                    THEN jsonb_set(
                        entry,
                        '{models}',
                        jsonb_build_array('gpt-6-astra') ||
                            CASE
                                WHEN jsonb_typeof(entry->'models') = 'array'
                                    THEN entry->'models'
                                ELSE '[]'::jsonb
                            END,
                        true
                    )
                WHEN entry->>'platform' = 'gemini'
                     AND NOT COALESCE(entry->'models', '[]'::jsonb) @> '["gemini-3.8-flash"]'::jsonb
                    THEN jsonb_set(
                        entry,
                        '{models}',
                        jsonb_build_array('gemini-3.8-flash') ||
                            CASE
                                WHEN jsonb_typeof(entry->'models') = 'array'
                                    THEN entry->'models'
                                ELSE '[]'::jsonb
                            END,
                        true
                    )
                ELSE entry
            END
            ORDER BY position
        )
        FROM jsonb_array_elements(COALESCE(config.platforms, '[]'::jsonb))
             WITH ORDINALITY AS platform(entry, position)
    ),
    version = version + 1,
    updated_at = NOW()
WHERE id = 1
  AND (
      EXISTS (
          SELECT 1
          FROM jsonb_array_elements(COALESCE(config.platforms, '[]'::jsonb)) entry
          WHERE entry->>'platform' = 'openai'
            AND NOT COALESCE(entry->'models', '[]'::jsonb) @> '["gpt-6-astra"]'::jsonb
      )
      OR EXISTS (
          SELECT 1
          FROM jsonb_array_elements(COALESCE(config.platforms, '[]'::jsonb)) entry
          WHERE entry->>'platform' = 'gemini'
            AND NOT COALESCE(entry->'models', '[]'::jsonb) @> '["gemini-3.8-flash"]'::jsonb
      )
  );
