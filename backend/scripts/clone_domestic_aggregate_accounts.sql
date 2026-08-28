\set ON_ERROR_STOP on

-- Usage:
--   psql "$DATABASE_URL" \
--     -v source_account_id=183 \
--     -f backend/scripts/clone_domestic_aggregate_accounts.sql
--
-- The source credential remains inside PostgreSQL. No API key is selected or
-- printed by this script.

BEGIN;

CREATE TEMP TABLE domestic_account_seeds (
    platform text PRIMARY KEY,
    account_name text NOT NULL,
    group_name text NOT NULL,
    model_mapping jsonb NOT NULL,
    primary_model text NOT NULL,
    extra_models jsonb NOT NULL
) ON COMMIT DROP;

INSERT INTO domestic_account_seeds (
    platform, account_name, group_name, model_mapping, primary_model, extra_models
)
VALUES
    ('qwen', 'aggregate Qwen', 'Qwen',
        '{"qwen3.8-max":"qwen3.8-max","qwen3.7-max":"qwen3.7-max","qwen3.7-plus":"qwen3.7-plus"}'::jsonb,
        'qwen3.7-plus', '["qwen3.7-max","qwen3.8-max"]'::jsonb),
    ('minimax', 'aggregate MiniMax', 'MiniMax',
        '{"minimax-m3":"minimax-m3","minimax-m2.7":"minimax-m2.7","minimax-m2.7-highspeed":"minimax-m2.7-highspeed"}'::jsonb,
        'minimax-m3', '["minimax-m2.7","minimax-m2.7-highspeed"]'::jsonb),
    ('mimo', 'aggregate MiMo', 'MiMo',
        '{"mimo-v2.5-pro":"mimo-v2.5-pro","mimo-v2.5":"mimo-v2.5"}'::jsonb,
        'mimo-v2.5', '["mimo-v2.5-pro"]'::jsonb),
    ('hunyuan', 'aggregate Hunyuan', 'Hunyuan',
        '{"hy3":"hy3","hunyuan-hy3":"hy3"}'::jsonb,
        'hy3', '["hunyuan-hy3"]'::jsonb);

SELECT EXISTS (
    SELECT 1
    FROM accounts
    WHERE id = :'source_account_id'::bigint
      AND deleted_at IS NULL
      AND platform = 'glm'
      AND type = 'apikey'
) AS source_account_ok
\gset

\if :source_account_ok
\else
    \echo 'GLM source account is missing or invalid'
    \quit 3
\endif

INSERT INTO accounts (
    name,
    notes,
    platform,
    type,
    credentials,
    extra,
    proxy_id,
    concurrency,
    load_factor,
    priority,
    rate_multiplier,
    status,
    expires_at,
    auto_pause_on_expired,
    schedulable,
    quota_dimension,
    created_at,
    updated_at
)
SELECT seed.account_name,
       source.notes,
       seed.platform,
       source.type,
       jsonb_set(source.credentials, '{model_mapping}', seed.model_mapping, true),
       jsonb_set(COALESCE(source.extra, '{}'::jsonb), '{monitor_only}', 'true'::jsonb, true),
       source.proxy_id,
       source.concurrency,
       source.load_factor,
       source.priority,
       source.rate_multiplier,
       source.status,
       source.expires_at,
       source.auto_pause_on_expired,
       false,
       source.quota_dimension,
       NOW(),
       NOW()
FROM accounts source
CROSS JOIN domestic_account_seeds seed
WHERE source.id = :'source_account_id'::bigint
  AND source.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM accounts existing
      WHERE existing.deleted_at IS NULL
        AND existing.platform = seed.platform
        AND existing.name = seed.account_name
  );

INSERT INTO account_groups (account_id, group_id, priority, created_at)
SELECT account.id, group_row.id, account.priority, NOW()
FROM domestic_account_seeds seed
JOIN accounts account
  ON account.deleted_at IS NULL
 AND account.platform = seed.platform
 AND account.name = seed.account_name
JOIN groups group_row
  ON group_row.deleted_at IS NULL
 AND group_row.platform = seed.platform
 AND LOWER(group_row.name) = LOWER(seed.group_name)
WHERE NOT EXISTS (
    SELECT 1
    FROM account_groups existing
    WHERE existing.account_id = account.id
      AND existing.group_id = group_row.id
);

-- Copy the working GLM monitor configuration but keep new monitors disabled
-- until each copied account has passed a real manual test.
WITH source_monitor AS (
    SELECT monitor.*
    FROM channel_monitors monitor
    WHERE monitor.provider = 'glm'
    ORDER BY monitor.enabled DESC, monitor.id
    LIMIT 1
)
INSERT INTO channel_monitors (
    name,
    provider,
    api_mode,
    endpoint,
    api_key_encrypted,
    primary_model,
    extra_models,
    group_name,
    account_group_id,
    enabled,
    interval_seconds,
    jitter_seconds,
    request_timeout_seconds,
    last_checked_at,
    created_by,
    extra_headers,
    body_override_mode,
    body_override,
    template_id,
    created_at,
    updated_at
)
SELECT seed.account_name || ' monitor',
       seed.platform,
       source.api_mode,
       source.endpoint,
       source.api_key_encrypted,
       seed.primary_model,
       seed.extra_models,
       group_row.name,
       group_row.id,
       false,
       source.interval_seconds,
       source.jitter_seconds,
       source.request_timeout_seconds,
       NULL,
       source.created_by,
       source.extra_headers,
       source.body_override_mode,
       source.body_override,
       NULL,
       NOW(),
       NOW()
FROM source_monitor source
CROSS JOIN domestic_account_seeds seed
JOIN groups group_row
  ON group_row.deleted_at IS NULL
 AND group_row.platform = seed.platform
 AND LOWER(group_row.name) = LOWER(seed.group_name)
WHERE NOT EXISTS (
    SELECT 1
    FROM channel_monitors existing
    WHERE existing.provider = seed.platform
      AND existing.account_group_id = group_row.id
);

COMMIT;

SELECT account.id,
       account.name,
       account.platform,
       account.schedulable,
       account.concurrency
FROM accounts account
WHERE account.deleted_at IS NULL
  AND account.platform IN ('qwen', 'minimax', 'mimo', 'hunyuan')
ORDER BY account.platform, account.id;
