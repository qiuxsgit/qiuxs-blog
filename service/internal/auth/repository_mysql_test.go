package auth

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	insertAdminSQL = `INSERT INTO admins \(id, singleton_key, username, password_hash, state\) VALUES \(\?, \?, \?, \?, \?\)`
	selectAdminSQL = `SELECT id, username, password_hash, state FROM admins`
)

type repositoryCounter struct {
	next int64
	err  error
}

func (c *repositoryCounter) Increment(context.Context, string) (int64, error) {
	if c.err != nil {
		return 0, c.err
	}
	return c.next, nil
}

func (c *repositoryCounter) Raise(context.Context, string, int64) (int64, error) {
	return 0, errors.New("unexpected counter raise")
}

func newRepositoryTest(t *testing.T, rawID int64) (*MySQLRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ids, err := idgen.New(&repositoryCounter{next: rawID}, nil, 2, 3, false)
	require.NoError(t, err)
	return NewMySQLRepository(db, ids), mock
}

func TestMySQLRepositoryCreateUsesGeneratedSignedIDAndSingleton(t *testing.T) {
	repo, mock := newRepositoryTest(t, 4)
	mock.ExpectExec(insertAdminSQL).
		WithArgs(int64(11), 1, "qiuxs", "encoded-hash", "active").
		WillReturnResult(sqlmock.NewResult(999, 1))

	admin, err := repo.Create(context.Background(), "  Qiuxs  ", "encoded-hash")

	require.NoError(t, err)
	assert.Equal(t, Admin{ID: 11, Username: "qiuxs", PasswordHash: "encoded-hash", State: "active"}, admin)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryCreateRequiresIDGenerator(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = NewMySQLRepository(db, nil).Create(context.Background(), "qiuxs", "encoded-hash")

	assert.ErrorContains(t, err, "ID generator")
}

func TestMySQLRepositoryCreateRejectsInvalidUsernameBeforeAllocatingID(t *testing.T) {
	counter := &repositoryCounter{err: errors.New("counter must not be called")}
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ids, err := idgen.New(counter, nil, 1, 1, false)
	require.NoError(t, err)

	_, err = NewMySQLRepository(db, ids).Create(context.Background(), "bad name", "encoded-hash")

	assert.ErrorContains(t, err, "username")
}

func TestMySQLRepositoryCreateClassifiesOnlyNamedBusinessConflicts(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		wantExists bool
	}{
		{name: "username", key: "uk_admins_username", wantExists: true},
		{name: "singleton", key: "uk_admins_singleton", wantExists: true},
		{name: "qualified singleton", key: "admins.uk_admins_singleton", wantExists: true},
		{name: "unrelated unique", key: "uk_admins_other", wantExists: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock := newRepositoryTest(t, 1)
			mysqlErr := &mysql.MySQLError{Number: 1062, Message: fmt.Sprintf("Duplicate entry 'x' for key '%s'", tt.key)}
			mock.ExpectExec(insertAdminSQL).WillReturnError(mysqlErr)

			_, err := repo.Create(context.Background(), "qiuxs", "encoded-hash")

			assert.Equal(t, tt.wantExists, errors.Is(err, ErrAdminAlreadyExists))
			assert.ErrorIs(t, err, mysqlErr)
			assert.False(t, idgen.IsPKConflict(err))
		})
	}
}

func TestMySQLRepositoryCreateLeavesPrimaryConflictDetectable(t *testing.T) {
	repo, mock := newRepositoryTest(t, 1)
	mysqlErr := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry '2' for key 'PRIMARY'"}
	mock.ExpectExec(insertAdminSQL).WillReturnError(mysqlErr)

	_, err := repo.Create(context.Background(), "qiuxs", "encoded-hash")

	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrAdminAlreadyExists))
	assert.True(t, idgen.IsPKConflict(err))
}

func TestMySQLRepositoryCount(t *testing.T) {
	repo, mock := newRepositoryTest(t, 1)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM admins`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	count, err := repo.Count(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestMySQLRepositoryFindByUsernameNormalizesAndSelectsOnlyAuthFields(t *testing.T) {
	repo, mock := newRepositoryTest(t, 1)
	mock.ExpectQuery(selectAdminSQL + ` WHERE username = \?`).
		WithArgs("qiuxs").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "state"}).AddRow(int64(7), "qiuxs", "encoded-hash", "active"))

	admin, err := repo.FindByUsername(context.Background(), " QIUXS ")

	require.NoError(t, err)
	assert.Equal(t, Admin{ID: 7, Username: "qiuxs", PasswordHash: "encoded-hash", State: "active"}, admin)
}

func TestMySQLRepositoryFindMapsNoRowsToAdminNotFound(t *testing.T) {
	tests := []struct {
		name string
		call func(*MySQLRepository) (Admin, error)
		sql  string
		arg  any
	}{
		{name: "by username", call: func(r *MySQLRepository) (Admin, error) { return r.FindByUsername(context.Background(), "qiuxs") }, sql: selectAdminSQL + ` WHERE username = \?`, arg: "qiuxs"},
		{name: "by id", call: func(r *MySQLRepository) (Admin, error) { return r.FindByID(context.Background(), 7) }, sql: selectAdminSQL + ` WHERE id = \?`, arg: int64(7)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock := newRepositoryTest(t, 1)
			mock.ExpectQuery(tt.sql).WithArgs(tt.arg).WillReturnError(sql.ErrNoRows)

			_, err := tt.call(repo)

			assert.ErrorIs(t, err, ErrAdminNotFound)
		})
	}
}

func TestMySQLRepositoryUpdateLastLoginUsesUTC(t *testing.T) {
	repo, mock := newRepositoryTest(t, 1)
	local := time.Date(2026, 8, 13, 14, 30, 0, 123000, time.FixedZone("CST", 8*60*60))
	mock.ExpectExec(`UPDATE admins SET last_login_at = \? WHERE id = \?`).
		WithArgs(local.UTC(), int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateLastLogin(context.Background(), 7, local)

	assert.NoError(t, err)
}
