-- Publish GLM-5.3-Flash to existing GLM groups and channels.
--
-- Existing administrator pricing is preserved: a default pricing row is only
-- inserted when the channel does not already contain this model.

UPDATE groups
SET models_list_config = jsonb_set(
        COALESCE(models_list_config, '{}'::jsonb),
        '{models}',
        jsonb_build_array('glm-5.3-flash') ||
            CASE
                WHEN jsonb_typeof(models_list_config->'models') = 'array'
                    THEN models_list_config->'models'
                ELSE '[]'::jsonb
            END,
        true
    ),
    updated_at = NOW()
WHERE platform = 'glm'
  AND deleted_at IS NULL
  AND NOT COALESCE(models_list_config->'models', '[]'::jsonb) @> '["glm-5.3-flash"]'::jsonb;

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
    '["glm-5.3-flash"]'::jsonb,
    'token',
    0.000000800000,
    0.000002800000,
    0.000000000000,
    0.000000230000,
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
        AND cmp.models @> '["glm-5.3-flash"]'::jsonb
  );
