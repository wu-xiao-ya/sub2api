-- Some deployments may already have a GLM-5.3 channel row copied from
-- GLM-5.2 before migration 238 runs. Correct only that exact legacy price
-- signature so administrator-defined custom prices remain untouched.
UPDATE channel_model_pricing
SET input_price = 0.000000800000,
    output_price = 0.000002560000,
    cache_read_price = 0.000000160000,
    updated_at = NOW()
WHERE platform = 'glm'
  AND models @> '["glm-5.3"]'::jsonb
  AND input_price = 0.000001400000
  AND output_price = 0.000004400000
  AND cache_read_price = 0.000000260000;
