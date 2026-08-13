package sqls_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var stageTwoTables = []string{
	"media",
	"tags",
	"articles",
	"article_revisions",
	"article_revision_tags",
	"article_revision_media",
	"site_settings",
	"hotlink_settings",
	"referer_allowlist",
}

func TestDevelopSQLDefinesSignedSingleAdministratorTable(t *testing.T) {
	normalized := readDevelopSQL(t)
	require.Contains(t, normalized, "CREATE TABLE ADMINS")
	require.Contains(t, normalized, "ID BIGINT NOT NULL")
	require.Contains(t, normalized, "PRIMARY KEY (ID)")
	require.Contains(t, normalized, "UNIQUE KEY UK_ADMINS_SINGLETON (SINGLETON_KEY)")
	require.Contains(t, normalized, "UNIQUE KEY UK_ADMINS_USERNAME (USERNAME)")
	require.NotContains(t, normalized, "AUTO_INCREMENT")
	require.NotContains(t, normalized, "UNSIGNED")
}

func TestDevelopSQLDefinesSignedStageTwoTables(t *testing.T) {
	normalized := readDevelopSQL(t)

	for _, table := range stageTwoTables {
		t.Run(table, func(t *testing.T) {
			definition := tableDefinition(t, normalized, table)
			require.Contains(t, definition, "ID BIGINT NOT NULL")
			require.Contains(t, definition, "PRIMARY KEY (ID)")
		})
	}

	require.NotContains(t, normalized, "AUTO_INCREMENT")
	require.NotContains(t, normalized, "UNSIGNED")
}

func TestDevelopSQLDefinesStageTwoConstraints(t *testing.T) {
	normalized := readDevelopSQL(t)
	expected := map[string][]string{
		"media": {
			"UNIQUE KEY UK_MEDIA_PUBLIC_KEY (PUBLIC_KEY)",
			"UNIQUE KEY UK_MEDIA_GFS_FILE_ID (GFS_FILE_ID)",
			"CONSTRAINT CHK_MEDIA_STATE CHECK (STATE IN ('ACTIVE', 'INACTIVE'))",
			"CONSTRAINT CHK_MEDIA_FILE_SIZE CHECK (FILE_SIZE > 0)",
			"CONSTRAINT CHK_MEDIA_DIMENSIONS CHECK (WIDTH > 0 AND HEIGHT > 0)",
		},
		"tags": {
			"UNIQUE KEY UK_TAGS_NAME (NAME)",
			"UNIQUE KEY UK_TAGS_SLUG (SLUG)",
		},
		"articles": {
			"UNIQUE KEY UK_ARTICLES_SLUG (SLUG)",
			"CONSTRAINT CHK_ARTICLES_STATE CHECK (STATE IN ('ACTIVE', 'TRASHED'))",
		},
		"article_revisions": {
			"UNIQUE KEY UK_ARTICLE_REVISIONS_NO (ARTICLE_ID, REVISION_NO)",
			"UNIQUE KEY UK_ARTICLE_REVISIONS_EDITING (EDITING_ARTICLE_ID)",
			"CONSTRAINT CHK_ARTICLE_REVISIONS_STATUS CHECK (STATUS IN ('EDITING', 'FROZEN'))",
			"CONSTRAINT CHK_ARTICLE_REVISIONS_REASON CHECK (REASON IN ('DRAFT', 'MANUAL_VERSION', 'PUBLISH_SNAPSHOT'))",
			"CONSTRAINT CHK_ARTICLE_REVISIONS_LOCK_VERSION CHECK (LOCK_VERSION > 0)",
		},
		"article_revision_tags": {
			"UNIQUE KEY UK_ARTICLE_REVISION_TAGS_POSITION (REVISION_ID, POSITION)",
			"CONSTRAINT CHK_ARTICLE_REVISION_TAGS_POSITION CHECK (POSITION >= 0)",
		},
		"article_revision_media": {
			"UNIQUE KEY UK_ARTICLE_REVISION_MEDIA_POSITION (REVISION_ID, POSITION)",
			"CONSTRAINT CHK_ARTICLE_REVISION_MEDIA_PURPOSE CHECK (PURPOSE IN ('CONTENT', 'COVER'))",
			"CONSTRAINT CHK_ARTICLE_REVISION_MEDIA_POSITION CHECK (POSITION >= 0)",
		},
		"site_settings": {
			"UNIQUE KEY UK_SITE_SETTINGS_SINGLETON (SINGLETON_KEY)",
			"CONSTRAINT CHK_SITE_SETTINGS_SINGLETON CHECK (SINGLETON_KEY = 1)",
			"CONSTRAINT CHK_SITE_SETTINGS_LOCK_VERSION CHECK (LOCK_VERSION > 0)",
		},
		"hotlink_settings": {
			"UNIQUE KEY UK_HOTLINK_SETTINGS_SINGLETON (SINGLETON_KEY)",
			"CONSTRAINT CHK_HOTLINK_SETTINGS_SINGLETON CHECK (SINGLETON_KEY = 1)",
		},
		"referer_allowlist": {
			"UNIQUE KEY UK_REFERER_ALLOWLIST_HOSTNAME (HOSTNAME)",
		},
	}

	for table, fragments := range expected {
		t.Run(table, func(t *testing.T) {
			definition := tableDefinition(t, normalized, table)
			for _, fragment := range fragments {
				require.Contains(t, definition, fragment)
			}
		})
	}

	require.Contains(t, normalized, "EDITING_ARTICLE_ID BIGINT GENERATED ALWAYS AS (CASE WHEN STATUS = 'EDITING' THEN ARTICLE_ID ELSE NULL END) STORED")
	require.Contains(t, normalized, "CONTENT_MD LONGTEXT NOT NULL")
	require.Contains(t, normalized, "CONTENT_HASH CHAR(64) NOT NULL")
	require.Contains(t, normalized, "SOCIAL_LINKS_JSON JSON NOT NULL")
	require.Contains(t, normalized, "DATETIME(6)")
	require.Contains(t, normalized, "ON DELETE RESTRICT")
}

func TestDevelopSQLContainsNoSeedDataOrMigrationCommand(t *testing.T) {
	normalized := readDevelopSQL(t)
	for _, line := range strings.Split(normalized, "\n") {
		require.False(t, strings.HasPrefix(strings.TrimSpace(line), "INSERT INTO "))
	}

	migrationCommands, err := filepath.Glob(filepath.Join(sqlsDir(t), "..", "cmd", "*migrat*"))
	require.NoError(t, err)
	require.Empty(t, migrationCommands)
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

func readDevelopSQL(t *testing.T) string {
	t.Helper()
	sqlText, err := os.ReadFile(filepath.Join(sqlsDir(t), "develop", "develop.sql"))
	require.NoError(t, err)
	return strings.ToUpper(string(sqlText))
}

func tableDefinition(t *testing.T, normalizedSQL, table string) string {
	t.Helper()
	startMarker := "CREATE TABLE " + strings.ToUpper(table) + " ("
	start := strings.Index(normalizedSQL, startMarker)
	require.NotEqual(t, -1, start, "missing table %s", table)

	remainder := normalizedSQL[start:]
	end := strings.Index(remainder, ") ENGINE=INNODB")
	require.NotEqual(t, -1, end, "unterminated table %s", table)
	return remainder[:end]
}
