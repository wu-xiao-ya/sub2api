-- DeepSeek standard-card billing and V4.1 Flash aliases.
--
-- Official CNY off-peak prices are stored as the same USD numbers. Peak windows
-- (Beijing 09:00-12:00 and 14:00-18:00) apply a 2x multiplier in code, so this
-- migration must not write peak prices into channel_model_pricing.

WITH policy(model, input_price, output_price, cache_read_price) AS (
    VALUES
        ('deepseek-flash',                0.000001000000::numeric, 0.000004000000::numeric, 0.000000020000::numeric),
        ('deepseek-v4.1-flash',           0.000001000000::numeric, 0.000004000000::numeric, 0.000000020000::numeric),
        ('deepseek-v4-flash',             0.000001000000::numeric, 0.000004000000::numeric, 0.000000020000::numeric),
        ('deepseek-v4-flash-0731',        0.000001000000::numeric, 0.000004000000::numeric, 0.000000020000::numeric),
        ('deepseek-v4-flash-vision-exp',  0.000001000000::numeric, 0.000004000000::numeric, 0.000000020000::numeric),
        ('deepseek-chat',                 0.000001000000::numeric, 0.000004000000::numeric, 0.000000020000::numeric),
        ('deepseek-reasoner',             0.000001000000::numeric, 0.000004000000::numeric, 0.000000020000::numeric),
        ('deepseek-v4-pro',               0.000004500000::numeric, 0.000013500000::numeric, 0.000000150000::numeric),
        ('deepseek-v4-pro-0813',          0.000004500000::numeric, 0.000013500000::numeric, 0.000000150000::numeric)
)
UPDATE channel_model_pricing AS pricing
SET input_price = policy.input_price,
    output_price = policy.output_price,
    cache_read_price = policy.cache_read_price,
    updated_at = NOW()
FROM policy
WHERE pricing.billing_mode = 'token'
  AND jsonb_typeof(pricing.models) = 'array'
  AND jsonb_array_length(pricing.models) = 1
  AND LOWER(pricing.models->>0) = policy.model;

UPDATE groups
SET models_list_config = jsonb_set(
        COALESCE(models_list_config, '{}'::jsonb),
        '{models}',
        (
            SELECT COALESCE(jsonb_agg(to_jsonb(model) ORDER BY ordinality), '[]'::jsonb)
            FROM (
                SELECT model, ordinality
                FROM unnest(ARRAY[
                    'deepseek-v4.1-flash',
                    'deepseek-flash',
                    'deepseek-v4-flash-vision-exp'
                ]) WITH ORDINALITY AS seed(model, ordinality)
                WHERE NOT COALESCE(models_list_config->'models', '[]'::jsonb) @> jsonb_build_array(seed.model)
                UNION ALL
                SELECT model, 100 + ordinality
                FROM jsonb_array_elements_text(COALESCE(models_list_config->'models', '[]'::jsonb))
                     WITH ORDINALITY AS existing(model, ordinality)
            ) ranked
        ),
        true
    ),
    updated_at = NOW()
WHERE platform = 'deepseek'
  AND deleted_at IS NULL;

WITH target_channels AS (
    SELECT DISTINCT cg.channel_id
    FROM channel_groups cg
    JOIN groups g ON g.id = cg.group_id
    WHERE g.platform = 'deepseek'
      AND g.deleted_at IS NULL
),
new_models(model) AS (
    VALUES
        ('deepseek-flash'),
        ('deepseek-v4.1-flash'),
        ('deepseek-v4-flash-vision-exp')
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
    created_at,
    updated_at
)
SELECT
    target.channel_id,
    'deepseek',
    jsonb_build_array(new_models.model),
    'token',
    0.000001000000,
    0.000004000000,
    0,
    0.000000020000,
    0,
    0,
    NOW(),
    NOW()
FROM target_channels target
CROSS JOIN new_models
WHERE NOT EXISTS (
    SELECT 1
    FROM channel_model_pricing existing
    WHERE existing.channel_id = target.channel_id
      AND existing.platform = 'deepseek'
      AND existing.models @> jsonb_build_array(new_models.model)
);

UPDATE accounts
SET credentials = jsonb_set(
        credentials,
        '{model_mapping}',
        (credentials->'model_mapping') || jsonb_build_object(
            'deepseek-v4.1-flash', COALESCE(credentials->'model_mapping'->>'deepseek-v4.1-flash', 'deepseek-v4.1-flash'),
            'deepseek-flash', COALESCE(credentials->'model_mapping'->>'deepseek-flash', 'deepseek-flash'),
            'deepseek-v4-flash-vision-exp', COALESCE(credentials->'model_mapping'->>'deepseek-v4-flash-vision-exp', 'deepseek-v4-flash-vision-exp')
        ),
        true
    ),
    updated_at = NOW()
WHERE platform = 'deepseek'
  AND deleted_at IS NULL
  AND jsonb_typeof(credentials->'model_mapping') = 'object'
  AND (
      NOT (credentials->'model_mapping' ? 'deepseek-v4.1-flash')
      OR NOT (credentials->'model_mapping' ? 'deepseek-flash')
      OR NOT (credentials->'model_mapping' ? 'deepseek-v4-flash-vision-exp')
  );
