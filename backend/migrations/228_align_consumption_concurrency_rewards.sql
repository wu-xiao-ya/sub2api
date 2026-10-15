-- Lifetime-consumption concurrency rewards:
--   >= $50  -> +5  (total +5)
--   >= $100 -> +5  (total +10)
--   >= $200 -> +10 (total +20)
--   >= $500 -> +10 (total +30)
--
-- Keep administrator and redeem-code adjustments intact. A prior deployment
-- could have created users with the temporary default baseline of 5, so only
-- those users are raised to the intended baseline of 10.

WITH desired AS (
    SELECT
        id,
        CASE
            WHEN lifetime_consumption_usd >= 500 THEN 4
            WHEN lifetime_consumption_usd >= 200 THEN 3
            WHEN lifetime_consumption_usd >= 100 THEN 2
            WHEN lifetime_consumption_usd >= 50 THEN 1
            ELSE 0
        END AS target_tier,
        CASE
            WHEN lifetime_consumption_usd >= 500 THEN 30
            WHEN lifetime_consumption_usd >= 200 THEN 20
            WHEN lifetime_consumption_usd >= 100 THEN 10
            WHEN lifetime_consumption_usd >= 50 THEN 5
            ELSE 0
        END AS target_bonus
    FROM users
    WHERE deleted_at IS NULL
)
UPDATE users u
SET
    concurrency = u.concurrency
        + (d.target_bonus - u.consumption_concurrency_bonus)
        + CASE
            WHEN u.concurrency - u.consumption_concurrency_bonus = 5 THEN 5
            ELSE 0
        END,
    consumption_concurrency_tier = d.target_tier,
    consumption_concurrency_bonus = d.target_bonus,
    updated_at = NOW()
FROM desired d
WHERE u.id = d.id
  AND (
      u.consumption_concurrency_tier <> d.target_tier
      OR u.consumption_concurrency_bonus <> d.target_bonus
      OR u.concurrency - u.consumption_concurrency_bonus = 5
  );

INSERT INTO settings (key, value)
VALUES ('default_concurrency', '10')
ON CONFLICT (key) DO UPDATE
SET
    value = EXCLUDED.value,
    updated_at = NOW()
WHERE settings.value IS DISTINCT FROM EXCLUDED.value;
