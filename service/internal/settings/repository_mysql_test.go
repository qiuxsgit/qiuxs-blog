package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/stretchr/testify/require"
)

const (
	testSiteColumns   = "id, site_name, author_name, author_bio, home_status, about_md, social_links_json, seo_default_title, seo_default_description, seo_default_image_media_id, filing_name, filing_number, lock_version, updated_at"
	testSelectSiteSQL = "SELECT " + testSiteColumns + " FROM site_settings WHERE singleton_key = 1"
	testInsertSiteSQL = "INSERT INTO site_settings (id, singleton_key, site_name, author_name, author_bio, home_status, about_md, social_links_json, seo_default_title, seo_default_description, seo_default_image_media_id, filing_name, filing_number, lock_version, created_at, updated_at) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)"
	testUpdateSiteSQL = "UPDATE site_settings SET site_name=?, author_name=?, author_bio=?, home_status=?, about_md=?, social_links_json=?, seo_default_title=?, seo_default_description=?, seo_default_image_media_id=?, filing_name=?, filing_number=?, lock_version=lock_version+1, updated_at=? WHERE singleton_key=1 AND lock_version=?"
)

func TestSiteMySQLGetUsesExactQueryAndStrictOrderedJSON(t *testing.T) {
	repository, mock, _ := newSettingsRepositoryTest(t, 1)
	want := storedSite()
	mock.ExpectQuery(testSelectSiteSQL).WillReturnRows(siteRows(want, `[{"label":"GitHub","url":"https://github.com/qiuxsgit"},{"label":"Site","url":"https://qiuxs.me/about?from=blog#contact"}]`))

	got, err := repository.GetSite(context.Background())
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.NotNil(t, got.SocialLinks)
	got.SocialLinks[0].Label = "mutated"
	require.Equal(t, "GitHub", want.SocialLinks[0].Label)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSiteMySQLGetMapsMissingAndSanitizesInvalidStoredJSON(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		repository, mock, _ := newSettingsRepositoryTest(t, 1)
		mock.ExpectQuery(testSelectSiteSQL).WillReturnError(sql.ErrNoRows)
		_, err := repository.GetSite(context.Background())
		require.ErrorIs(t, err, ErrNotFound)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	for _, raw := range []string{
		`[{"label":"GitHub","url":"https://github.com/qiuxsgit","secret":"database-value"}]`,
		`[{"label":"GitHub","url":"https://github.com/qiuxsgit"}] {}`,
		`null`,
		`[{"label":"GitHub","url":"http://invalid-secret.example"}]`,
	} {
		t.Run(raw, func(t *testing.T) {
			repository, mock, _ := newSettingsRepositoryTest(t, 1)
			mock.ExpectQuery(testSelectSiteSQL).WillReturnRows(siteRows(storedSite(), raw))
			_, err := repository.GetSite(context.Background())
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrInvalid)
			require.NotContains(t, err.Error(), "secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSiteMySQLGetAllowsLegacyBlankFilingForRepairAndPublishGate(t *testing.T) {
	repository, mock, _ := newSettingsRepositoryTest(t, 1)
	legacy := storedSite()
	legacy.FilingName = " \t"
	legacy.FilingNumber = "\n"
	mock.ExpectQuery(testSelectSiteSQL).WillReturnRows(siteRows(legacy, `[{"label":"GitHub","url":"https://github.com/qiuxsgit"},{"label":"Site","url":"https://qiuxs.me/about?from=blog#contact"}]`))

	got, err := repository.GetSite(context.Background())
	require.NoError(t, err)
	require.Equal(t, legacy, got)
	require.ErrorIs(t, ValidatePublishable(got), ErrInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSiteMySQLCreateUsesSharedIDAndExactCanonicalJSON(t *testing.T) {
	repository, mock, counter := newSettingsRepositoryTest(t, 4)
	at := time.Date(2026, 8, 14, 18, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	input := storedSite()
	input.ID, input.LockVersion, input.UpdatedAt = 999, 0, time.Time{}
	imageID := int64(31)
	input.SEODefaultImageMediaID = &imageID
	mock.ExpectExec(testInsertSiteSQL).WithArgs(
		int64(11), input.SiteName, input.AuthorName, input.AuthorBio, input.HomeStatus, input.AboutMD,
		`[{"label":"GitHub","url":"https://github.com/qiuxsgit"},{"label":"Site","url":"https://qiuxs.me/about?from=blog#contact"}]`,
		input.SEODefaultTitle, input.SEODefaultDescription, int64(31), input.FilingName, input.FilingNumber, at.UTC(), at.UTC(),
	).WillReturnResult(sqlmock.NewResult(999, 1))

	got, err := repository.CreateSite(context.Background(), input, at)
	require.NoError(t, err)
	require.Equal(t, int64(11), got.ID)
	require.Equal(t, int64(1), got.LockVersion)
	require.Equal(t, at.UTC(), got.UpdatedAt)
	require.Equal(t, []string{"idseq:site_settings"}, counter.keys)
	input.SocialLinks[0].Label = "mutated"
	require.Equal(t, "GitHub", got.SocialLinks[0].Label)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSiteMySQLCreateMapsOnlyNamedSingletonConflict(t *testing.T) {
	for _, test := range []struct {
		name         string
		key          string
		wantConflict bool
	}{
		{name: "singleton", key: "uk_site_settings_singleton", wantConflict: true},
		{name: "qualified singleton", key: "site_settings.uk_site_settings_singleton", wantConflict: true},
		{name: "substring", key: "uk_site_settings_singleton_backup"},
		{name: "unrelated", key: "uk_site_settings_other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newSettingsRepositoryTest(t, 1)
			mysqlErr := &mysql.MySQLError{Number: 1062, Message: fmt.Sprintf("Duplicate entry 'singleton-secret' for key '%s'", test.key)}
			mock.ExpectExec(testInsertSiteSQL).WillReturnError(mysqlErr)
			_, err := repository.CreateSite(context.Background(), createSiteInput(), time.Now())
			require.Error(t, err)
			require.Equal(t, test.wantConflict, errors.Is(err, ErrConflict))
			require.ErrorIs(t, err, mysqlErr)
			require.NotContains(t, err.Error(), "singleton-secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSiteMySQLUpdateIsConditionalAndReloadsPersistentIdentityInOneTransaction(t *testing.T) {
	repository, mock, _ := newSettingsRepositoryTest(t, 1)
	at := time.Date(2026, 8, 14, 18, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	input := createSiteInput()
	want := storedSite()
	want.LockVersion = 3
	want.UpdatedAt = at.UTC()
	mock.ExpectBegin()
	mock.ExpectExec(testUpdateSiteSQL).WithArgs(
		input.SiteName, input.AuthorName, input.AuthorBio, input.HomeStatus, input.AboutMD,
		`[{"label":"GitHub","url":"https://github.com/qiuxsgit"},{"label":"Site","url":"https://qiuxs.me/about?from=blog#contact"}]`,
		input.SEODefaultTitle, input.SEODefaultDescription, nil, input.FilingName, input.FilingNumber, at.UTC(), int64(2),
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(testSelectSiteSQL).WillReturnRows(siteRows(want, `[{"label":"GitHub","url":"https://github.com/qiuxsgit"},{"label":"Site","url":"https://qiuxs.me/about?from=blog#contact"}]`))
	mock.ExpectCommit()

	got, err := repository.UpdateSite(context.Background(), input, 2, at)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSiteMySQLUpdateZeroRowsReturnsConflictAndRollsBack(t *testing.T) {
	repository, mock, _ := newSettingsRepositoryTest(t, 1)
	mock.ExpectBegin()
	mock.ExpectExec(testUpdateSiteSQL).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	_, err := repository.UpdateSite(context.Background(), createSiteInput(), 2, time.Now())
	require.ErrorIs(t, err, ErrConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSiteMySQLSanitizesDependencyFailuresAndRollsBackUpdate(t *testing.T) {
	tests := []struct {
		name  string
		setup func(sqlmock.Sqlmock)
		call  func(*MySQLRepository) error
	}{
		{name: "get query", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(testSelectSiteSQL).WillReturnError(errors.New("get-secret"))
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.GetSite(context.Background())
			return err
		}},
		{name: "create insert", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectExec(testInsertSiteSQL).WillReturnError(errors.New("insert-secret"))
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.CreateSite(context.Background(), createSiteInput(), time.Now())
			return err
		}},
		{name: "update begin", setup: func(mock sqlmock.Sqlmock) { mock.ExpectBegin().WillReturnError(errors.New("begin-secret")) }, call: func(repository *MySQLRepository) error {
			_, err := repository.UpdateSite(context.Background(), createSiteInput(), 2, time.Now())
			return err
		}},
		{name: "update exec", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(testUpdateSiteSQL).WillReturnError(errors.New("update-secret"))
			mock.ExpectRollback()
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.UpdateSite(context.Background(), createSiteInput(), 2, time.Now())
			return err
		}},
		{name: "update affected", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(testUpdateSiteSQL).WillReturnResult(sqlmock.NewErrorResult(errors.New("rows-secret")))
			mock.ExpectRollback()
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.UpdateSite(context.Background(), createSiteInput(), 2, time.Now())
			return err
		}},
		{name: "reload query", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(testUpdateSiteSQL).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(testSelectSiteSQL).WillReturnError(errors.New("reload-secret"))
			mock.ExpectRollback()
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.UpdateSite(context.Background(), createSiteInput(), 2, time.Now())
			return err
		}},
		{name: "reload invalid JSON", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(testUpdateSiteSQL).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(testSelectSiteSQL).WillReturnRows(siteRows(storedSite(), `{"invalid":"secret"}`))
			mock.ExpectRollback()
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.UpdateSite(context.Background(), createSiteInput(), 2, time.Now())
			return err
		}},
		{name: "commit", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(testUpdateSiteSQL).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(testSelectSiteSQL).WillReturnRows(siteRows(storedSite(), `[{"label":"GitHub","url":"https://github.com/qiuxsgit"},{"label":"Site","url":"https://qiuxs.me/about?from=blog#contact"}]`))
			mock.ExpectCommit().WillReturnError(errors.New("commit-secret"))
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.UpdateSite(context.Background(), createSiteInput(), 2, time.Now())
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newSettingsRepositoryTest(t, 1)
			test.setup(mock)
			err := test.call(repository)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSiteMySQLRejectsNilConfigurationContextAndInvalidWritesWithoutPanic(t *testing.T) {
	valid, mock, _ := newSettingsRepositoryTest(t, 1)
	var nilRepository *MySQLRepository
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, test := range []struct {
		name string
		call func() error
	}{
		{name: "nil receiver", call: func() error { _, err := nilRepository.GetSite(context.Background()); return err }},
		{name: "nil database", call: func() error { _, err := NewMySQLRepository(nil, valid.ids).GetSite(context.Background()); return err }},
		{name: "nil generator", call: func() error { _, err := NewMySQLRepository(db, nil).GetSite(context.Background()); return err }},
		{name: "zero generator", call: func() error {
			_, err := NewMySQLRepository(db, &idgen.Generator{}).CreateSite(context.Background(), createSiteInput(), time.Now())
			return err
		}},
		{name: "nil get context", call: func() error { _, err := valid.GetSite(nil); return err }},
		{name: "nil create context", call: func() error { _, err := valid.CreateSite(nil, createSiteInput(), time.Now()); return err }},
		{name: "nil update context", call: func() error { _, err := valid.UpdateSite(nil, createSiteInput(), 1, time.Now()); return err }},
		{name: "invalid create", call: func() error { _, err := valid.CreateSite(context.Background(), Site{}, time.Now()); return err }},
		{name: "invalid update lock", call: func() error {
			_, err := valid.UpdateSite(context.Background(), createSiteInput(), 0, time.Now())
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var callErr error
			require.NotPanics(t, func() { callErr = test.call() })
			require.Error(t, callErr)
		})
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

type settingsCounter struct {
	raw  int64
	keys []string
}

func (c *settingsCounter) Increment(_ context.Context, key string) (int64, error) {
	c.keys = append(c.keys, key)
	return c.raw, nil
}
func (*settingsCounter) Raise(context.Context, string, int64) (int64, error) {
	return 0, errors.New("unexpected counter raise")
}

func newSettingsRepositoryTest(t *testing.T, raw int64) (*MySQLRepository, sqlmock.Sqlmock, *settingsCounter) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	counter := &settingsCounter{raw: raw}
	ids, err := idgen.New(counter, nil, 2, 3, false)
	require.NoError(t, err)
	return NewMySQLRepository(db, ids), mock, counter
}

func createSiteInput() Site {
	site := storedSite()
	site.ID, site.LockVersion, site.UpdatedAt = 0, 0, time.Time{}
	site.SEODefaultImageMediaID = nil
	return site
}

func storedSite() Site {
	imageID := int64(31)
	return Site{
		ID: 11, LockVersion: 2, SiteName: "qiuxs blog", AuthorName: "qiuxs", AuthorBio: "Bio", HomeStatus: "Writing",
		AboutMD: "# About\n", SocialLinks: []SocialLink{{Label: "GitHub", URL: "https://github.com/qiuxsgit"}, {Label: "Site", URL: "https://qiuxs.me/about?from=blog#contact"}},
		SEODefaultTitle: "qiuxs blog", SEODefaultDescription: "Personal notes", SEODefaultImageMediaID: &imageID,
		FilingName: "长安休息室", FilingNumber: "浙ICP备17057726号-1", UpdatedAt: time.Date(2026, 8, 14, 10, 0, 0, 123000, time.UTC),
	}
}

func siteRows(site Site, socialsJSON string) *sqlmock.Rows {
	var imageID any
	if site.SEODefaultImageMediaID != nil {
		imageID = *site.SEODefaultImageMediaID
	}
	return sqlmock.NewRows([]string{"id", "site_name", "author_name", "author_bio", "home_status", "about_md", "social_links_json", "seo_default_title", "seo_default_description", "seo_default_image_media_id", "filing_name", "filing_number", "lock_version", "updated_at"}).
		AddRow(site.ID, site.SiteName, site.AuthorName, site.AuthorBio, site.HomeStatus, site.AboutMD, socialsJSON, site.SEODefaultTitle, site.SEODefaultDescription, imageID, site.FilingName, site.FilingNumber, site.LockVersion, site.UpdatedAt)
}
