package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestReforkArchiveGuard(t *testing.T) {
	for _, tc := range []struct {
		name       string
		legacy     bool
		checkpoint bool
		valid      bool
		wantError  bool
	}{
		{"official empty database", false, false, false, false},
		{"unarchived legacy database", true, false, false, true},
		{"invalid checkpoint", true, true, false, true},
		{"archived legacy candidate", true, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectQuery("SELECT EXISTS").
				WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(tc.legacy))
			if tc.legacy {
				mock.ExpectQuery("SELECT to_regclass").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(tc.checkpoint))
				if tc.checkpoint {
					mock.ExpectQuery("SELECT EXISTS").
						WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(tc.valid))
					if tc.valid {
						mock.ExpectQuery("SELECT EXISTS").
							WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
					}
				}
			}
			conn, err := db.Conn(context.Background())
			require.NoError(t, err)
			defer func() { _ = conn.Close() }()
			err = validateReforkArchive(context.Background(), conn)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
