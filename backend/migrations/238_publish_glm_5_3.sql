-- Publish GLM-5.3 across the maintained GLM catalog and existing channel
-- pricing rows. The upstream account mapping is managed separately by admins;
-- this migration only exposes the model where the deployment already offers
-- the GLM platform.

UPDATE groups
SET models_list_config = jsonb_set(
        COALESCE(models_list_config, '{}'::jsonb),
        '{models}',
        jsonb_build_array('glm-5.3') ||
            COALESCE(models_list_config->'models', '[]'::jsonb),
        true
    ),
    updated_at = NOW()
WHERE platform = 'glm'
  AND NOT COALESCE(models_list_config->'models', '[]'::jsonb) @> '["glm-5.3"]'::jsonb;

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
    'glm',
    '["glm-5.3"]'::jsonb,
    'token',
    0.000000800000,
    0.000002560000,
    0.000000000000,
    0.000000160000,
    0.000000000000,
    0.000000000000,
    NULL::numeric,
    NOW(),
    NOW()
FROM channel_groups cg
JOIN groups g ON g.id = cg.group_id
WHERE g.platform = 'glm'
  AND g.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM channel_model_pricing cmp
      WHERE cmp.channel_id = cg.channel_id
        AND cmp.platform = 'glm'
        AND cmp.models @> '["glm-5.3"]'::jsonb
  );
