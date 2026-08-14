package builder

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/stretchr/testify/require"
)

const (
	testSelectBuilderSQL          = "SELECT id, name, base_url, username, token_ciphertext, job_name, enabled FROM builder_config WHERE singleton_key = 1"
	testSelectBuilderForUpdateSQL = "SELECT id, name, base_url, username, token_ciphertext, job_name, enabled FROM builder_config WHERE singleton_key = 1 FOR UPDATE"
	testInsertBuilderSQL          = "INSERT INTO builder_config (id, singleton_key, name, base_url, username, token_ciphertext, job_name, enabled) VALUES (?, 1, ?, ?, ?, ?, ?, ?)"
	testUpdateBuilderSQL          = "UPDATE builder_config SET name = ?, base_url = ?, username = ?, token_ciphertext = ?, job_name = ?, enabled = ? WHERE id = ? AND singleton_key = 1"
)

func TestBuilderRepositoryCreateEncryptsTokenWithSharedIDAndReturnsOnlyView(t *testing.T) {
	repo, mock, counter, box := newBuilderRepositoryTest(t, 1)
	input := validBuilderInput()
	expectedCiphertext := sealWithNonce(t, 1, input.Token)

	mock.ExpectBegin()
	mock.ExpectQuery(testSelectBuilderForUpdateSQL).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(testInsertBuilderSQL).WithArgs(int64(1), input.Name, input.BaseURL, input.Username, expectedCiphertext, input.JobName, true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(testSelectBuilderForUpdateSQL).WillReturnRows(builderRows(1, input, expectedCiphertext))
	mock.ExpectCommit()

	view, err := repo.Save(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, ConfigView{ID: 1, Name: input.Name, BaseURL: input.BaseURL, Username: input.Username, JobName: input.JobName, Enabled: true, TokenConfigured: true}, view)
	require.Equal(t, []string{"idseq:builder_config"}, counter.keys)
	plaintext, err := box.Open(expectedCiphertext)
	require.NoError(t, err)
	require.Equal(t, input.Token, string(plaintext))
	require.NotContains(t, view.Name+view.BaseURL+view.Username+view.JobName, input.Token)
}

func TestBuilderRepositoryEmptyTokenAtomicallyPreservesCiphertextAndDiagnosesNoOp(t *testing.T) {
	repo, mock, counter, _ := newBuilderRepositoryTest(t, 3)
	storedInput := validBuilderInput()
	storedCiphertext := sealWithNonce(t, 2, storedInput.Token)
	updated := storedInput
	updated.Name = "Renamed"
	updated.Token = ""

	mock.ExpectBegin()
	mock.ExpectQuery(testSelectBuilderForUpdateSQL).WillReturnRows(builderRows(7, storedInput, storedCiphertext))
	mock.ExpectExec(testUpdateBuilderSQL).WithArgs(updated.Name, updated.BaseURL, updated.Username, storedCiphertext, updated.JobName, updated.Enabled, int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(testSelectBuilderForUpdateSQL).WillReturnRows(builderRows(7, updated, storedCiphertext))
	mock.ExpectCommit()

	view, err := repo.Save(context.Background(), updated)
	require.NoError(t, err)
	require.Equal(t, "Renamed", view.Name)
	require.True(t, view.TokenConfigured)
	require.Empty(t, counter.keys)
}

func TestBuilderRepositoryRequiresTokenForFirstSaveAndMapsSingletonConflict(t *testing.T) {
	t.Run("missing first token", func(t *testing.T) {
		repo, mock, counter, _ := newBuilderRepositoryTest(t, 1)
		input := validBuilderInput()
		input.Token = ""
		mock.ExpectBegin()
		mock.ExpectQuery(testSelectBuilderForUpdateSQL).WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()
		_, err := repo.Save(context.Background(), input)
		require.ErrorIs(t, err, ErrInvalidConfig)
		require.Empty(t, counter.keys)
	})

	t.Run("singleton conflict", func(t *testing.T) {
		repo, mock, counter, _ := newBuilderRepositoryTest(t, 1)
		input := validBuilderInput()
		ciphertext := sealWithNonce(t, 1, input.Token)
		mock.ExpectBegin()
		mock.ExpectQuery(testSelectBuilderForUpdateSQL).WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(testInsertBuilderSQL).WithArgs(int64(1), input.Name, input.BaseURL, input.Username, ciphertext, input.JobName, true).
			WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry '1' for key 'uk_builder_config_singleton'"})
		mock.ExpectRollback()
		_, err := repo.Save(context.Background(), input)
		require.ErrorIs(t, err, ErrConflict)
		require.NotContains(t, err.Error(), input.Token)
		require.Equal(t, []string{"idseq:builder_config"}, counter.keys)
		require.Empty(t, counter.raises)
	})
}

func TestBuilderRepositoryLoadReturnsCiphertextOnlyAndRejectsCorruptRows(t *testing.T) {
	repo, mock, _, _ := newBuilderRepositoryTest(t, 1)
	input := validBuilderInput()
	ciphertext := sealWithNonce(t, 4, input.Token)
	mock.ExpectQuery(testSelectBuilderSQL).WillReturnRows(builderRows(7, input, ciphertext))

	stored, err := repo.Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(7), stored.ID)
	require.Equal(t, ciphertext, stored.EncryptedToken)
	require.True(t, stored.TokenConfigured)
	require.NotContains(t, stored.Name+stored.BaseURL+stored.Username+stored.JobName, input.Token)

	mock.ExpectQuery(testSelectBuilderSQL).WillReturnRows(builderRows(7, input, "ciphertext-secret"))
	_, err = repo.Load(context.Background())
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), "ciphertext-secret")
}

func TestBuilderRepositoryEncryptionAndDependencyFailuresRollbackSanitized(t *testing.T) {
	t.Run("nonce reader", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = db.Close() })
		counter := &builderCounter{}
		ids, err := idgen.New(counter, nil, 1, 1, false)
		require.NoError(t, err)
		box, err := platform.NewSecretBox(bytes.Repeat([]byte{1}, 32), errorNonceReader{err: errors.New("nonce-secret")})
		require.NoError(t, err)
		repo := NewMySQLRepository(db, ids, &box)
		mock.ExpectBegin()
		mock.ExpectQuery(testSelectBuilderForUpdateSQL).WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()
		_, err = repo.Save(context.Background(), validBuilderInput())
		require.ErrorIs(t, err, ErrDependencyUnavailable)
		require.NotContains(t, err.Error(), "nonce-secret")
		require.Empty(t, counter.keys)
	})

	t.Run("reload mismatch", func(t *testing.T) {
		repo, mock, _, _ := newBuilderRepositoryTest(t, 1)
		input := validBuilderInput()
		ciphertext := sealWithNonce(t, 5, input.Token)
		updated := input
		updated.Name = "Expected"
		updated.Token = ""
		mismatch := updated
		mismatch.Name = "Unexpected"
		mock.ExpectBegin()
		mock.ExpectQuery(testSelectBuilderForUpdateSQL).WillReturnRows(builderRows(7, input, ciphertext))
		mock.ExpectExec(testUpdateBuilderSQL).WithArgs(updated.Name, updated.BaseURL, updated.Username, ciphertext, updated.JobName, true, int64(7)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(testSelectBuilderForUpdateSQL).WillReturnRows(builderRows(7, mismatch, ciphertext))
		mock.ExpectRollback()
		_, err := repo.Save(context.Background(), updated)
		require.ErrorIs(t, err, ErrDependencyUnavailable)
	})
}

func TestBuilderRepositoryNilSafetyAndNotFound(t *testing.T) {
	var nilBox *platform.SecretBox
	require.NotPanics(t, func() {
		repo := NewMySQLRepository(nil, nil, nilBox)
		_, err := repo.Load(context.Background())
		require.Error(t, err)
	})

	repo, mock, _, _ := newBuilderRepositoryTest(t, 1)
	mock.ExpectQuery(testSelectBuilderSQL).WillReturnError(sql.ErrNoRows)
	_, err := repo.Load(context.Background())
	require.ErrorIs(t, err, ErrNotFound)
	_, err = repo.Load(nil)
	require.Error(t, err)
	var nilRepo *MySQLRepository
	_, err = nilRepo.Save(context.Background(), validBuilderInput())
	require.Error(t, err)

	db, zeroMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, zeroMock.ExpectationsWereMet()); _ = db.Close() })
	counter := &builderCounter{}
	ids, err := idgen.New(counter, nil, 1, 1, false)
	require.NoError(t, err)
	zeroBox := &platform.SecretBox{}
	zeroRepo := NewMySQLRepository(db, ids, zeroBox)
	zeroMock.ExpectBegin()
	zeroMock.ExpectQuery(testSelectBuilderForUpdateSQL).WillReturnError(sql.ErrNoRows)
	zeroMock.ExpectRollback()
	require.NotPanics(t, func() {
		_, err = zeroRepo.Save(context.Background(), validBuilderInput())
	})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
}

func validBuilderInput() ConfigInput {
	return ConfigInput{Name: "Production", BaseURL: "https://jenkins.example.com", Username: "ci", Token: "private-token", JobName: "site/build", Enabled: true}
}

func newBuilderRepositoryTest(t *testing.T, nonce byte) (*MySQLRepository, sqlmock.Sqlmock, *builderCounter, *platform.SecretBox) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = db.Close() })
	counter := &builderCounter{}
	ids, err := idgen.New(counter, nil, 1, 1, false)
	require.NoError(t, err)
	box, err := platform.NewSecretBox(bytes.Repeat([]byte{7}, 32), bytes.NewReader(bytes.Repeat([]byte{nonce}, 12*16)))
	require.NoError(t, err)
	return NewMySQLRepository(db, ids, &box), mock, counter, &box
}

func sealWithNonce(t *testing.T, nonce byte, token string) string {
	t.Helper()
	box, err := platform.NewSecretBox(bytes.Repeat([]byte{7}, 32), bytes.NewReader(bytes.Repeat([]byte{nonce}, 12)))
	require.NoError(t, err)
	sealed, err := box.Seal([]byte(token))
	require.NoError(t, err)
	return sealed
}

func builderRows(id int64, input ConfigInput, ciphertext string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "base_url", "username", "token_ciphertext", "job_name", "enabled"}).
		AddRow(id, input.Name, input.BaseURL, input.Username, ciphertext, input.JobName, input.Enabled)
}

type builderCounter struct {
	keys   []string
	raises []string
	next   int64
}

func (c *builderCounter) Increment(_ context.Context, key string) (int64, error) {
	c.keys = append(c.keys, key)
	c.next++
	return c.next, nil
}

func (c *builderCounter) Raise(_ context.Context, key string, _ int64) (int64, error) {
	c.raises = append(c.raises, key)
	return c.next, nil
}

type errorNonceReader struct{ err error }

func (r errorNonceReader) Read([]byte) (int, error) { return 0, r.err }
