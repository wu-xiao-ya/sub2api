-- Correct historical cost/profit reporting once using each account's current
-- upstream billing probe rate.
--
-- The marker is intentionally placed at the beginning of the supported
-- timestamp range so historical usage rows without a probe history resolve to
-- the current snapshot. New probe snapshots with real observed_at values take
-- precedence for future buckets.
WITH current_rates AS (
    SELECT
        a.id AS account_id,
        CASE
            WHEN jsonb_typeof(a.extra #> '{upstream_billing_probe,data,effective_rate_multiplier}') = 'number'
            THEN (a.extra #>> '{upstream_billing_probe,data,effective_rate_multiplier}')::numeric
        END AS effective_rate_multiplier
    FROM accounts a
    WHERE a.deleted_at IS NULL
),
account_group_scope AS (
    SELECT ag.account_id, ag.group_id
    FROM account_groups ag
    UNION ALL
    SELECT a.id, 0
    FROM accounts a
    WHERE a.deleted_at IS NULL
      AND NOT EXISTS (
          SELECT 1
          FROM account_groups ag
          WHERE ag.account_id = a.id
      )
)
INSERT INTO account_upstream_rate_snapshots (
    account_id,
    group_id,
    effective_rate_multiplier,
    observed_at,
    captured_at,
    source
)
SELECT
    cr.account_id,
    ags.group_id,
    cr.effective_rate_multiplier,
    TIMESTAMPTZ '1970-01-01 00:00:00+00',
    NOW(),
    'current_snapshot_backfill'
FROM current_rates cr
JOIN account_group_scope ags ON ags.account_id = cr.account_id
WHERE cr.effective_rate_multiplier IS NOT NULL
  AND cr.effective_rate_multiplier >= 0
ON CONFLICT (account_id, group_id, observed_at)
DO UPDATE SET
    effective_rate_multiplier = EXCLUDED.effective_rate_multiplier,
    captured_at = EXCLUDED.captured_at,
    source = EXCLUDED.source;
