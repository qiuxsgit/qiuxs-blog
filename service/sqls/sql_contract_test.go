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
	require.Contains(t, normalized, "CONTENT_HASH CHAR(64) CHARACTER SET ASCII COLLATE ASCII_BIN NOT NULL")
	require.Contains(t, normalized, "SOCIAL_LINKS_JSON JSON NOT NULL")
	require.Contains(t, normalized, "DATETIME(6)")
	require.Contains(t, normalized, "ON DELETE RESTRICT")
}

func TestDevelopSQLUsesCaseSensitiveMachineDomains(t *testing.T) {
	raw := readDevelopSQLRaw(t)
	expectedColumns := map[string][]string{
		"admins": {
			"state VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'active'",
		},
		"media": {
			"public_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
			"state VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'active'",
		},
		"tags": {
			"slug VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
		},
		"articles": {
			"slug VARCHAR(12) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
			"state VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'active'",
		},
		"article_revisions": {
			"status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
			"reason VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
			"content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
		},
		"article_revision_tags": {
			"tag_slug VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
		},
		"article_revision_media": {
			"purpose VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
		},
	}
	for table, columns := range expectedColumns {
		t.Run(table, func(t *testing.T) {
			definition := rawTableDefinition(t, raw, table)
			for _, column := range columns {
				require.Contains(t, definition, column)
			}
		})
	}

	expectedRegexChecks := []string{
		"CONSTRAINT chk_media_public_key CHECK (REGEXP_LIKE(public_key, '^m_[a-z0-9_-]{22}$', 'c'))",
		"CONSTRAINT chk_tags_slug CHECK (REGEXP_LIKE(slug, '^t_[a-z0-9_-]{12}$', 'c'))",
		"CONSTRAINT chk_articles_slug CHECK (REGEXP_LIKE(slug, '^[a-z0-9_-]{12}$', 'c'))",
		"CONSTRAINT chk_article_revisions_hash CHECK (REGEXP_LIKE(content_hash, '^[a-f0-9]{64}$', 'c'))",
	}
	for _, check := range expectedRegexChecks {
		require.Contains(t, raw, check)
	}

	require.NotContains(t, raw, "name VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin")
	require.NotContains(t, raw, "tag_name VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin")
	require.NotContains(t, raw, "title VARCHAR(200) CHARACTER SET ascii COLLATE ascii_bin")
}

func TestDevelopSQLDefinesExactStageTwoForeignKeys(t *testing.T) {
	normalized := readDevelopSQL(t)
	expected := []string{
		"CONSTRAINT FK_ARTICLE_REVISIONS_ARTICLE FOREIGN KEY (ARTICLE_ID) REFERENCES ARTICLES (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_ARTICLE_REVISIONS_COVER_MEDIA FOREIGN KEY (COVER_MEDIA_ID) REFERENCES MEDIA (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_ARTICLE_REVISION_TAGS_REVISION FOREIGN KEY (REVISION_ID) REFERENCES ARTICLE_REVISIONS (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_ARTICLE_REVISION_TAGS_TAG FOREIGN KEY (TAG_ID) REFERENCES TAGS (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_ARTICLE_REVISION_MEDIA_REVISION FOREIGN KEY (REVISION_ID) REFERENCES ARTICLE_REVISIONS (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_ARTICLE_REVISION_MEDIA_MEDIA FOREIGN KEY (MEDIA_ID) REFERENCES MEDIA (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_SITE_SETTINGS_SEO_MEDIA FOREIGN KEY (SEO_DEFAULT_IMAGE_MEDIA_ID) REFERENCES MEDIA (ID) ON DELETE RESTRICT",
		"ADD CONSTRAINT FK_ARTICLES_DRAFT_REVISION FOREIGN KEY (DRAFT_REVISION_ID) REFERENCES ARTICLE_REVISIONS (ID) ON DELETE RESTRICT",
		"ADD CONSTRAINT FK_ARTICLES_PUBLISHED_REVISION FOREIGN KEY (PUBLISHED_REVISION_ID) REFERENCES ARTICLE_REVISIONS (ID) ON DELETE RESTRICT",
	}
	for _, foreignKey := range expected {
		require.Equal(t, 1, strings.Count(normalized, foreignKey), foreignKey)
	}

	expectedShapes := map[string][]string{
		"articles": {
			"DRAFT_REVISION_ID BIGINT NULL",
			"PUBLISHED_REVISION_ID BIGINT NULL",
		},
		"article_revisions": {
			"ARTICLE_ID BIGINT NOT NULL",
			"COVER_MEDIA_ID BIGINT NULL",
		},
		"article_revision_tags": {
			"REVISION_ID BIGINT NOT NULL",
			"TAG_ID BIGINT NOT NULL",
		},
		"article_revision_media": {
			"REVISION_ID BIGINT NOT NULL",
			"MEDIA_ID BIGINT NOT NULL",
		},
		"site_settings": {
			"SEO_DEFAULT_IMAGE_MEDIA_ID BIGINT NULL",
		},
	}
	for table, shapes := range expectedShapes {
		t.Run(table, func(t *testing.T) {
			definition := tableDefinition(t, normalized, table)
			for _, shape := range shapes {
				require.Contains(t, definition, shape)
			}
		})
	}

	require.Contains(t, normalized, "ALTER TABLE ARTICLES\n    ADD CONSTRAINT FK_ARTICLES_DRAFT_REVISION FOREIGN KEY (DRAFT_REVISION_ID) REFERENCES ARTICLE_REVISIONS (ID) ON DELETE RESTRICT,\n    ADD CONSTRAINT FK_ARTICLES_PUBLISHED_REVISION FOREIGN KEY (PUBLISHED_REVISION_ID) REFERENCES ARTICLE_REVISIONS (ID) ON DELETE RESTRICT;")
}

func TestDevelopSQLCreatesStageTwoTablesInDependencyOrder(t *testing.T) {
	normalized := readDevelopSQL(t)
	requireFragmentsInOrder(t, normalized, []string{
		"CREATE TABLE MEDIA (",
		"CREATE TABLE TAGS (",
		"CREATE TABLE ARTICLES (",
		"CREATE TABLE ARTICLE_REVISIONS (",
		"CREATE TABLE ARTICLE_REVISION_TAGS (",
		"CREATE TABLE ARTICLE_REVISION_MEDIA (",
		"CREATE TABLE SITE_SETTINGS (",
		"CREATE TABLE HOTLINK_SETTINGS (",
		"CREATE TABLE REFERER_ALLOWLIST (",
		"ALTER TABLE ARTICLES",
	})
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
	return strings.ToUpper(readDevelopSQLRaw(t))
}

func readDevelopSQLRaw(t *testing.T) string {
	t.Helper()
	sqlText, err := os.ReadFile(filepath.Join(sqlsDir(t), "develop", "develop.sql"))
	require.NoError(t, err)
	return string(sqlText)
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

func rawTableDefinition(t *testing.T, rawSQL, table string) string {
	t.Helper()
	startMarker := "CREATE TABLE " + table + " ("
	start := strings.Index(rawSQL, startMarker)
	require.NotEqual(t, -1, start, "missing table %s", table)

	remainder := rawSQL[start:]
	end := strings.Index(remainder, ") ENGINE=InnoDB")
	require.NotEqual(t, -1, end, "unterminated table %s", table)
	return remainder[:end]
}

func requireFragmentsInOrder(t *testing.T, text string, fragments []string) {
	t.Helper()
	remaining := text
	for _, fragment := range fragments {
		index := strings.Index(remaining, fragment)
		require.NotEqual(t, -1, index, "missing or out-of-order fragment %q", fragment)
		remaining = remaining[index+len(fragment):]
	}
}
