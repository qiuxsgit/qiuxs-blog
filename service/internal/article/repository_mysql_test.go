package article

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
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
	"github.com/stretchr/testify/require"
)

const (
	insertArticleSQL         = `INSERT INTO articles (id, slug, draft_revision_id, published_revision_id, state, created_at, updated_at) VALUES (?, ?, NULL, NULL, 'active', ?, ?)`
	insertInitialRevisionSQL = `INSERT INTO article_revisions (id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at) VALUES (?, ?, 1, 'editing', 'draft', '', '', NULL, '', ?, 1, ?, ?)`
	setDraftPointerSQL       = `UPDATE articles SET draft_revision_id = ?, updated_at = ? WHERE id = ? AND draft_revision_id IS NULL`
	selectArticleSQL         = `SELECT id, slug, draft_revision_id, published_revision_id, state, created_at, updated_at FROM articles WHERE id = ?`
	listArticlesSQL          = `SELECT a.id, a.slug, a.draft_revision_id, a.published_revision_id, a.state, a.created_at, a.updated_at, r.title, r.updated_at FROM articles a JOIN article_revisions r ON r.id = a.draft_revision_id WHERE a.state = ? ORDER BY r.updated_at DESC, a.id ASC`
	trashArticleSQL          = `UPDATE articles SET state = 'trashed', updated_at = ? WHERE id = ? AND state = 'active' AND published_revision_id IS NULL`
	untrashArticleSQL        = `UPDATE articles SET state = 'active', updated_at = ? WHERE id = ? AND state = 'trashed'`
)

func TestMySQLRepositoryCreateUsesOneTransactionAndSharedSignedIDGenerator(t *testing.T) {
	repository, mock, counter := newArticleRepositoryTest(t, 2, 3)
	at := time.Date(2026, 8, 14, 9, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	hash := revision.ComputeHash(revision.PreparedContent{})
	mock.ExpectBegin()
	mock.ExpectExec(insertArticleSQL).
		WithArgs(int64(2), "aaaaaaaaaaaa", at.UTC(), at.UTC()).
		WillReturnResult(sqlmock.NewResult(100, 1))
	mock.ExpectExec(insertInitialRevisionSQL).
		WithArgs(int64(2), int64(2), hash, at.UTC(), at.UTC()).
		WillReturnResult(sqlmock.NewResult(101, 1))
	mock.ExpectExec(setDraftPointerSQL).
		WithArgs(int64(2), at.UTC(), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := repository.Create(context.Background(), "aaaaaaaaaaaa", at)

	require.NoError(t, err)
	require.Equal(t, Article{
		ID: 2, Slug: "aaaaaaaaaaaa", DraftRevisionID: 2, State: StateActive,
		CreatedAt: at.UTC(), UpdatedAt: at.UTC(),
	}, got)
	require.Equal(t, []string{"idseq:articles", "idseq:article_revisions"}, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryCreateMapsOnlyNamedArticleSlugConflict(t *testing.T) {
	for _, test := range []struct {
		name string
		key  string
		want bool
	}{
		{name: "exact", key: "uk_articles_slug", want: true},
		{name: "qualified", key: "articles.uk_articles_slug", want: true},
		{name: "substring", key: "uk_articles_slug_backup"},
		{name: "other", key: "uk_article_revisions_no"},
		{name: "primary", key: "PRIMARY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newArticleRepositoryTest(t, 1, 1)
			mysqlErr := &mysql.MySQLError{Number: 1062, Message: fmt.Sprintf("Duplicate entry 'slug-secret' for key '%s'", test.key)}
			at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
			mock.ExpectBegin()
			mock.ExpectExec(insertArticleSQL).
				WithArgs(int64(1), "aaaaaaaaaaaa", at, at).
				WillReturnError(mysqlErr)
			mock.ExpectRollback()

			_, err := repository.Create(context.Background(), "aaaaaaaaaaaa", at)

			require.Error(t, err)
			require.Equal(t, test.want, errors.Is(err, ErrSlugConflict))
			require.ErrorIs(t, err, mysqlErr)
			require.NotContains(t, err.Error(), "slug-secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryCreateRollsBackEveryPartialWriteFailure(t *testing.T) {
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	hash := revision.ComputeHash(revision.PreparedContent{})
	tests := []struct {
		name       string
		counterErr map[string]error
		setup      func(sqlmock.Sqlmock)
	}{
		{name: "article allocation", counterErr: map[string]error{"idseq:articles": errors.New("redis-secret")}, setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectRollback()
		}},
		{name: "article insert", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(insertArticleSQL).
				WithArgs(int64(1), "aaaaaaaaaaaa", at, at).
				WillReturnError(errors.New("article-insert-secret"))
			mock.ExpectRollback()
		}},
		{name: "revision allocation", counterErr: map[string]error{"idseq:article_revisions": errors.New("redis-secret")}, setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(insertArticleSQL).
				WithArgs(int64(1), "aaaaaaaaaaaa", at, at).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectRollback()
		}},
		{name: "revision insert", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(insertArticleSQL).
				WithArgs(int64(1), "aaaaaaaaaaaa", at, at).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(insertInitialRevisionSQL).
				WithArgs(int64(1), int64(1), hash, at, at).
				WillReturnError(errors.New("revision-insert-secret"))
			mock.ExpectRollback()
		}},
		{name: "pointer update", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(insertArticleSQL).
				WithArgs(int64(1), "aaaaaaaaaaaa", at, at).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(insertInitialRevisionSQL).
				WithArgs(int64(1), int64(1), hash, at, at).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(setDraftPointerSQL).
				WithArgs(int64(1), at, int64(1)).
				WillReturnError(errors.New("pointer-secret"))
			mock.ExpectRollback()
		}},
		{name: "pointer rows affected", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(insertArticleSQL).
				WithArgs(int64(1), "aaaaaaaaaaaa", at, at).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(insertInitialRevisionSQL).
				WithArgs(int64(1), int64(1), hash, at, at).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(setDraftPointerSQL).
				WithArgs(int64(1), at, int64(1)).
				WillReturnResult(sqlmock.NewErrorResult(errors.New("rows-secret")))
			mock.ExpectRollback()
		}},
		{name: "conditional pointer lost", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(insertArticleSQL).
				WithArgs(int64(1), "aaaaaaaaaaaa", at, at).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(insertInitialRevisionSQL).
				WithArgs(int64(1), int64(1), hash, at, at).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(setDraftPointerSQL).
				WithArgs(int64(1), at, int64(1)).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectRollback()
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, counter := newArticleRepositoryTest(t, 1, 1)
			counter.errors = test.counterErr
			test.setup(mock)

			_, err := repository.Create(context.Background(), "aaaaaaaaaaaa", at)

			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryCreateSanitizesBeginAndCommitFailures(t *testing.T) {
	t.Run("begin", func(t *testing.T) {
		repository, mock, _ := newArticleRepositoryTest(t, 1, 1)
		mock.ExpectBegin().WillReturnError(errors.New("begin-secret"))

		_, err := repository.Create(context.Background(), "aaaaaaaaaaaa", time.Now())

		require.Error(t, err)
		require.NotContains(t, err.Error(), "begin-secret")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("commit", func(t *testing.T) {
		repository, mock, _ := newArticleRepositoryTest(t, 1, 1)
		at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
		hash := revision.ComputeHash(revision.PreparedContent{})
		mock.ExpectBegin()
		mock.ExpectExec(insertArticleSQL).
			WithArgs(int64(1), "aaaaaaaaaaaa", at, at).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(insertInitialRevisionSQL).
			WithArgs(int64(1), int64(1), hash, at, at).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(setDraftPointerSQL).
			WithArgs(int64(1), at, int64(1)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit().WillReturnError(errors.New("commit-secret"))

		_, err := repository.Create(context.Background(), "aaaaaaaaaaaa", at)

		require.Error(t, err)
		require.NotContains(t, err.Error(), "commit-secret")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestMySQLRepositoryFindByIDScansNullablePublishedRevision(t *testing.T) {
	for _, test := range []struct {
		name      string
		published any
		want      *int64
	}{
		{name: "unpublished"},
		{name: "published", published: int64(31), want: articleInt64Pointer(31)},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newArticleRepositoryTest(t, 1, 1)
			createdAt := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
			updatedAt := createdAt.Add(time.Hour)
			mock.ExpectQuery(selectArticleSQL).WithArgs(int64(11)).WillReturnRows(
				sqlmock.NewRows([]string{"id", "slug", "draft_revision_id", "published_revision_id", "state", "created_at", "updated_at"}).
					AddRow(int64(11), "aaaaaaaaaaaa", int64(21), test.published, "active", createdAt, updatedAt),
			)

			got, err := repository.FindByID(context.Background(), 11)

			require.NoError(t, err)
			require.Equal(t, test.want, got.PublishedRevisionID)
			require.Equal(t, int64(21), got.DraftRevisionID)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryFindByIDMapsMissingAndSanitizesScanErrors(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		repository, mock, _ := newArticleRepositoryTest(t, 1, 1)
		mock.ExpectQuery(selectArticleSQL).WithArgs(int64(11)).WillReturnError(sql.ErrNoRows)

		_, err := repository.FindByID(context.Background(), 11)

		require.ErrorIs(t, err, ErrNotFound)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("scan", func(t *testing.T) {
		repository, mock, _ := newArticleRepositoryTest(t, 1, 1)
		mock.ExpectQuery(selectArticleSQL).WithArgs(int64(11)).WillReturnRows(
			sqlmock.NewRows([]string{"id", "slug", "draft_revision_id", "published_revision_id", "state", "created_at", "updated_at"}).
				AddRow("id-secret", "aaaaaaaaaaaa", int64(21), nil, "active", time.Now(), time.Now()),
		)

		_, err := repository.FindByID(context.Background(), 11)

		require.Error(t, err)
		require.NotContains(t, err.Error(), "id-secret")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestMySQLRepositoryListUsesDraftTimeThenArticleIDOrder(t *testing.T) {
	repository, mock, _ := newArticleRepositoryTest(t, 1, 1)
	createdAt := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	newest := createdAt.Add(3 * time.Hour)
	older := createdAt.Add(2 * time.Hour)
	mock.ExpectQuery(listArticlesSQL).WithArgs(StateActive).WillReturnRows(
		sqlmock.NewRows([]string{"id", "slug", "draft_revision_id", "published_revision_id", "state", "created_at", "updated_at", "title", "draft_updated_at"}).
			AddRow(int64(9), "bbbbbbbbbbbb", int64(29), nil, "active", createdAt, newest, "Newest", newest).
			AddRow(int64(2), "aaaaaaaaaaaa", int64(22), int64(32), "active", createdAt, older, "Older", older),
	)

	got, err := repository.List(context.Background(), StateActive)

	require.NoError(t, err)
	require.Equal(t, []Summary{
		{Article: Article{ID: 9, Slug: "bbbbbbbbbbbb", DraftRevisionID: 29, State: StateActive, CreatedAt: createdAt, UpdatedAt: newest}, DraftTitle: "Newest", DraftUpdatedAt: newest},
		{Article: Article{ID: 2, Slug: "aaaaaaaaaaaa", DraftRevisionID: 22, PublishedRevisionID: articleInt64Pointer(32), State: StateActive, CreatedAt: createdAt, UpdatedAt: older}, DraftTitle: "Older", DraftUpdatedAt: older},
	}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositorySetStateUsesConditionalUpdates(t *testing.T) {
	at := time.Date(2026, 8, 14, 9, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	for _, test := range []struct {
		name, query string
		from, to    State
	}{
		{name: "trash", query: trashArticleSQL, from: StateActive, to: StateTrashed},
		{name: "untrash", query: untrashArticleSQL, from: StateTrashed, to: StateActive},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newArticleRepositoryTest(t, 1, 1)
			mock.ExpectExec(test.query).WithArgs(at.UTC(), int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))

			err := repository.SetState(context.Background(), 11, test.from, test.to, at)

			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryTrashZeroRowsReloadsForPublishedAndRaceErrors(t *testing.T) {
	publishedID := int64(31)
	for _, test := range []struct {
		name      string
		findError error
		state     State
		published *int64
		want      error
	}{
		{name: "published raced", state: StateActive, published: &publishedID, want: ErrMustBeUnpublished},
		{name: "state raced", state: StateTrashed, want: ErrStateConflict},
		{name: "active mismatch", state: StateActive, want: ErrStateConflict},
		{name: "deleted raced", findError: sql.ErrNoRows, want: ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newArticleRepositoryTest(t, 1, 1)
			at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
			mock.ExpectExec(trashArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewResult(0, 0))
			query := mock.ExpectQuery(selectArticleSQL).WithArgs(int64(11))
			if test.findError != nil {
				query.WillReturnError(test.findError)
			} else {
				query.WillReturnRows(articleRow(11, test.state, test.published))
			}

			err := repository.SetState(context.Background(), 11, StateActive, StateTrashed, at)

			require.ErrorIs(t, err, test.want)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryUntrashZeroRowsReloadsForMissingOrStateConflict(t *testing.T) {
	for _, test := range []struct {
		name      string
		findError error
		want      error
	}{
		{name: "missing", findError: sql.ErrNoRows, want: ErrNotFound},
		{name: "state raced", want: ErrStateConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newArticleRepositoryTest(t, 1, 1)
			at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
			mock.ExpectExec(untrashArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewResult(0, 0))
			query := mock.ExpectQuery(selectArticleSQL).WithArgs(int64(11))
			if test.findError != nil {
				query.WillReturnError(test.findError)
			} else {
				query.WillReturnRows(articleRow(11, StateActive, nil))
			}

			err := repository.SetState(context.Background(), 11, StateTrashed, StateActive, at)

			require.ErrorIs(t, err, test.want)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryRejectsInvalidAndNilConfigurationWithoutSQL(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ids, err := idgen.New(&articleCounter{}, nil, 1, 1, false)
	require.NoError(t, err)

	for _, repository := range []*MySQLRepository{nil, NewMySQLRepository(nil, ids), NewMySQLRepository(db, nil), NewMySQLRepository(db, &idgen.Generator{})} {
		require.NotPanics(t, func() {
			_, createErr := repository.Create(context.Background(), "aaaaaaaaaaaa", time.Now())
			require.Error(t, createErr)
		})
	}

	repository := NewMySQLRepository(db, ids)
	_, err = repository.FindByID(context.Background(), 0)
	require.ErrorIs(t, err, ErrStateConflict)
	_, err = repository.List(context.Background(), State("ACTIVE"))
	require.ErrorIs(t, err, ErrStateConflict)
	err = repository.SetState(context.Background(), 11, StateActive, StateActive, time.Now())
	require.ErrorIs(t, err, ErrStateConflict)
	_, err = repository.FindByID(nil, 11)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

type articleCounter struct {
	raw    map[string]int64
	keys   []string
	errors map[string]error
}

func (c *articleCounter) Increment(_ context.Context, key string) (int64, error) {
	c.keys = append(c.keys, key)
	if err := c.errors[key]; err != nil {
		return 0, err
	}
	if c.raw == nil {
		c.raw = make(map[string]int64)
	}
	c.raw[key]++
	return c.raw[key], nil
}

func (c *articleCounter) Raise(_ context.Context, _ string, _ int64) (int64, error) {
	return 0, errors.New("unexpected raise")
}

func newArticleRepositoryTest(t *testing.T, offset, step int64) (*MySQLRepository, sqlmock.Sqlmock, *articleCounter) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	counter := &articleCounter{}
	ids, err := idgen.New(counter, nil, offset, step, false)
	require.NoError(t, err)
	return NewMySQLRepository(db, ids), mock, counter
}

func articleInt64Pointer(value int64) *int64 { return &value }

func articleRow(id int64, state State, published *int64) *sqlmock.Rows {
	var publishedValue any
	if published != nil {
		publishedValue = *published
	}
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{"id", "slug", "draft_revision_id", "published_revision_id", "state", "created_at", "updated_at"}).
		AddRow(id, "aaaaaaaaaaaa", int64(21), publishedValue, state, at, at)
}
