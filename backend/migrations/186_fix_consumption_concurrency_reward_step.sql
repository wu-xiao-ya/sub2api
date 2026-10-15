-- Correct lifetime-consumption rewards to +5 concurrency per tier.
--
-- Migration 185 initially granted +4 per tier. The reward column tracks only
-- consumption rewards, so adjust users by the difference and preserve any
-- administrator or redeem-code concurrency changes already present.

WITH desired AS (
    SELECT
        id,
        GREATEST(LEAST(consumption_concurrency_tier, 4), 0) * 5 AS target_bonus
    FROM users
    WHERE deleted_at IS NULL
)
UPDATE users u
SET
    concurrency = u.concurrency + (d.target_bonus - u.consumption_concurrency_bonus),
    consumption_concurrency_bonus = d.target_bonus,
    updated_at = NOW()
FROM desired d
WHERE u.id = d.id
  AND u.consumption_concurrency_bonus <> d.target_bonus;
