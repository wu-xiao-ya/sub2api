//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration175EnforcesOpenAILongContextBillingWriteInvariant(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("175_default_openai_long_context_billing.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
DROP TRIGGER IF EXISTS accounts_propagate_openai_long_context_billing_extra ON accounts;
DROP TRIGGER IF EXISTS accounts_enforce_openai_long_context_billing_extra ON accounts;
`)
	require.NoError(t, err)

	var ordinaryID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-175-ordinary', 'openai', 'oauth', '{}'::jsonb)
RETURNING id
`).Scan(&ordinaryID))

	var parentID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-175-parent', 'openai', 'oauth', '{"openai_long_context_billing_enabled":false}'::jsonb)
RETURNING id
`).Scan(&parentID))

	var shadowID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra, parent_account_id, quota_dimension)
VALUES ('migration-175-shadow', 'openai', 'oauth', '{}'::jsonb, $1, 'spark')
RETURNING id
`, parentID).Scan(&shadowID))

	var malformedLegacyID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-175-malformed-legacy', 'openai', 'oauth', '{"openai_long_context_billing_enabled":"false"}'::jsonb)
RETURNING id
`).Scan(&malformedLegacyID))

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	var ordinaryEnabled bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra->>'openai_long_context_billing_enabled')::boolean
FROM accounts
WHERE id = $1
`, ordinaryID).Scan(&ordinaryEnabled))
	require.False(t, ordinaryEnabled)

	var shadowEnabled bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra->>'openai_long_context_billing_enabled')::boolean
FROM accounts
WHERE id = $1
`, shadowID).Scan(&shadowEnabled))
	require.False(t, shadowEnabled)

	var initialShadowOutboxEvents int
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM scheduler_outbox
WHERE event_type = 'account_changed' AND account_id = $1
`, shadowID).Scan(&initialShadowOutboxEvents))
	require.Equal(t, 1, initialShadowOutboxEvents)

	var malformedLegacyEnabled bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra->>'openai_long_context_billing_enabled')::boolean
FROM accounts
WHERE id = $1
`, malformedLegacyID).Scan(&malformedLegacyEnabled))
	require.False(t, malformedLegacyEnabled)
	_, err = tx.ExecContext(ctx, `
UPDATE accounts
SET extra = extra || '{"migration_175_unrelated_update":true}'::jsonb
WHERE id = $1
`, malformedLegacyID)
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, "TRUNCATE scheduler_outbox")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE accounts
SET extra = '{"legacy_writer_replaced_extra":true}'::jsonb
WHERE id = $1
`, parentID)
	require.NoError(t, err)
	var parentEnabled bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra->>'openai_long_context_billing_enabled')::boolean
FROM accounts
WHERE id = $1
`, parentID).Scan(&parentEnabled))
	require.False(t, parentEnabled)
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra->>'openai_long_context_billing_enabled')::boolean
FROM accounts
WHERE id = $1
`, shadowID).Scan(&shadowEnabled))
	require.False(t, shadowEnabled)
	var preservedOptOutEvents int
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM scheduler_outbox
WHERE event_type = 'account_changed' AND account_id = $1
`, shadowID).Scan(&preservedOptOutEvents))
	require.Zero(t, preservedOptOutEvents)

	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-175-rolling-writer', 'openai', 'oauth', '{}'::jsonb)
RETURNING (extra->>'openai_long_context_billing_enabled')::boolean
`).Scan(&ordinaryEnabled))
	require.False(t, ordinaryEnabled)

	_, err = tx.ExecContext(ctx, "TRUNCATE scheduler_outbox")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE accounts
SET extra = jsonb_set(extra, '{openai_long_context_billing_enabled}', 'true'::jsonb, true)
WHERE id = $1
`, parentID)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra->>'openai_long_context_billing_enabled')::boolean
FROM accounts
WHERE id = $1
`, shadowID).Scan(&shadowEnabled))
	require.True(t, shadowEnabled)

	var shadowOutboxEvents int
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM scheduler_outbox
WHERE event_type = 'account_changed' AND account_id = $1
`, shadowID).Scan(&shadowOutboxEvents))
	require.Equal(t, 1, shadowOutboxEvents)

	_, err = tx.ExecContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-175-malformed', 'openai', 'oauth', '{"openai_long_context_billing_enabled":"false"}'::jsonb)
`)
	require.ErrorContains(t, err, "openai_long_context_billing_enabled must be a boolean")
}

func TestMigration230EnablesOpenAILongContextBillingAndRefreshesSchedulerCache(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("230_enable_openai_long_context_billing_by_default.sql")
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, `
DROP TRIGGER IF EXISTS accounts_propagate_openai_long_context_billing_extra ON accounts;
DROP TRIGGER IF EXISTS accounts_enforce_openai_long_context_billing_extra ON accounts;
TRUNCATE scheduler_outbox;
`)
	require.NoError(t, err)

	var ordinaryID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-230-ordinary', 'openai', 'oauth', '{}'::jsonb)
RETURNING id
`).Scan(&ordinaryID))

	var parentID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-230-parent', 'openai', 'oauth', '{"openai_long_context_billing_enabled":false}'::jsonb)
RETURNING id
`).Scan(&parentID))

	var shadowID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra, parent_account_id, quota_dimension)
VALUES ('migration-230-shadow', 'openai', 'oauth', '{}'::jsonb, $1, 'spark')
RETURNING id
`, parentID).Scan(&shadowID))

	var malformedID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-230-malformed', 'openai', 'oauth', '{"openai_long_context_billing_enabled":"false"}'::jsonb)
RETURNING id
`).Scan(&malformedID))

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	for _, accountID := range []int64{ordinaryID, parentID, shadowID, malformedID} {
		var enabled bool
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra->>'openai_long_context_billing_enabled')::boolean
FROM accounts
WHERE id = $1
`, accountID).Scan(&enabled))
		require.True(t, enabled, "account %d should be enabled by migration 230", accountID)
	}

	var migrationOutboxEvents int
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM scheduler_outbox
WHERE event_type = 'account_changed'
  AND account_id IN ($1, $2, $3, $4)
`, ordinaryID, parentID, shadowID, malformedID).Scan(&migrationOutboxEvents))
	require.Equal(t, 4, migrationOutboxEvents)

	var postMigrationDefault bool
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-230-new-default', 'openai', 'oauth', '{}'::jsonb)
RETURNING (extra->>'openai_long_context_billing_enabled')::boolean
`).Scan(&postMigrationDefault))
	require.True(t, postMigrationDefault)

	var optOutParentID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-230-opted-out-parent', 'openai', 'oauth', '{"openai_long_context_billing_enabled":false}'::jsonb)
RETURNING id
`).Scan(&optOutParentID))

	var optOutShadowID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra, parent_account_id, quota_dimension)
VALUES ('migration-230-opted-out-shadow', 'openai', 'oauth', '{}'::jsonb, $1, 'spark')
RETURNING id
`, optOutParentID).Scan(&optOutShadowID))
	var optOutShadowEnabled bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra->>'openai_long_context_billing_enabled')::boolean
FROM accounts
WHERE id = $1
`, optOutShadowID).Scan(&optOutShadowEnabled))
	require.False(t, optOutShadowEnabled)

	_, err = tx.ExecContext(ctx, "TRUNCATE scheduler_outbox")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
UPDATE accounts
SET extra = '{"unrelated_update":true}'::jsonb
WHERE id = $1
`, optOutParentID)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra->>'openai_long_context_billing_enabled')::boolean
FROM accounts
WHERE id = $1
`, optOutParentID).Scan(&postMigrationDefault))
	require.False(t, postMigrationDefault)
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra->>'openai_long_context_billing_enabled')::boolean
FROM accounts
WHERE id = $1
`, optOutShadowID).Scan(&optOutShadowEnabled))
	require.False(t, optOutShadowEnabled)

	var optOutShadowEvents int
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM scheduler_outbox
WHERE event_type = 'account_changed' AND account_id = $1
`, optOutShadowID).Scan(&optOutShadowEvents))
	require.Zero(t, optOutShadowEvents)

	_, err = tx.ExecContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-230-malformed-new', 'openai', 'oauth', '{"openai_long_context_billing_enabled":"false"}'::jsonb)
`)
	require.ErrorContains(t, err, "openai_long_context_billing_enabled must be a boolean")
}
