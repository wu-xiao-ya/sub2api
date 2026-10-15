-- Publish independently managed domestic aggregate platforms.
--
-- Credentials are intentionally excluded. Production account cloning is a
-- separate, operator-triggered step so secrets never enter source control.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN (
        'anthropic', 'openai', 'gemini', 'antigravity', 'grok',
        'deepseek', 'kimi', 'glm', 'qwen', 'minimax', 'mimo', 'hunyuan'
    ));

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN (
        'openai', 'anthropic', 'gemini', 'grok', 'antigravity',
        'deepseek', 'kimi', 'glm', 'qwen', 'minimax', 'mimo', 'hunyuan'
    ));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN (
        'openai', 'anthropic', 'gemini', 'grok',
        'deepseek', 'kimi', 'glm', 'qwen', 'minimax', 'mimo', 'hunyuan'
    ));

UPDATE groups
SET rate_multiplier = CASE platform
        WHEN 'deepseek' THEN 0.6000
        WHEN 'kimi' THEN 0.4500
        WHEN 'glm' THEN 0.3000
        ELSE rate_multiplier
    END,
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND (
      (platform = 'deepseek' AND LOWER(name) = 'deepseek')
      OR (platform = 'kimi' AND LOWER(name) = 'kimi')
      OR (platform = 'glm' AND LOWER(name) = 'glm')
  );

INSERT INTO groups (
    name,
    description,
    platform,
    rate_multiplier,
    status,
    models_list_config,
    created_at,
    updated_at
)
SELECT seed.name,
       seed.description,
       seed.platform,
       seed.rate_multiplier,
       'active',
       jsonb_build_object('enabled', true, 'models', seed.models),
       NOW(),
       NOW()
FROM (
    VALUES
        ('Qwen', 'Domestic aggregate Qwen models', 'qwen', 0.5000::numeric,
            '["qwen3.8-max","qwen3.7-max","qwen3.7-plus"]'::jsonb),
        ('MiniMax', 'Domestic aggregate MiniMax models', 'minimax', 0.3000::numeric,
            '["minimax-m3","minimax-m2.7","minimax-m2.7-highspeed"]'::jsonb),
        ('MiMo', 'Domestic aggregate MiMo models', 'mimo', 0.3000::numeric,
            '["mimo-v2.5-pro","mimo-v2.5"]'::jsonb),
        ('Hunyuan', 'Domestic aggregate Hunyuan models', 'hunyuan', 0.3000::numeric,
            '["hy3","hunyuan-hy3"]'::jsonb)
) AS seed(name, description, platform, rate_multiplier, models)
WHERE NOT EXISTS (
    SELECT 1
    FROM groups g
    WHERE g.deleted_at IS NULL
      AND g.platform = seed.platform
      AND LOWER(g.name) = LOWER(seed.name)
);

-- Keep an existing administrator-created group but complete its maintained
-- model catalog and requested initial rate.
WITH seed(platform, name, rate_multiplier, models) AS (
    VALUES
        ('qwen', 'Qwen', 0.5000::numeric,
            '["qwen3.8-max","qwen3.7-max","qwen3.7-plus"]'::jsonb),
        ('minimax', 'MiniMax', 0.3000::numeric,
            '["minimax-m3","minimax-m2.7","minimax-m2.7-highspeed"]'::jsonb),
        ('mimo', 'MiMo', 0.3000::numeric,
            '["mimo-v2.5-pro","mimo-v2.5"]'::jsonb),
        ('hunyuan', 'Hunyuan', 0.3000::numeric,
            '["hy3","hunyuan-hy3"]'::jsonb)
)
UPDATE groups g
SET rate_multiplier = seed.rate_multiplier,
    models_list_config = jsonb_build_object('enabled', true, 'models', seed.models),
    updated_at = NOW()
FROM seed
WHERE g.deleted_at IS NULL
  AND g.platform = seed.platform
  AND LOWER(g.name) = LOWER(seed.name);

-- Reuse the channel already attached to the working GLM aggregate group.
WITH aggregate_channel AS (
    SELECT cg.channel_id
    FROM channel_groups cg
    JOIN groups g ON g.id = cg.group_id
    WHERE g.deleted_at IS NULL
      AND g.platform = 'glm'
    ORDER BY cg.channel_id
    LIMIT 1
)
INSERT INTO channel_groups (channel_id, group_id, created_at)
SELECT aggregate_channel.channel_id, g.id, NOW()
FROM aggregate_channel
JOIN groups g ON g.deleted_at IS NULL
    AND g.platform IN ('qwen', 'minimax', 'mimo', 'hunyuan')
WHERE NOT EXISTS (
    SELECT 1 FROM channel_groups existing WHERE existing.group_id = g.id
);

WITH prices(platform, model, input_price, output_price, cache_write_price, cache_read_price) AS (
    VALUES
        ('qwen', 'qwen3.8-max', 0.000012000000::numeric, 0.000036000000::numeric, 0::numeric, 0.000001200000::numeric),
        ('qwen', 'qwen3.7-max', 0.000012000000::numeric, 0.000036000000::numeric, 0::numeric, 0.000001200000::numeric),
        ('qwen', 'qwen3.7-plus', 0.000002000000::numeric, 0.000008000000::numeric, 0::numeric, 0.000000400000::numeric),
        ('minimax', 'minimax-m3', 0.000002000000::numeric, 0.000008000000::numeric, 0.000001250000::numeric, 0.000000200000::numeric),
        ('minimax', 'minimax-m2.7', 0.000002000000::numeric, 0.000008000000::numeric, 0.000001250000::numeric, 0.000000200000::numeric),
        ('minimax', 'minimax-m2.7-highspeed', 0.000002400000::numeric, 0.000009600000::numeric, 0.000001500000::numeric, 0.000000240000::numeric),
        ('mimo', 'mimo-v2.5', 0.000001000000::numeric, 0.000002000000::numeric, 0::numeric, 0.000000020000::numeric),
        ('mimo', 'mimo-v2.5-pro', 0.000003000000::numeric, 0.000006000000::numeric, 0::numeric, 0.000000025000::numeric),
        ('hunyuan', 'hy3', 0.000001000000::numeric, 0.000004000000::numeric, 0::numeric, 0.000000250000::numeric),
        ('hunyuan', 'hunyuan-hy3', 0.000001000000::numeric, 0.000004000000::numeric, 0::numeric, 0.000000250000::numeric)
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
SELECT DISTINCT
    cg.channel_id,
    prices.platform,
    jsonb_build_array(prices.model),
    'token',
    prices.input_price,
    prices.output_price,
    prices.cache_write_price,
    prices.cache_read_price,
    0,
    0,
    NULL::numeric,
    NOW(),
    NOW()
FROM prices
JOIN groups g ON g.deleted_at IS NULL AND g.platform = prices.platform
JOIN channel_groups cg ON cg.group_id = g.id
WHERE NOT EXISTS (
    SELECT 1
    FROM channel_model_pricing existing
    WHERE existing.channel_id = cg.channel_id
      AND existing.platform = prices.platform
      AND existing.models @> jsonb_build_array(prices.model)
);

-- Qwen 3.7 Plus bills input/output/cache at 3x from 256K input tokens.
INSERT INTO channel_pricing_intervals (
    pricing_id,
    min_tokens,
    max_tokens,
    tier_label,
    input_price,
    output_price,
    cache_write_price,
    cache_read_price,
    sort_order,
    created_at,
    updated_at
)
SELECT pricing.id,
       interval.min_tokens,
       interval.max_tokens,
       interval.tier_label,
       interval.input_price,
       interval.output_price,
       0,
       interval.cache_read_price,
       interval.sort_order,
       NOW(),
       NOW()
FROM channel_model_pricing pricing
CROSS JOIN (
    VALUES
        (0, 256000, 'standard', 0.000002000000::numeric, 0.000008000000::numeric, 0.000000400000::numeric, 0),
        (256000, NULL::integer, 'long_context', 0.000006000000::numeric, 0.000024000000::numeric, 0.000001200000::numeric, 1)
) AS interval(min_tokens, max_tokens, tier_label, input_price, output_price, cache_read_price, sort_order)
WHERE pricing.platform = 'qwen'
  AND pricing.models = '["qwen3.7-plus"]'::jsonb
  AND NOT EXISTS (
      SELECT 1
      FROM channel_pricing_intervals existing
      WHERE existing.pricing_id = pricing.id
        AND existing.min_tokens = interval.min_tokens
        AND existing.max_tokens IS NOT DISTINCT FROM interval.max_tokens
  );
