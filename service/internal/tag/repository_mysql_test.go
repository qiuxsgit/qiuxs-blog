package tag

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
	insertTagSQL = `INSERT INTO tags (id, name, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
	selectTagSQL = `SELECT id, name, slug, created_at, updated_at FROM tags`
	renameTagSQL = `UPDATE tags SET name = ?, updated_at = ? WHERE id = ?`
)

func TestMySQLRepositoryCreateUsesSharedTagIDGeneratorAndSignedInt64(t *testing.T) {
	repository, mock, counter := newRepositoryTest(t, 4)
	at := time.Date(2026, 8, 13, 18, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	mock.ExpectExec(insertTagSQL).
		WithArgs(int64(11), "Modern Go", "t_stable_slug", at.UTC(), at.UTC()).
		WillReturnResult(sqlmock.NewResult(999, 1))

	got, err := repository.Create(context.Background(), "Modern Go", "t_stable_slug", at)

	require.NoError(t, err)
	require.Equal(t, Tag{
		ID:        11,
		Name:      "Modern Go",
		Slug:      "t_stable_slug",
		CreatedAt: at.UTC(),
		UpdatedAt: at.UTC(),
	}, got)
	require.Equal(t, []string{"idseq:tags"}, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryCreateMapsOnlyNamedTagUniqueConflicts(t *testing.T) {
	for _, test := range []struct {
		name string
		key  string
		want error
	}{
		{name: "name", key: "uk_tags_name", want: ErrNameConflict},
		{name: "qualified name", key: "tags.uk_tags_name", want: ErrNameConflict},
		{name: "slug", key: "uk_tags_slug", want: ErrSlugConflict},
		{name: "qualified slug", key: "tags.uk_tags_slug", want: ErrSlugConflict},
		{name: "other unique", key: "uk_tags_other"},
		{name: "name substring", key: "uk_tags_name_backup"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newRepositoryTest(t, 1)
			mysqlErr := &mysql.MySQLError{Number: 1062, Message: fmt.Sprintf("Duplicate entry 'name-slug-secret' for key '%s'", test.key)}
			mock.ExpectExec(insertTagSQL).WillReturnError(mysqlErr)

			_, err := repository.Create(context.Background(), "Secret Name", "t_secret_slug", time.Now())

			require.Error(t, err)
			if test.want == nil {
				require.False(t, errors.Is(err, ErrNameConflict))
				require.False(t, errors.Is(err, ErrSlugConflict))
			} else {
				require.ErrorIs(t, err, test.want)
			}
			require.ErrorIs(t, err, mysqlErr)
			require.NotContains(t, err.Error(), "name-slug-secret")
		})
	}
}

func TestMySQLRepositoryCreateLeavesPrimaryConflictForIDGenerator(t *testing.T) {
	repository, mock, _ := newRepositoryTest(t, 1)
	mysqlErr := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry '2' for key 'PRIMARY'"}
	mock.ExpectExec(insertTagSQL).WillReturnError(mysqlErr)

	_, err := repository.Create(context.Background(), "Go", "t_go", time.Now())

	require.Error(t, err)
	require.True(t, idgen.IsPKConflict(err))
	require.False(t, errors.Is(err, ErrNameConflict))
	require.False(t, errors.Is(err, ErrSlugConflict))
}

func TestMySQLRepositoryCreateStopsBeforeInsertWhenAllocationFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	counter := &repositoryCounter{err: errors.New("redis-counter-secret")}
	ids, err := idgen.New(counter, nil, 1, 1, false)
	require.NoError(t, err)

	_, err = NewMySQLRepository(db, ids).Create(context.Background(), "Go", "t_go", time.Now())

	require.Error(t, err)
	require.NotContains(t, err.Error(), "redis-counter-secret")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryListUsesNameThenIDOrder(t *testing.T) {
	repository, mock, _ := newRepositoryTest(t, 1)
	createdAt := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	mock.ExpectQuery(selectTagSQL + ` ORDER BY name ASC, id ASC`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "created_at", "updated_at"}).
			AddRow(int64(9), "Go", "t_go", createdAt, updatedAt).
			AddRow(int64(2), "Rust", "t_rust", createdAt, updatedAt))

	got, err := repository.List(context.Background())

	require.NoError(t, err)
	require.Equal(t, []Tag{
		{ID: 9, Name: "Go", Slug: "t_go", CreatedAt: createdAt, UpdatedAt: updatedAt},
		{ID: 2, Name: "Rust", Slug: "t_rust", CreatedAt: createdAt, UpdatedAt: updatedAt},
	}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryFindByIDsUsesOnlyBindMarkersAndIDOrder(t *testing.T) {
	repository, mock, _ := newRepositoryTest(t, 1)
	createdAt := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(selectTagSQL+` WHERE id IN (?, ?) ORDER BY id ASC`).
		WithArgs(int64(9), int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "created_at", "updated_at"}).
			AddRow(int64(2), "Rust", "t_rust", createdAt, createdAt).
			AddRow(int64(9), "Go", "t_go", createdAt, createdAt))

	got, err := repository.FindByIDs(context.Background(), []int64{9, 2})

	require.NoError(t, err)
	require.Equal(t, []Tag{
		{ID: 2, Name: "Rust", Slug: "t_rust", CreatedAt: createdAt, UpdatedAt: createdAt},
		{ID: 9, Name: "Go", Slug: "t_go", CreatedAt: createdAt, UpdatedAt: createdAt},
	}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryFindByIDsRejectsInvalidInputBeforeQuery(t *testing.T) {
	for _, ids := range [][]int64{nil, {}, {0}, {-1}, {1, 0}} {
		repository, mock, _ := newRepositoryTest(t, 1)

		got, err := repository.FindByIDs(context.Background(), ids)

		require.Nil(t, got)
		require.ErrorIs(t, err, ErrInvalidSelection)
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestMySQLRepositoryRenameUpdatesNameThenReadsStableSlug(t *testing.T) {
	repository, mock, _ := newRepositoryTest(t, 1)
	at := time.Date(2026, 8, 13, 18, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	createdAt := at.UTC().Add(-time.Hour)
	mock.ExpectExec(renameTagSQL).
		WithArgs("Modern Go", at.UTC(), int64(41)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(selectTagSQL + ` WHERE id = ?`).
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "created_at", "updated_at"}).
			AddRow(int64(41), "Modern Go", "t_stable_slug", createdAt, at.UTC()))

	got, err := repository.Rename(context.Background(), 41, "Modern Go", at)

	require.NoError(t, err)
	require.Equal(t, Tag{ID: 41, Name: "Modern Go", Slug: "t_stable_slug", CreatedAt: createdAt, UpdatedAt: at.UTC()}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryRenameMapsZeroRowsAndMissingSelectToNotFound(t *testing.T) {
	t.Run("zero update rows", func(t *testing.T) {
		repository, mock, _ := newRepositoryTest(t, 1)
		mock.ExpectExec(renameTagSQL).
			WillReturnResult(sqlmock.NewResult(0, 0))

		_, err := repository.Rename(context.Background(), 41, "Go", time.Now())

		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("select disappeared", func(t *testing.T) {
		repository, mock, _ := newRepositoryTest(t, 1)
		mock.ExpectExec(renameTagSQL).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(selectTagSQL + ` WHERE id = ?`).WillReturnError(sql.ErrNoRows)

		_, err := repository.Rename(context.Background(), 41, "Go", time.Now())

		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestMySQLRepositoryRenameMapsOnlyNameConflict(t *testing.T) {
	for _, test := range []struct {
		name string
		key  string
		want bool
	}{
		{name: "name", key: "uk_tags_name", want: true},
		{name: "slug", key: "uk_tags_slug", want: false},
		{name: "other", key: "uk_tags_other", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newRepositoryTest(t, 1)
			mysqlErr := &mysql.MySQLError{Number: 1062, Message: fmt.Sprintf("Duplicate entry 'rename-secret' for key '%s'", test.key)}
			mock.ExpectExec(renameTagSQL).WillReturnError(mysqlErr)

			_, err := repository.Rename(context.Background(), 41, "Secret Name", time.Now())

			require.Equal(t, test.want, errors.Is(err, ErrNameConflict))
			require.ErrorIs(t, err, mysqlErr)
			require.NotContains(t, err.Error(), "rename-secret")
		})
	}
}

func TestMySQLRepositoryRejectsInvalidDependenciesInputsAndNilContext(t *testing.T) {
	valid, _, _ := newRepositoryTest(t, 1)
	var nilRepository *MySQLRepository
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, call := range []struct {
		name string
		fn   func() error
	}{
		{name: "nil receiver", fn: func() error { _, callErr := nilRepository.List(context.Background()); return callErr }},
		{name: "nil database", fn: func() error {
			_, callErr := NewMySQLRepository(nil, valid.ids).List(context.Background())
			return callErr
		}},
		{name: "nil generator", fn: func() error { _, callErr := NewMySQLRepository(db, nil).List(context.Background()); return callErr }},
		{name: "nil context create", fn: func() error { _, callErr := valid.Create(nil, "Go", "t_go", time.Now()); return callErr }},
		{name: "nil context list", fn: func() error { _, callErr := valid.List(nil); return callErr }},
		{name: "nil context rename", fn: func() error { _, callErr := valid.Rename(nil, 1, "Go", time.Now()); return callErr }},
		{name: "nil context find", fn: func() error { _, callErr := valid.FindByIDs(nil, []int64{1}); return callErr }},
		{name: "invalid create name", fn: func() error { _, callErr := valid.Create(context.Background(), "", "t_go", time.Now()); return callErr }},
		{name: "invalid create slug", fn: func() error { _, callErr := valid.Create(context.Background(), "Go", "", time.Now()); return callErr }},
		{name: "invalid rename ID", fn: func() error { _, callErr := valid.Rename(context.Background(), 0, "Go", time.Now()); return callErr }},
		{name: "invalid rename name", fn: func() error { _, callErr := valid.Rename(context.Background(), 1, "", time.Now()); return callErr }},
	} {
		t.Run(call.name, func(t *testing.T) {
			require.NotPanics(t, func() { require.Error(t, call.fn()) })
		})
	}
}

func TestMySQLRepositorySanitizesDependencyErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(*MySQLRepository) error
		set  func(sqlmock.Sqlmock)
	}{
		{name: "list", call: func(repository *MySQLRepository) error { _, err := repository.List(context.Background()); return err }, set: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(selectTagSQL + ` ORDER BY name ASC, id ASC`).WillReturnError(errors.New("list-name-slug-secret"))
		}},
		{name: "find", call: func(repository *MySQLRepository) error {
			_, err := repository.FindByIDs(context.Background(), []int64{1})
			return err
		}, set: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(selectTagSQL + ` WHERE id IN (?) ORDER BY id ASC`).WillReturnError(errors.New("find-name-slug-secret"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newRepositoryTest(t, 1)
			test.set(mock)

			err := test.call(repository)

			require.Error(t, err)
			require.NotContains(t, err.Error(), "name-slug-secret")
		})
	}
}

type repositoryCounter struct {
	next int64
	err  error
	keys []string
}

func (c *repositoryCounter) Increment(_ context.Context, key string) (int64, error) {
	c.keys = append(c.keys, key)
	if c.err != nil {
		return 0, c.err
	}
	return c.next, nil
}

func (*repositoryCounter) Raise(context.Context, string, int64) (int64, error) {
	return 0, errors.New("unexpected counter raise")
}

func newRepositoryTest(t *testing.T, rawID int64) (*MySQLRepository, sqlmock.Sqlmock, *repositoryCounter) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	counter := &repositoryCounter{next: rawID}
	ids, err := idgen.New(counter, nil, 2, 3, false)
	require.NoError(t, err)
	return NewMySQLRepository(db, ids), mock, counter
}
