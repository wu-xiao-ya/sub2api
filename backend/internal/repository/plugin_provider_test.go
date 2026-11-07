package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPluginProviderConfigLockCommitsOnlySuccessfulRotation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{true: "rollback", false: "commit"}[fail], func(t *testing.T) {
			db, mock, e := sqlmock.New()
			require.NoError(t, e)
			defer db.Close()
			repo := &pluginRepository{db: db}
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT config_encrypted.*FOR UPDATE").WithArgs(int64(7), "binary").WillReturnRows(sqlmock.NewRows([]string{"config_encrypted"}).AddRow("old-encrypted"))
			if fail {
				mock.ExpectRollback()
			} else {
				mock.ExpectExec("UPDATE sub2api_plugin_installations").WithArgs(int64(7), "next-encrypted").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			e = repo.WithPluginConfig(context.Background(), 7, "binary", func(current string) (string, error) {
				require.Equal(t, "old-encrypted", current)
				if fail {
					return "", errors.New("refresh refused")
				}
				return "next-encrypted", nil
			})
			if fail {
				require.Error(t, e)
			} else {
				require.NoError(t, e)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
func TestPluginProviderBindingRejectsSilentRebind(t *testing.T) {
	db, mock, e := sqlmock.New()
	require.NoError(t, e)
	defer db.Close()
	repo := &pluginRepository{db: db}
	mock.ExpectExec("INSERT INTO sub2api_plugin_account_bindings").WithArgs(int64(7), int64(8), "identity").WillReturnResult(sqlmock.NewResult(0, 0))
	require.Error(t, repo.BindPluginAccount(context.Background(), service.PluginAccountBinding{AccountID: 7, PluginID: 8, IdentityRef: "identity"}))
	require.NoError(t, mock.ExpectationsWereMet())
}
