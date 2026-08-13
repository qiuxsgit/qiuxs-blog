package sqls_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
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

var releaseTables = []string{
	"releases",
	"release_articles",
	"publish_jobs",
	"site_state",
	"builder_config",
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

func TestDevelopSQLDefinesReleaseStateWithoutUnsignedOrAutoIncrement(t *testing.T) {
	normalized := readDevelopSQL(t)
	for _, table := range releaseTables {
		t.Run(table, func(t *testing.T) {
			definition := tableDefinition(t, normalized, table)
			require.Contains(t, definition, "ID BIGINT NOT NULL")
			require.Contains(t, definition, "PRIMARY KEY (ID)")
		})
	}
	require.NotContains(t, normalized, "AUTO_INCREMENT")
	require.NotContains(t, normalized, "UNSIGNED")
}

func TestDevelopSQLReleaseRowsContainCompleteImmutableSnapshots(t *testing.T) {
	normalized := readDevelopSQL(t)
	expected := map[string][]string{
		"releases": {
			"SITE_SNAPSHOT_JSON JSON NOT NULL", "CHECKSUM CHAR(71)", "STATUS VARCHAR(16)",
			"CREATED_AT DATETIME(6) NOT NULL", "COMPLETED_AT DATETIME(6) NULL",
			"CONSTRAINT CHK_RELEASES_CHECKSUM", "CONSTRAINT CHK_RELEASES_STATUS",
		},
		"release_articles": {
			"RELEASE_ID BIGINT NOT NULL", "ARTICLE_ID BIGINT NOT NULL", "REVISION_ID BIGINT NOT NULL",
			"SLUG VARCHAR(12)", "TITLE VARCHAR(200) NOT NULL", "SUMMARY VARCHAR(600) NOT NULL",
			"CONTENT_MD LONGTEXT NOT NULL", "CONTENT_HASH CHAR(71)", "PUBLISHED_AT DATETIME(6) NOT NULL",
			"TAGS_SNAPSHOT_JSON JSON NOT NULL", "UNIQUE KEY UK_RELEASE_ARTICLES_ARTICLE (RELEASE_ID, ARTICLE_ID)",
		},
		"publish_jobs": {
			"RELEASE_ID BIGINT NOT NULL", "BUILDER_ID BIGINT NOT NULL", "STATUS VARCHAR(16)", "STAGE VARCHAR(64)",
			"BUILD_NUMBER BIGINT NULL", "ERROR_SUMMARY VARCHAR(512) NOT NULL", "CREATED_AT DATETIME(6) NOT NULL",
			"UPDATED_AT DATETIME(6) NOT NULL", "FINISHED_AT DATETIME(6) NULL", "CONSTRAINT CHK_PUBLISH_JOBS_STATUS",
		},
		"site_state": {
			"SINGLETON_KEY TINYINT NOT NULL DEFAULT 1", "CURRENT_RELEASE_ID BIGINT NULL", "ACTIVE_PUBLISH_JOB_ID BIGINT NULL",
			"UNIQUE KEY UK_SITE_STATE_SINGLETON (SINGLETON_KEY)", "CONSTRAINT CHK_SITE_STATE_SINGLETON CHECK (SINGLETON_KEY = 1)",
		},
		"builder_config": {
			"SINGLETON_KEY TINYINT NOT NULL DEFAULT 1", "NAME VARCHAR(100) NOT NULL", "BASE_URL VARCHAR(2048)",
			"USERNAME VARCHAR(255) NOT NULL", "TOKEN_CIPHERTEXT TEXT", "JOB_NAME VARCHAR(128)", "ENABLED BOOLEAN NOT NULL",
			"UNIQUE KEY UK_BUILDER_CONFIG_SINGLETON (SINGLETON_KEY)", "CONSTRAINT CHK_BUILDER_CONFIG_SINGLETON CHECK (SINGLETON_KEY = 1)",
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

	builder := tableDefinition(t, normalized, "builder_config")
	require.NotContains(t, builder, "TOKEN VARCHAR")
	require.NotContains(t, builder, "PLAINTEXT")
	require.Contains(t, builder, "CONSTRAINT CHK_BUILDER_CONFIG_BASE_URL CHECK (REGEXP_LIKE(BASE_URL, '^HTTPS://[A-Z0-9.-]+(:[1-9][0-9]{0,4})?$', 'C'))")
	require.Contains(t, builder, "CONSTRAINT CHK_BUILDER_CONFIG_JOB_NAME CHECK (REGEXP_LIKE(JOB_NAME, '^[A-ZA-Z0-9][A-ZA-Z0-9._/-]{0,127}$', 'C') AND JOB_NAME NOT LIKE '%//%')")
}

func TestDevelopSQLReleaseDomainsAndForeignKeysAreExactAndCaseSensitive(t *testing.T) {
	raw := readDevelopSQLRaw(t)
	for table, fragments := range map[string][]string{
		"releases": {
			"checksum CHAR(71) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
			"status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'queued'",
			"CONSTRAINT chk_releases_checksum CHECK (REGEXP_LIKE(checksum, '^sha256:[a-f0-9]{64}$', 'c'))",
			"CONSTRAINT chk_releases_status CHECK (status IN ('queued', 'success', 'failed'))",
		},
		"release_articles": {
			"slug VARCHAR(12) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
			"content_hash CHAR(71) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
			"CONSTRAINT chk_release_articles_slug CHECK (REGEXP_LIKE(slug, '^[a-z0-9_-]{12}$', 'c'))",
			"CONSTRAINT chk_release_articles_hash CHECK (REGEXP_LIKE(content_hash, '^sha256:[a-f0-9]{64}$', 'c'))",
		},
		"publish_jobs": {
			"status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'pending'",
			"stage VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'pending'",
			"CONSTRAINT chk_publish_jobs_status CHECK (status IN ('pending', 'queued', 'building', 'deploying', 'success', 'failed'))",
		},
		"builder_config": {
			"base_url VARCHAR(2048) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
			"token_ciphertext TEXT CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
			"job_name VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL",
		},
	} {
		t.Run(table, func(t *testing.T) {
			definition := rawTableDefinition(t, raw, table)
			for _, fragment := range fragments {
				require.Contains(t, definition, fragment)
			}
		})
	}

	normalized := strings.ToUpper(raw)
	foreignKeys := []string{
		"CONSTRAINT FK_RELEASE_ARTICLES_RELEASE FOREIGN KEY (RELEASE_ID) REFERENCES RELEASES (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_RELEASE_ARTICLES_ARTICLE FOREIGN KEY (ARTICLE_ID) REFERENCES ARTICLES (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_RELEASE_ARTICLES_REVISION FOREIGN KEY (REVISION_ID) REFERENCES ARTICLE_REVISIONS (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_PUBLISH_JOBS_RELEASE FOREIGN KEY (RELEASE_ID) REFERENCES RELEASES (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_PUBLISH_JOBS_BUILDER FOREIGN KEY (BUILDER_ID) REFERENCES BUILDER_CONFIG (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_SITE_STATE_CURRENT_RELEASE FOREIGN KEY (CURRENT_RELEASE_ID) REFERENCES RELEASES (ID) ON DELETE RESTRICT",
		"CONSTRAINT FK_SITE_STATE_ACTIVE_JOB FOREIGN KEY (ACTIVE_PUBLISH_JOB_ID) REFERENCES PUBLISH_JOBS (ID) ON DELETE RESTRICT",
	}
	for _, foreignKey := range foreignKeys {
		require.Equal(t, 1, strings.Count(normalized, foreignKey), foreignKey)
	}
	requireFragmentsInOrder(t, normalized, []string{
		"CREATE TABLE RELEASES (", "CREATE TABLE RELEASE_ARTICLES (", "CREATE TABLE BUILDER_CONFIG (",
		"CREATE TABLE PUBLISH_JOBS (", "CREATE TABLE SITE_STATE (",
	})
}

func TestDevelopSQLBuilderCiphertextUsesUnpaddedRawStandardBase64(t *testing.T) {
	raw := readDevelopSQLRaw(t)
	definition := rawTableDefinition(t, raw, "builder_config")
	const storage = "token_ciphertext TEXT CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'RawStd base64 of nonce || ciphertext-and-tag'"
	require.Contains(t, definition, storage)
	for _, constraint := range []string{
		"CHAR_LENGTH(token_ciphertext) >= 39",
		"MOD(CHAR_LENGTH(token_ciphertext), 4) <> 1",
		"REGEXP_LIKE(token_ciphertext, '^[A-Za-z0-9+/]+$', 'c')",
		"REGEXP_LIKE(token_ciphertext, '[AQgw]$', 'c')",
		"REGEXP_LIKE(token_ciphertext, '[AEIMQUYcgkosw048]$', 'c')",
	} {
		require.Contains(t, definition, constraint)
	}
	require.NotContains(t, definition, "A-Za-z0-9_-")
	require.NotContains(t, definition, "token_nonce")

	alphabet := regexp.MustCompile(`^[A-Za-z0-9+/]+$`)
	require.True(t, alphabet.MatchString("Ab09+/"), "RawStdEncoding permits + and /")
	for _, nonCanonical := range []string{"Ab09-_", "Ab09=="} {
		require.False(t, alphabet.MatchString(nonCanonical), nonCanonical)
	}
	for _, invalid := range []string{"A", "AAAA", strings.Repeat("A", 38) + "B"} {
		require.False(t, validRawStdCiphertext(invalid), invalid)
	}
	canonical := base64.RawStdEncoding.EncodeToString(append([]byte(strings.Repeat("\xfb\xff\xff", 9)), 0xfb, 0xff))
	require.Len(t, canonical, 39, "minimum nonce, tag, and one-byte token payload")
	require.Contains(t, canonical, "+")
	require.Contains(t, canonical, "/")
	require.True(t, validRawStdCiphertext(canonical))
}

func validRawStdCiphertext(encoded string) bool {
	decoded, err := base64.RawStdEncoding.Strict().DecodeString(encoded)
	return err == nil && len(decoded) >= 29 && base64.RawStdEncoding.EncodeToString(decoded) == encoded
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
