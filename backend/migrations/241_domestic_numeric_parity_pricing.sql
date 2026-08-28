-- Domestic model pricing policy:
-- official mainland CNY price numbers are billed as the same USD numbers.
--
-- Only exact one-model token pricing rows are changed. Multi-model rows are
-- left untouched because one shared row cannot safely represent model-specific
-- prices. Administrators can continue to override any row after this migration.
WITH policy(model, input_price, output_price, cache_read_price) AS (
    VALUES
        ('deepseek-v4-flash',             0.000003000000::numeric, 0.000009000000::numeric, 0.000000100000::numeric),
        ('deepseek-v4-flash-0731',        0.000003000000::numeric, 0.000009000000::numeric, 0.000000100000::numeric),
        ('deepseek-v4-flash-vision-exp',  0.000003000000::numeric, 0.000009000000::numeric, 0.000000100000::numeric),
        ('deepseek-v4-pro',               0.000009000000::numeric, 0.000027000000::numeric, 0.000000300000::numeric),
        ('deepseek-v4-pro-0813',          0.000009000000::numeric, 0.000027000000::numeric, 0.000000300000::numeric),
        ('glm-5.1',                       0.000008000000::numeric, 0.000028000000::numeric, 0.000002000000::numeric),
        ('glm-5.2',                       0.000008000000::numeric, 0.000028000000::numeric, 0.000002000000::numeric),
        ('glm-5.3',                       0.000008000000::numeric, 0.000028000000::numeric, 0.000002000000::numeric),
        ('glm-5.3-flash',                 0.000000800000::numeric, 0.000002800000::numeric, 0.000000230000::numeric),
        ('kimi-k3',                       0.000020000000::numeric, 0.000100000000::numeric, 0.000002000000::numeric),
        ('kimi-k2.7-code',                0.000006500000::numeric, 0.000027000000::numeric, 0.000001300000::numeric),
        ('kimi-k2.7code',                 0.000006500000::numeric, 0.000027000000::numeric, 0.000001300000::numeric),
        ('kimi-k2.7-code-highspeed',      0.000013000000::numeric, 0.000054000000::numeric, 0.000002600000::numeric),
        ('kimi-k2.6',                     0.000006500000::numeric, 0.000027000000::numeric, 0.000001100000::numeric),
        ('kimi-k2.5',                     0.000004000000::numeric, 0.000021000000::numeric, 0.000000700000::numeric),
        ('mimo-v2.5',                     0.000001000000::numeric, 0.000002000000::numeric, 0.000000020000::numeric),
        ('mimo-v2.5-pro',                 0.000003000000::numeric, 0.000006000000::numeric, 0.000000025000::numeric),
        ('hy3',                           0.000001000000::numeric, 0.000004000000::numeric, 0.000000250000::numeric),
        ('hunyuan-hy3',                   0.000001000000::numeric, 0.000004000000::numeric, 0.000000250000::numeric),
        ('qwen3.7-max',                   0.000012000000::numeric, 0.000036000000::numeric, 0.000001200000::numeric),
        ('qwen3.7-plus',                  0.000002000000::numeric, 0.000008000000::numeric, 0.000000400000::numeric),
        ('qwen3.8-max',                   0.000012000000::numeric, 0.000036000000::numeric, 0.000001200000::numeric)
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

-- Qwen 3.7 Plus uses a 3x price tier above 256K input context.
UPDATE channel_pricing_intervals AS interval
SET input_price = CASE WHEN interval.min_tokens >= 256000 THEN 0.000006000000 ELSE 0.000002000000 END,
    output_price = CASE WHEN interval.min_tokens >= 256000 THEN 0.000024000000 ELSE 0.000008000000 END,
    cache_read_price = CASE WHEN interval.min_tokens >= 256000 THEN 0.000001200000 ELSE 0.000000400000 END,
    updated_at = NOW()
FROM channel_model_pricing AS pricing
WHERE interval.pricing_id = pricing.id
  AND pricing.billing_mode = 'token'
  AND jsonb_typeof(pricing.models) = 'array'
  AND jsonb_array_length(pricing.models) = 1
  AND LOWER(pricing.models->>0) = 'qwen3.7-plus';
