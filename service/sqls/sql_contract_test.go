package sqls_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDevelopSQLDefinesSignedSingleAdministratorTable(t *testing.T) {
	sqlText, err := os.ReadFile(filepath.Join(sqlsDir(t), "develop", "develop.sql"))
	require.NoError(t, err)
	normalized := strings.ToUpper(string(sqlText))
	require.Contains(t, normalized, "CREATE TABLE ADMINS")
	require.Contains(t, normalized, "ID BIGINT NOT NULL")
	require.Contains(t, normalized, "PRIMARY KEY (ID)")
	require.Contains(t, normalized, "UNIQUE KEY UK_ADMINS_SINGLETON (SINGLETON_KEY)")
	require.Contains(t, normalized, "UNIQUE KEY UK_ADMINS_USERNAME (USERNAME)")
	require.NotContains(t, normalized, "AUTO_INCREMENT")
	require.NotContains(t, normalized, "UNSIGNED")
}

func TestReleaseArchiveStartsWithoutVersionedSQL(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(sqlsDir(t), "releases", "v*.sql"))
	require.NoError(t, err)
	require.Empty(t, files)
}

func sqlsDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Dir(filename)
}
