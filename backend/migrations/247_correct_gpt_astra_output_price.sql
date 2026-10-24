-- Correct the initially published GPT-6 Astra output price.
-- Only touch rows that still match the previous generated defaults; explicit
-- administrator pricing with a different input/cache combination is kept.

UPDATE channel_model_pricing
SET
    output_price = 0.000050000000,
    updated_at = NOW()
WHERE platform = 'openai'
  AND models = '["gpt-6-astra"]'::jsonb
  AND input_price = 0.000010000000
  AND output_price = 0.000060000000
  AND cache_write_price = 0.000012500000
  AND cache_read_price = 0.000001000000;
