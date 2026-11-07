package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *pluginRepository) GetPluginAccountBinding(ctx context.Context, id int64) (*service.PluginAccountBinding, error) {
	b := &service.PluginAccountBinding{}
	err := r.db.QueryRowContext(ctx, `SELECT account_id, plugin_id, identity_ref FROM sub2api_plugin_account_bindings WHERE account_id=$1`, id).Scan(&b.AccountID, &b.PluginID, &b.IdentityRef)
	return b, err
}
func (r *pluginRepository) BindPluginAccount(ctx context.Context, b service.PluginAccountBinding) error {
	// An account cannot be silently rebound to a different identity.
	result, err := r.db.ExecContext(ctx, `INSERT INTO sub2api_plugin_account_bindings(account_id,plugin_id,identity_ref)
 SELECT a.id,p.id,$3 FROM accounts a CROSS JOIN sub2api_plugin_installations p
 WHERE a.id=$1 AND a.deleted_at IS NULL AND a.platform='deepseek' AND a.type='plugin' AND a.credentials='{}'::jsonb AND p.id=$2 AND p.state='enabled'
 ON CONFLICT(account_id) DO UPDATE SET identity_ref=EXCLUDED.identity_ref
 WHERE sub2api_plugin_account_bindings.plugin_id=EXCLUDED.plugin_id AND sub2api_plugin_account_bindings.identity_ref=EXCLUDED.identity_ref`, b.AccountID, b.PluginID, b.IdentityRef)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("account must be an unbound DeepSeek plugin account with empty credentials")
	}
	return err
}
func (r *pluginRepository) WithPluginConfig(ctx context.Context, id int64, binary string, fn func(string) (string, error)) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var encrypted string
	err = tx.QueryRowContext(ctx, `SELECT config_encrypted FROM sub2api_plugin_installations WHERE id=$1 AND binary_sha256=$2 AND state='enabled' FOR UPDATE`, id, binary).Scan(&encrypted)
	if err != nil {
		return err
	}
	next, err := fn(encrypted)
	if err != nil {
		return err
	}
	if next != encrypted {
		if _, err = tx.ExecContext(ctx, `UPDATE sub2api_plugin_installations SET config_encrypted=$2,updated_at=NOW() WHERE id=$1`, id, next); err != nil {
			return err
		}
	}
	return tx.Commit()
}
