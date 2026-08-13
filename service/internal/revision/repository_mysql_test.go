package revision

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/stretchr/testify/require"
)

const (
	selectEditingDraftSQL  = `SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE article_id = ? AND status = 'editing'`
	selectDraftTagsSQL     = `SELECT tag_id, tag_name, tag_slug, position FROM article_revision_tags WHERE revision_id = ? ORDER BY position ASC`
	selectDraftMediaSQL    = `SELECT arm.media_id, m.public_key, arm.purpose, arm.position FROM article_revision_media arm JOIN media m ON m.id = arm.media_id WHERE arm.revision_id = ? ORDER BY arm.position ASC`
	updateEditingDraftSQL  = `UPDATE article_revisions SET title = ?, summary = ?, cover_media_id = ?, content_md = ?, content_hash = ?, lock_version = lock_version + 1, updated_at = ? WHERE article_id = ? AND status = 'editing' AND lock_version = ?`
	selectSavedIdentitySQL = `SELECT id, lock_version, revision_no, created_at FROM article_revisions WHERE article_id = ? AND status = 'editing'`
	deleteDraftTagsSQL     = `DELETE FROM article_revision_tags WHERE revision_id = ?`
	insertDraftTagSQL      = `INSERT INTO article_revision_tags (id, revision_id, tag_id, tag_name, tag_slug, position, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	deleteDraftMediaSQL    = `DELETE FROM article_revision_media WHERE revision_id = ?`
	insertDraftMediaSQL    = `INSERT INTO article_revision_media (id, revision_id, media_id, purpose, position, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	touchActiveArticleSQL  = `UPDATE articles SET updated_at = ? WHERE id = ? AND state = 'active'`
	testContentHash        = `5b732fcfb7289a73704164ad25aaae5be4b188172d1a47932428a8d1cdc7d2dc`
)

func TestMySQLRepositoryGetDraftLoadsEditingScalarAndOrderedAssociations(t *testing.T) {
	repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
	createdAt := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	mock.ExpectQuery(selectEditingDraftSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().AddRow(
		int64(21), int64(11), int64(2), "editing", "draft", "Title", "Summary", int64(91), "body", testContentHash, int64(3), createdAt, updatedAt,
	))
	mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(
		sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).
			AddRow(int64(7), "Go", "t_go", 0).
			AddRow(int64(3), "Web", "t_web", 1),
	)
	mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnRows(
		sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}).
			AddRow(int64(91), firstMediaKey, "cover", 0).
			AddRow(int64(92), secondMediaKey, "content", 1),
	)

	got, err := repository.GetDraft(context.Background(), 11)

	require.NoError(t, err)
	require.Equal(t, Draft{
		ID: 21, ArticleID: 11, RevisionNo: 2, LockVersion: 3,
		Status: StatusEditing, Reason: ReasonDraft,
		Title: "Title", Summary: "Summary", CoverMediaID: revisionInt64Pointer(91), ContentMD: "body", ContentHash: testContentHash,
		Tags: []tag.Snapshot{
			{TagID: 7, Name: "Go", Slug: "t_go", Position: 0},
			{TagID: 3, Name: "Web", Slug: "t_web", Position: 1},
		},
		Media: []media.Reference{
			{MediaID: 91, PublicKey: firstMediaKey, Purpose: "cover", Position: 0},
			{MediaID: 92, PublicKey: secondMediaKey, Purpose: "content", Position: 1},
		},
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryGetDraftReturnsNonNilEmptyAssociationsAndNullCover(t *testing.T) {
	repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
	at := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(selectEditingDraftSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().AddRow(
		int64(21), int64(11), int64(1), "editing", "draft", "", "", nil, "", ComputeHash(PreparedContent{}), int64(1), at, at,
	))
	mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}))
	mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}))

	got, err := repository.GetDraft(context.Background(), 11)

	require.NoError(t, err)
	require.Nil(t, got.CoverMediaID)
	require.NotNil(t, got.Tags)
	require.Empty(t, got.Tags)
	require.NotNil(t, got.Media)
	require.Empty(t, got.Media)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryGetDraftMapsMissingAndSanitizesAssociationFailures(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
		mock.ExpectQuery(selectEditingDraftSQL).WithArgs(int64(11)).WillReturnError(sql.ErrNoRows)

		_, err := repository.GetDraft(context.Background(), 11)

		require.ErrorIs(t, err, ErrNotFound)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	for _, test := range []struct {
		name  string
		setup func(sqlmock.Sqlmock)
	}{
		{name: "scalar scan", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(selectEditingDraftSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().AddRow(
				"id-secret", int64(11), int64(1), "editing", "draft", "", "", nil, "", ComputeHash(PreparedContent{}), int64(1), time.Now(), time.Now(),
			))
		}},
		{name: "tag query", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnError(errors.New("tag-query-secret"))
		}},
		{name: "tag scan", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(
				sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).AddRow("tag-id-secret", "Go", "t_go", 0),
			)
		}},
		{name: "tag rows", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(
				sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).
					AddRow(int64(7), "Go", "t_go", 0).
					RowError(0, errors.New("tag-rows-secret")),
			)
		}},
		{name: "media query", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			expectNoStoredTags(mock)
			mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnError(errors.New("media-query-secret"))
		}},
		{name: "media scan", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			expectNoStoredTags(mock)
			mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnRows(
				sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}).AddRow("media-id-secret", firstMediaKey, "content", 0),
			)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
			test.setup(mock)

			_, err := repository.GetDraft(context.Background(), 11)

			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositorySaveDraftUsesOneTransactionAndSharedAssociationIDs(t *testing.T) {
	repository, mock, counter := newRevisionRepositoryTest(t, 2, 3)
	at := time.Date(2026, 8, 14, 9, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	createdAt := at.UTC().Add(-time.Hour)
	content := preparedRepositoryContent()
	mock.ExpectBegin()
	expectDraftUpdate(mock, content, at.UTC(), sqlmock.NewResult(0, 1))
	mock.ExpectQuery(selectSavedIdentitySQL).WithArgs(int64(11)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(21), int64(4), int64(2), createdAt),
	)
	mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(insertDraftTagSQL).
		WithArgs(int64(2), int64(21), int64(7), "Go", "t_go", 0, at.UTC()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftTagSQL).
		WithArgs(int64(5), int64(21), int64(3), "Web", "t_web", 1, at.UTC()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(2), int64(21), int64(91), "cover", 0, at.UTC()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(5), int64(21), int64(92), "content", 1, at.UTC()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(8), int64(21), int64(93), "content", 2, at.UTC()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(touchActiveArticleSQL).WithArgs(at.UTC(), int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

	require.NoError(t, err)
	require.Equal(t, Draft{
		ID: 21, ArticleID: 11, RevisionNo: 2, LockVersion: 4,
		Status: StatusEditing, Reason: ReasonDraft,
		Title: content.Title, Summary: content.Summary, CoverMediaID: revisionInt64Pointer(91), ContentMD: content.ContentMD, ContentHash: content.ContentHash,
		Tags: content.Tags, Media: content.Media, CreatedAt: createdAt, UpdatedAt: at.UTC(),
	}, got)
	require.Equal(t, []string{
		"idseq:article_revision_tags", "idseq:article_revision_tags",
		"idseq:article_revision_media", "idseq:article_revision_media", "idseq:article_revision_media",
	}, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositorySaveDraftSupportsNullCoverAndEmptyAssociations(t *testing.T) {
	repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	createdAt := at.Add(-time.Hour)
	content := PreparedContent{Title: "Title", ContentMD: "body", Tags: []tag.Snapshot{}, Media: []media.Reference{}}
	content.ContentHash = ComputeHash(content)
	mock.ExpectBegin()
	mock.ExpectExec(updateEditingDraftSQL).
		WithArgs("Title", "", nil, "body", content.ContentHash, at, int64(11), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(selectSavedIdentitySQL).WithArgs(int64(11)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(21), int64(2), int64(1), createdAt),
	)
	mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := repository.SaveDraft(context.Background(), 11, 1, content, at)

	require.NoError(t, err)
	require.Nil(t, got.CoverMediaID)
	require.NotNil(t, got.Tags)
	require.NotNil(t, got.Media)
	require.Empty(t, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositorySaveDraftConflictStopsBeforeAssociationMutation(t *testing.T) {
	repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	content := preparedRepositoryContent()
	mock.ExpectBegin()
	expectDraftUpdate(mock, content, at, sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	_, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositorySaveDraftRollsBackSQLAndAllocationFailures(t *testing.T) {
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	createdAt := at.Add(-time.Hour)
	content := preparedRepositoryContent()
	tests := []struct {
		name   string
		failAt map[string]map[int]error
		setup  func(sqlmock.Sqlmock)
	}{
		{name: "update", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(updateEditingDraftSQL).
				WithArgs(content.Title, content.Summary, int64(91), content.ContentMD, content.ContentHash, at, int64(11), int64(3)).
				WillReturnError(errors.New("update-secret"))
			mock.ExpectRollback()
		}},
		{name: "update rows", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectDraftUpdate(mock, content, at, sqlmock.NewErrorResult(errors.New("rows-secret")))
			mock.ExpectRollback()
		}},
		{name: "identity query", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectDraftUpdate(mock, content, at, sqlmock.NewResult(0, 1))
			mock.ExpectQuery(selectSavedIdentitySQL).WithArgs(int64(11)).WillReturnError(errors.New("identity-secret"))
			mock.ExpectRollback()
		}},
		{name: "identity lock mismatch", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectDraftUpdate(mock, content, at, sqlmock.NewResult(0, 1))
			mock.ExpectQuery(selectSavedIdentitySQL).WithArgs(int64(11)).WillReturnRows(
				sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(21), int64(9), int64(2), createdAt),
			)
			mock.ExpectRollback()
		}},
		{name: "delete tags", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughIdentity(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnError(errors.New("delete-tags-secret"))
			mock.ExpectRollback()
		}},
		{name: "tag allocation", failAt: map[string]map[int]error{"idseq:article_revision_tags": {1: errors.New("redis-tag-secret")}}, setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughIdentity(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 2))
			mock.ExpectRollback()
		}},
		{name: "tag insert", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughIdentity(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 2))
			mock.ExpectExec(insertDraftTagSQL).
				WithArgs(int64(1), int64(21), int64(7), "Go", "t_go", 0, at).
				WillReturnError(errors.New("insert-tag-secret"))
			mock.ExpectRollback()
		}},
		{name: "delete media", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughTags(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnError(errors.New("delete-media-secret"))
			mock.ExpectRollback()
		}},
		{name: "media allocation", failAt: map[string]map[int]error{"idseq:article_revision_media": {1: errors.New("redis-media-secret")}}, setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughTags(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 3))
			mock.ExpectRollback()
		}},
		{name: "media insert", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughTags(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 3))
			mock.ExpectExec(insertDraftMediaSQL).
				WithArgs(int64(1), int64(21), int64(91), "cover", 0, at).
				WillReturnError(errors.New("insert-media-secret"))
			mock.ExpectRollback()
		}},
		{name: "touch article", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughMedia(mock, content, at, createdAt)
			mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnError(errors.New("touch-secret"))
			mock.ExpectRollback()
		}},
		{name: "touch rows", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughMedia(mock, content, at, createdAt)
			mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewErrorResult(errors.New("touch-rows-secret")))
			mock.ExpectRollback()
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
			counter.failAt = test.failAt
			test.setup(mock)

			_, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositorySaveDraftMapsInactiveArticleAndCommitFailure(t *testing.T) {
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	createdAt := at.Add(-time.Hour)
	content := preparedRepositoryContent()

	t.Run("inactive article", func(t *testing.T) {
		repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
		expectSaveThroughMedia(mock, content, at, createdAt)
		mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectRollback()

		_, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

		require.ErrorIs(t, err, ErrArticleInactive)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("commit", func(t *testing.T) {
		repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
		expectSaveThroughMedia(mock, content, at, createdAt)
		mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit().WillReturnError(errors.New("commit-secret"))

		_, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

		require.Error(t, err)
		require.NotContains(t, err.Error(), "commit-secret")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestMySQLRepositoryRejectsInvalidAndNilConfigurationSafely(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ids, err := idgen.New(&revisionCounter{}, nil, 1, 1, false)
	require.NoError(t, err)

	for _, repository := range []*MySQLRepository{nil, NewMySQLRepository(nil, ids), NewMySQLRepository(db, nil)} {
		require.NotPanics(t, func() {
			_, getErr := repository.GetDraft(context.Background(), 11)
			require.Error(t, getErr)
		})
	}

	repository := NewMySQLRepository(db, ids)
	_, err = repository.GetDraft(nil, 11)
	require.Error(t, err)
	_, err = repository.GetDraft(context.Background(), 0)
	require.ErrorIs(t, err, ErrInvalidContent)
	_, err = repository.SaveDraft(context.Background(), 11, 0, preparedRepositoryContent(), time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)
	invalid := preparedRepositoryContent()
	invalid.ContentHash = "not-a-hash-secret"
	_, err = repository.SaveDraft(context.Background(), 11, 1, invalid, time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryZeroIDGeneratorReturnsSanitizedErrorAndRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repository := NewMySQLRepository(db, &idgen.Generator{})
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	createdAt := at.Add(-time.Hour)
	content := preparedRepositoryContent()
	expectSaveThroughIdentity(mock, content, at, createdAt)
	mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectRollback()

	_, err = repository.SaveDraft(context.Background(), 11, 3, content, at)

	require.Error(t, err)
	require.NotContains(t, err.Error(), "configured")
	require.NoError(t, mock.ExpectationsWereMet())
}

type revisionCounter struct {
	raw    map[string]int64
	keys   []string
	calls  map[string]int
	failAt map[string]map[int]error
}

func (c *revisionCounter) Increment(_ context.Context, key string) (int64, error) {
	c.keys = append(c.keys, key)
	if c.calls == nil {
		c.calls = make(map[string]int)
	}
	c.calls[key]++
	if err := c.failAt[key][c.calls[key]]; err != nil {
		return 0, err
	}
	if c.raw == nil {
		c.raw = make(map[string]int64)
	}
	c.raw[key]++
	return c.raw[key], nil
}

func (c *revisionCounter) Raise(_ context.Context, _ string, _ int64) (int64, error) {
	return 0, errors.New("unexpected raise")
}

func newRevisionRepositoryTest(t *testing.T, offset, step int64) (*MySQLRepository, sqlmock.Sqlmock, *revisionCounter) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	counter := &revisionCounter{}
	ids, err := idgen.New(counter, nil, offset, step, false)
	require.NoError(t, err)
	return NewMySQLRepository(db, ids), mock, counter
}

func preparedRepositoryContent() PreparedContent {
	cover := &media.Media{ID: 91, PublicKey: firstMediaKey, State: "active"}
	return PreparedContent{
		Title: "Title", Summary: "Summary", Cover: cover,
		ContentMD: "![first](/img/proxy/" + firstMediaKey + ")\n![second](/img/proxy/" + secondMediaKey + ")",
		Tags: []tag.Snapshot{
			{TagID: 7, Name: "Go", Slug: "t_go", Position: 0},
			{TagID: 3, Name: "Web", Slug: "t_web", Position: 1},
		},
		Media: []media.Reference{
			{MediaID: 91, PublicKey: firstMediaKey, Purpose: "cover", Position: 0},
			{MediaID: 92, PublicKey: firstMediaKey, Purpose: "content", Position: 1},
			{MediaID: 93, PublicKey: secondMediaKey, Purpose: "content", Position: 2},
		},
		ContentHash: testContentHash,
	}
}

func expectDraftUpdate(mock sqlmock.Sqlmock, content PreparedContent, at time.Time, result sql.Result) {
	mock.ExpectExec(updateEditingDraftSQL).
		WithArgs(content.Title, content.Summary, int64(91), content.ContentMD, content.ContentHash, at, int64(11), int64(3)).
		WillReturnResult(result)
}

func expectSaveThroughIdentity(mock sqlmock.Sqlmock, content PreparedContent, at, createdAt time.Time) {
	mock.ExpectBegin()
	expectDraftUpdate(mock, content, at, sqlmock.NewResult(0, 1))
	mock.ExpectQuery(selectSavedIdentitySQL).WithArgs(int64(11)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(21), int64(4), int64(2), createdAt),
	)
}

func expectSaveThroughTags(mock sqlmock.Sqlmock, content PreparedContent, at, createdAt time.Time) {
	expectSaveThroughIdentity(mock, content, at, createdAt)
	mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(insertDraftTagSQL).
		WithArgs(int64(1), int64(21), int64(7), "Go", "t_go", 0, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftTagSQL).
		WithArgs(int64(2), int64(21), int64(3), "Web", "t_web", 1, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectSaveThroughMedia(mock sqlmock.Sqlmock, content PreparedContent, at, createdAt time.Time) {
	expectSaveThroughTags(mock, content, at, createdAt)
	mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(1), int64(21), int64(91), "cover", 0, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(2), int64(21), int64(92), "content", 1, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(3), int64(21), int64(93), "content", 2, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func draftScalarRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "article_id", "revision_no", "status", "reason", "title", "summary", "cover_media_id", "content_md", "content_hash", "lock_version", "created_at", "updated_at",
	})
}

func expectStoredDraftScalar(mock sqlmock.Sqlmock) {
	at := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(selectEditingDraftSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().AddRow(
		int64(21), int64(11), int64(1), "editing", "draft", "", "", nil, "", ComputeHash(PreparedContent{}), int64(1), at, at,
	))
}

func expectNoStoredTags(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}))
}

func revisionInt64Pointer(value int64) *int64 { return &value }
