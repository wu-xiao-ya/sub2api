package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestPluginProviderMigrationAndBindingsIntegration(t *testing.T) {
	dsn := os.Getenv("SUB2API_PLUGIN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL fixture not configured")
	}
	u, e := url.Parse(dsn)
	require.NoError(t, e)
	require.Contains(t, []string{"localhost", "127.0.0.1"}, u.Hostname(), "this destructive fixture may only use loopback PostgreSQL")
	admin, e := sql.Open("postgres", dsn)
	require.NoError(t, e)
	defer admin.Close()
	ctx := context.Background()
	schema := fmt.Sprintf("plugin_test_%d", time.Now().UnixNano())
	_, e = admin.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, e)
	defer func() { _, _ = admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE") }()
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := sql.Open("postgres", u.String())
	require.NoError(t, e)
	defer db.Close()
	_, e = db.ExecContext(ctx, `CREATE TABLE users(id BIGINT PRIMARY KEY);CREATE TABLE accounts(id BIGINT PRIMARY KEY,platform TEXT,type TEXT,credentials JSONB NOT NULL DEFAULT '{}',schedulable BOOLEAN NOT NULL DEFAULT TRUE,status TEXT NOT NULL DEFAULT 'active',deleted_at TIMESTAMPTZ);`)
	require.NoError(t, e)
	for _, name := range []string{"229_plugins.sql", "258_plugin_provider_accounts.sql"} {
		raw, e := os.ReadFile("../../migrations/" + name)
		require.NoError(t, e)
		_, e = db.ExecContext(ctx, string(raw))
		require.NoError(t, e)
	}
	_, e = db.ExecContext(ctx, `INSERT INTO accounts(id,platform,type) VALUES(1,'deepseek','plugin'),(2,'deepseek','plugin'),(3,'openai','apikey');INSERT INTO sub2api_plugin_installations(id,plugin_key,name,version,artifact_path,install_path,binary_path,binary_sha256,state) VALUES(7,'test.plugin','test','0.1.0','','','','binary','enabled');`)
	require.NoError(t, e)
	repo := &pluginRepository{db: db}
	b := service.PluginAccountBinding{AccountID: 1, PluginID: 7, IdentityRef: "identity-a"}
	require.NoError(t, repo.BindPluginAccount(ctx, b))
	require.NoError(t, repo.BindPluginAccount(ctx, b))
	b.IdentityRef = "identity-b"
	require.Error(t, repo.BindPluginAccount(ctx, b))
	b.AccountID = 3
	require.Error(t, repo.BindPluginAccount(ctx, b))
	b.AccountID = 2
	b.IdentityRef = "identity-a"
	require.Error(t, repo.BindPluginAccount(ctx, b))
	actual, e := repo.GetPluginAccountBinding(ctx, 1)
	require.NoError(t, e)
	require.Equal(t, "identity-a", actual.IdentityRef)
	require.Error(t, repo.UnbindPluginAccount(ctx, 7, 1))
	_, e = db.ExecContext(ctx, "UPDATE accounts SET schedulable=FALSE WHERE id=1")
	require.NoError(t, e)
	require.NoError(t, repo.UnbindPluginAccount(ctx, 7, 1))
	require.NoError(t, repo.UnbindPluginAccount(ctx, 7, 1))
	_, e = repo.GetPluginAccountBinding(ctx, 1)
	require.ErrorIs(t, e, sql.ErrNoRows)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts").Scan(&count))
	require.Equal(t, 3, count)
	require.NoError(t, repo.WithPluginConfig(ctx, 7, "binary", func(string) (string, error) { return "encrypted-fixture", nil }))
	// A different session must not rotate the same token while its row is locked.
	tx, e := db.BeginTx(ctx, nil)
	require.NoError(t, e)
	defer func() { _ = tx.Rollback() }()
	var encrypted string
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT config_encrypted FROM sub2api_plugin_installations WHERE id=7 FOR UPDATE").Scan(&encrypted))
	blocked, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	called := false
	e = repo.WithPluginConfig(blocked, 7, "binary", func(string) (string, error) { called = true; return "should-not-write", nil })
	require.Error(t, e)
	require.False(t, called)
	require.NoError(t, tx.Rollback())
	require.NoError(t, repo.WithPluginConfig(ctx, 7, "binary", func(current string) (string, error) {
		require.Equal(t, "encrypted-fixture", current)
		return current + "-next", nil
	}))
}
