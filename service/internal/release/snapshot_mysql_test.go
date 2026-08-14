package release

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/stretchr/testify/require"
)

func TestMySQLSnapshotSourceFreezesSelectedDraftInsideCallerTransaction(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ids, err := idgen.New(&snapshotCounter{values: make(map[string]int64)}, nil, 1, 1, false)
	require.NoError(t, err)
	now := time.Date(2026, 8, 14, 13, 0, 0, 123456000, time.UTC)
	source := NewMySQLSnapshotSource(ids, func() time.Time { return now })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	mock.ExpectQuery(snapshotDraftSQL).WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{
		"article_id", "slug", "revision_id", "revision_no", "title", "summary", "cover_media_id", "content_md", "content_hash", "lock_version",
	}).AddRow(41, "article_slug", 71, 3, "Title", "Summary", nil, "Body", strings.Repeat("b", 64), 4))
	mock.ExpectQuery(snapshotTagsSQL).WithArgs(int64(71)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).
		AddRow(9, "Zed", "t_zzzzzzzzzzzz", 0).
		AddRow(5, "Alpha", "t_aaaaaaaaaaaa", 1))
	mock.ExpectQuery(snapshotMediaSQL).WithArgs(int64(71)).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}))
	mock.ExpectExec(freezeSnapshotSQL).WithArgs(now, int64(71), int64(4)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertSnapshotDraftSQL).WithArgs(int64(1), int64(41), int64(4), "Title", "Summary", nil, "Body", strings.Repeat("b", 64), now, now).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertSnapshotTagSQL).WithArgs(int64(1), int64(1), int64(9), "Zed", "t_zzzzzzzzzzzz", 0, now).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertSnapshotTagSQL).WithArgs(int64(2), int64(1), int64(5), "Alpha", "t_aaaaaaaaaaaa", 1, now).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(replaceSnapshotDraftSQL).WithArgs(int64(1), now, int64(41), int64(71)).WillReturnResult(sqlmock.NewResult(0, 1))

	prepared, err := source.PrepareSnapshot(context.Background(), tx, SnapshotRequest{
		Mode: PublishArticle, ArticleID: 41, CurrentReleaseID: 7,
		Base: PreparedSnapshot{Site: validSnapshotSite(), Articles: []ArticleSnapshot{}},
	})
	require.NoError(t, err)
	require.Len(t, prepared.Articles, 1)
	require.Equal(t, "sha256:"+strings.Repeat("b", 64), prepared.Articles[0].ContentHash)
	require.Equal(t, []TagSnapshot{
		{ID: 5, Name: "Alpha", Slug: "t_aaaaaaaaaaaa"},
		{ID: 9, Name: "Zed", Slug: "t_zzzzzzzzzzzz"},
	}, prepared.Articles[0].Tags)
	require.NoError(t, verifyPreparedSnapshotChecksum(prepared))
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLSnapshotSourceUsesVirtualSiteDefaultsAndRejectsInvalidStoredSocialJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		rows *sqlmock.Rows
		ok   bool
	}{
		{name: "virtual defaults", rows: sqlmock.NewRows([]string{"site_name", "author_bio", "about_md", "social_links_json", "filing_name", "filing_number"}), ok: true},
		{name: "duplicate stored key", rows: sqlmock.NewRows([]string{"site_name", "author_bio", "about_md", "social_links_json", "filing_name", "filing_number"}).AddRow("Blog", "Bio", "About", `[{"label":"GitHub","label":"GitLab","url":"https://example.com"}]`, "ICP", "ICP-1")},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			require.NoError(t, err)
			defer db.Close()
			ids, err := idgen.New(&snapshotCounter{values: make(map[string]int64)}, nil, 1, 1, false)
			require.NoError(t, err)
			source := NewMySQLSnapshotSource(ids, time.Now)
			mock.ExpectBegin()
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			mock.ExpectQuery(snapshotSiteSQL).WillReturnRows(test.rows)
			prepared, prepareErr := source.PrepareSnapshot(context.Background(), tx, SnapshotRequest{Mode: PublishSettings, Base: PreparedSnapshot{Articles: []ArticleSnapshot{}}})
			if test.ok {
				require.NoError(t, prepareErr)
				require.NotNil(t, prepared.Site.SocialLinks)
				require.NoError(t, verifyPreparedSnapshotChecksum(prepared))
			} else {
				require.Error(t, prepareErr)
			}
			mock.ExpectRollback()
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func validSnapshotSite() SiteSnapshot {
	return SiteSnapshot{Name: "Blog", FilingName: "ICP", FilingNumber: "ICP-1", SocialLinks: []SocialLink{}}
}

type snapshotCounter struct{ values map[string]int64 }

func (c *snapshotCounter) Increment(_ context.Context, key string) (int64, error) {
	c.values[key]++
	return c.values[key], nil
}

func (*snapshotCounter) Raise(context.Context, string, int64) (int64, error) {
	return 0, errors.New("unexpected raise")
}

func TestMySQLSnapshotSourceRejectsCorruptOwnershipAndMediaBeforeWrites(t *testing.T) {
	require.Contains(t, snapshotDraftSQL, "r.article_id = a.id")
	require.Contains(t, snapshotMediaSQL, "m.state = 'active'")

	t.Run("cross article draft pointer", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		defer db.Close()
		counter := &snapshotCounter{values: make(map[string]int64)}
		ids, err := idgen.New(counter, nil, 1, 1, false)
		require.NoError(t, err)
		source := NewMySQLSnapshotSource(ids, time.Now)
		mock.ExpectBegin()
		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		mock.ExpectQuery(snapshotDraftSQL).WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{
			"article_id", "slug", "revision_id", "revision_no", "title", "summary", "cover_media_id", "content_md", "content_hash", "lock_version",
		}))
		_, err = source.PrepareSnapshot(context.Background(), tx, SnapshotRequest{
			Mode: PublishArticle, ArticleID: 41, CurrentReleaseID: 7,
			Base: PreparedSnapshot{Site: validSnapshotSite(), Articles: []ArticleSnapshot{}},
		})
		require.Error(t, err)
		require.Empty(t, counter.values, "corrupt pointer must fail before ID allocation")
		mock.ExpectRollback()
		require.NoError(t, tx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("markdown reference differs from active association", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		defer db.Close()
		counter := &snapshotCounter{values: make(map[string]int64)}
		ids, err := idgen.New(counter, nil, 1, 1, false)
		require.NoError(t, err)
		source := NewMySQLSnapshotSource(ids, time.Now)
		mock.ExpectBegin()
		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		markdownKey := "m_aaaaaaaaaaaaaaaaaaaaaa"
		storedKey := "m_bbbbbbbbbbbbbbbbbbbbbb"
		mock.ExpectQuery(snapshotDraftSQL).WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{
			"article_id", "slug", "revision_id", "revision_no", "title", "summary", "cover_media_id", "content_md", "content_hash", "lock_version",
		}).AddRow(41, "article_slug", 71, 3, "Title", "Summary", nil, "![image](/img/proxy/"+markdownKey+")", strings.Repeat("b", 64), 4))
		mock.ExpectQuery(snapshotTagsSQL).WithArgs(int64(71)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}))
		mock.ExpectQuery(snapshotMediaSQL).WithArgs(int64(71)).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}).AddRow(91, storedKey, "content", 0))
		_, err = source.PrepareSnapshot(context.Background(), tx, SnapshotRequest{
			Mode: PublishArticle, ArticleID: 41, CurrentReleaseID: 7,
			Base: PreparedSnapshot{Site: validSnapshotSite(), Articles: []ArticleSnapshot{}},
		})
		require.EqualError(t, err, "stored publish draft associations are invalid")
		require.Empty(t, counter.values, "mismatched media must fail before ID allocation")
		mock.ExpectRollback()
		require.NoError(t, tx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

var _ SnapshotExecutor = (*sql.Tx)(nil)
