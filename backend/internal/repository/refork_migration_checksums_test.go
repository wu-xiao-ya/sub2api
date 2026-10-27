package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestReforkMigrationChecksumsAreDirectionalAndPinned(t *testing.T) {
	for name, rule := range reforkMigrationChecksumCompatibilityRules {
		t.Run(name, func(t *testing.T) {
			content, err := fs.ReadFile(migrations.FS, name)
			require.NoError(t, err)
			digest := sha256.Sum256([]byte(strings.TrimSpace(string(content))))
			require.Equal(t, rule.fileChecksum, hex.EncodeToString(digest[:]))
			for dbChecksum := range rule.acceptedDBChecksum {
				require.True(t, isReforkMigrationChecksumCompatible(name, dbChecksum, rule.fileChecksum))
				require.False(t, isReforkMigrationChecksumCompatible(name, rule.fileChecksum, dbChecksum))
				require.False(t, isReforkMigrationChecksumCompatible(name, dbChecksum, strings.Repeat("0", 64)))
				require.False(t, isReforkMigrationChecksumCompatible("other.sql", dbChecksum, rule.fileChecksum))
			}
		})
	}
}
