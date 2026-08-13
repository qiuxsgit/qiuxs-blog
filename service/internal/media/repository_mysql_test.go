package media

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
	mediaColumns       = "id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at"
	insertMediaSQL     = "INSERT INTO media (id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)"
	findMediaByGFSSQL  = "SELECT " + mediaColumns + " FROM media WHERE gfs_file_id = ?"
	findActiveByIDSQL  = "SELECT " + mediaColumns + " FROM media WHERE id = ? AND state = 'active'"
	findActiveByKeySQL = "SELECT " + mediaColumns + " FROM media WHERE public_key = ? AND state = 'active'"
)

func TestMySQLRepositoryCreateUsesSharedMediaIDGeneratorAndExactInsert(t *testing.T) {
	repository, mock, counter := newMediaRepositoryTest(t, 4)
	at := time.Date(2026, 8, 13, 18, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	input := validNewMedia()
	mock.ExpectExec(insertMediaSQL).
		WithArgs(int64(11), input.PublicKey, input.GFSFileID, input.OriginalName, input.MIMEType, input.FileSize, input.Width, input.Height, at.UTC(), at.UTC()).
		WillReturnResult(sqlmock.NewResult(999, 1))

	got, err := repository.Create(context.Background(), input, at)

	require.NoError(t, err)
	require.Equal(t, Media{
		ID: 11, PublicKey: input.PublicKey, GFSFileID: input.GFSFileID,
		OriginalName: input.OriginalName, MIMEType: input.MIMEType, FileSize: input.FileSize,
		Width: input.Width, Height: input.Height, State: "active", CreatedAt: at.UTC(), UpdatedAt: at.UTC(),
	}, got)
	require.Equal(t, []string{"idseq:media"}, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryCreateMapsOnlyNamedMediaUniqueConflicts(t *testing.T) {
	for _, test := range []struct {
		name string
		key  string
		want error
	}{
		{name: "public key", key: "uk_media_public_key", want: ErrPublicKeyConflict},
		{name: "qualified public key", key: "media.uk_media_public_key", want: ErrPublicKeyConflict},
		{name: "GFS file ID", key: "uk_media_gfs_file_id", want: ErrGFSFileIDConflict},
		{name: "qualified GFS file ID", key: "media.uk_media_gfs_file_id", want: ErrGFSFileIDConflict},
		{name: "unrelated unique", key: "uk_media_other"},
		{name: "public key substring", key: "uk_media_public_key_backup"},
		{name: "GFS key substring", key: "uk_media_gfs_file_id_backup"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newMediaRepositoryTest(t, 1)
			mysqlErr := &mysql.MySQLError{Number: 1062, Message: fmt.Sprintf("Duplicate entry 'media-secret' for key '%s'", test.key)}
			mock.ExpectExec(insertMediaSQL).WillReturnError(mysqlErr)

			_, err := repository.Create(context.Background(), validNewMedia(), time.Now())

			require.Error(t, err)
			if test.want == nil {
				require.False(t, errors.Is(err, ErrPublicKeyConflict))
				require.False(t, errors.Is(err, ErrGFSFileIDConflict))
			} else {
				require.ErrorIs(t, err, test.want)
			}
			require.ErrorIs(t, err, mysqlErr)
			require.NotContains(t, err.Error(), "media-secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryCreateLeavesPrimaryConflictForIDGenerator(t *testing.T) {
	repository, mock, _ := newMediaRepositoryTest(t, 1)
	mysqlErr := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry '2' for key 'PRIMARY'"}
	mock.ExpectExec(insertMediaSQL).WillReturnError(mysqlErr)

	_, err := repository.Create(context.Background(), validNewMedia(), time.Now())

	require.Error(t, err)
	require.True(t, idgen.IsPKConflict(err))
	require.False(t, errors.Is(err, ErrPublicKeyConflict))
	require.False(t, errors.Is(err, ErrGFSFileIDConflict))
}

func TestMySQLRepositoryFindByGFSFileIDUsesExactQueryAndMapsMissing(t *testing.T) {
	repository, mock, _ := newMediaRepositoryTest(t, 1)
	want := storedMedia(31, "m_0000000000000000000001", 91)
	mock.ExpectQuery(findMediaByGFSSQL).
		WithArgs(int64(91)).
		WillReturnRows(mediaRows(want))

	got, err := repository.FindByGFSFileID(context.Background(), 91)

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.NoError(t, mock.ExpectationsWereMet())

	repository, mock, _ = newMediaRepositoryTest(t, 1)
	mock.ExpectQuery(findMediaByGFSSQL).WithArgs(int64(92)).WillReturnError(sql.ErrNoRows)
	_, err = repository.FindByGFSFileID(context.Background(), 92)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryFindActiveByIDUsesExactActiveQuery(t *testing.T) {
	repository, mock, _ := newMediaRepositoryTest(t, 1)
	want := storedMedia(31, "m_0000000000000000000001", 91)
	mock.ExpectQuery(findActiveByIDSQL).WithArgs(int64(31)).WillReturnRows(mediaRows(want))

	got, err := repository.FindActiveByID(context.Background(), 31)

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryFindActiveByIDsUsesOnlyBindMarkers(t *testing.T) {
	repository, mock, _ := newMediaRepositoryTest(t, 1)
	first := storedMedia(31, "m_0000000000000000000001", 91)
	second := storedMedia(41, "m_0000000000000000000002", 92)
	mock.ExpectQuery("SELECT "+mediaColumns+" FROM media WHERE id IN (?, ?) AND state = 'active'").
		WithArgs(int64(41), int64(31)).
		WillReturnRows(mediaRows(second, first))

	got, err := repository.FindActiveByIDs(context.Background(), []int64{41, 31})

	require.NoError(t, err)
	require.Equal(t, []Media{second, first}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryFindActiveByPublicKeysUsesOnlyBindMarkers(t *testing.T) {
	repository, mock, _ := newMediaRepositoryTest(t, 1)
	first := storedMedia(31, "m_0000000000000000000001", 91)
	second := storedMedia(41, "m_0000000000000000000002", 92)
	mock.ExpectQuery("SELECT "+mediaColumns+" FROM media WHERE public_key IN (?, ?) AND state = 'active'").
		WithArgs(second.PublicKey, first.PublicKey).
		WillReturnRows(mediaRows(first, second))

	got, err := repository.FindActiveByPublicKeys(context.Background(), []string{second.PublicKey, first.PublicKey})

	require.NoError(t, err)
	require.Equal(t, []Media{first, second}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryFindActiveByPublicKeyUsesExactQuery(t *testing.T) {
	repository, mock, _ := newMediaRepositoryTest(t, 1)
	want := storedMedia(31, "m_0000000000000000000001", 91)
	mock.ExpectQuery(findActiveByKeySQL).WithArgs(want.PublicKey).WillReturnRows(mediaRows(want))

	got, err := repository.FindActiveByPublicKey(context.Background(), want.PublicKey)

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryRejectsInvalidInputsBeforeSQL(t *testing.T) {
	repository, mock, _ := newMediaRepositoryTest(t, 1)
	invalidCreate := []NewMedia{
		{},
		{PublicKey: "91", GFSFileID: 91, OriginalName: "photo.png", MIMEType: "image/png", FileSize: 1, Width: 1, Height: 1},
		{PublicKey: "m_0000000000000000000001", GFSFileID: 0, OriginalName: "photo.png", MIMEType: "image/png", FileSize: 1, Width: 1, Height: 1},
		{PublicKey: "m_0000000000000000000001", GFSFileID: 91, OriginalName: "", MIMEType: "image/png", FileSize: 1, Width: 1, Height: 1},
		{PublicKey: "m_0000000000000000000001", GFSFileID: 91, OriginalName: "photo.png", MIMEType: "", FileSize: 1, Width: 1, Height: 1},
		{PublicKey: "m_0000000000000000000001", GFSFileID: 91, OriginalName: "photo.png", MIMEType: "image/png", FileSize: 0, Width: 1, Height: 1},
		{PublicKey: "m_0000000000000000000001", GFSFileID: 91, OriginalName: "photo.png", MIMEType: "image/png", FileSize: 1, Width: 0, Height: 1},
		{PublicKey: "m_0000000000000000000001", GFSFileID: 91, OriginalName: "photo.png", MIMEType: "image/png", FileSize: 1, Width: 1, Height: 0},
	}
	for _, input := range invalidCreate {
		_, err := repository.Create(context.Background(), input, time.Now())
		require.ErrorIs(t, err, ErrInvalidMetadata)
	}
	for _, id := range []int64{0, -1} {
		_, err := repository.FindByGFSFileID(context.Background(), id)
		require.ErrorIs(t, err, ErrInvalidMetadata)
		_, err = repository.FindActiveByID(context.Background(), id)
		require.ErrorIs(t, err, ErrInvalidMetadata)
	}
	for _, ids := range [][]int64{nil, {}, {0}, {-1}, {1, 0}} {
		_, err := repository.FindActiveByIDs(context.Background(), ids)
		require.ErrorIs(t, err, ErrInvalidMetadata)
	}
	for _, keys := range [][]string{nil, {}, {""}, {"91"}, {"m_0000000000000000000001", ""}} {
		_, err := repository.FindActiveByPublicKeys(context.Background(), keys)
		require.ErrorIs(t, err, ErrInvalidMetadata)
	}
	for _, key := range []string{"", "91", "m_short"} {
		_, err := repository.FindActiveByPublicKey(context.Background(), key)
		require.ErrorIs(t, err, ErrInvalidMetadata)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryRejectsNilDependenciesAndContextsWithoutPanic(t *testing.T) {
	valid, _, _ := newMediaRepositoryTest(t, 1)
	var nilRepository *MySQLRepository
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, test := range []struct {
		name string
		call func() error
	}{
		{name: "nil receiver", call: func() error { _, callErr := nilRepository.FindActiveByID(context.Background(), 1); return callErr }},
		{name: "nil database", call: func() error {
			_, callErr := NewMySQLRepository(nil, valid.ids).FindActiveByID(context.Background(), 1)
			return callErr
		}},
		{name: "nil generator", call: func() error {
			_, callErr := NewMySQLRepository(db, nil).FindActiveByID(context.Background(), 1)
			return callErr
		}},
		{name: "zero generator", call: func() error {
			_, callErr := NewMySQLRepository(db, &idgen.Generator{}).Create(context.Background(), validNewMedia(), time.Now())
			return callErr
		}},
		{name: "nil create context", call: func() error { _, callErr := valid.Create(nil, validNewMedia(), time.Now()); return callErr }},
		{name: "nil GFS context", call: func() error { _, callErr := valid.FindByGFSFileID(nil, 1); return callErr }},
		{name: "nil ID context", call: func() error { _, callErr := valid.FindActiveByID(nil, 1); return callErr }},
		{name: "nil IDs context", call: func() error { _, callErr := valid.FindActiveByIDs(nil, []int64{1}); return callErr }},
		{name: "nil keys context", call: func() error {
			_, callErr := valid.FindActiveByPublicKeys(nil, []string{"m_0000000000000000000001"})
			return callErr
		}},
		{name: "nil key context", call: func() error {
			_, callErr := valid.FindActiveByPublicKey(nil, "m_0000000000000000000001")
			return callErr
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var callErr error
			require.NotPanics(t, func() { callErr = test.call() })
			require.Error(t, callErr)
		})
	}
}

func TestMySQLRepositorySanitizesDependencyErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(sqlmock.Sqlmock)
		call  func(*MySQLRepository) error
	}{
		{name: "find GFS", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(findMediaByGFSSQL).WillReturnError(errors.New("gfs-query-secret"))
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.FindByGFSFileID(context.Background(), 91)
			return err
		}},
		{name: "find ID", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(findActiveByIDSQL).WillReturnError(errors.New("id-query-secret"))
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.FindActiveByID(context.Background(), 31)
			return err
		}},
		{name: "find IDs", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT " + mediaColumns + " FROM media WHERE id IN (?) AND state = 'active'").WillReturnError(errors.New("ids-query-secret"))
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.FindActiveByIDs(context.Background(), []int64{31})
			return err
		}},
		{name: "find keys", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT " + mediaColumns + " FROM media WHERE public_key IN (?) AND state = 'active'").WillReturnError(errors.New("keys-query-secret"))
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.FindActiveByPublicKeys(context.Background(), []string{"m_0000000000000000000001"})
			return err
		}},
		{name: "find key", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(findActiveByKeySQL).WillReturnError(errors.New("key-query-secret"))
		}, call: func(repository *MySQLRepository) error {
			_, err := repository.FindActiveByPublicKey(context.Background(), "m_0000000000000000000001")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newMediaRepositoryTest(t, 1)
			test.setup(mock)

			err := test.call(repository)

			require.ErrorIs(t, err, ErrDependencyUnavailable)
			require.NotContains(t, err.Error(), "secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

type mediaCounter struct {
	next int64
	keys []string
}

func (c *mediaCounter) Increment(_ context.Context, key string) (int64, error) {
	c.keys = append(c.keys, key)
	return c.next, nil
}

func (*mediaCounter) Raise(context.Context, string, int64) (int64, error) {
	return 0, errors.New("unexpected counter raise")
}

func newMediaRepositoryTest(t *testing.T, rawID int64) (*MySQLRepository, sqlmock.Sqlmock, *mediaCounter) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	counter := &mediaCounter{next: rawID}
	ids, err := idgen.New(counter, nil, 2, 3, false)
	require.NoError(t, err)
	return NewMySQLRepository(db, ids), mock, counter
}

func validNewMedia() NewMedia {
	return NewMedia{
		PublicKey: "m_0000000000000000000001", GFSFileID: 91, OriginalName: "photo.png",
		MIMEType: "image/png", FileSize: 2048, Width: 640, Height: 480,
	}
}

func storedMedia(id int64, publicKey string, gfsFileID int64) Media {
	createdAt := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	return Media{
		ID: id, PublicKey: publicKey, GFSFileID: gfsFileID, OriginalName: "photo.png",
		MIMEType: "image/png", FileSize: 2048, Width: 640, Height: 480,
		State: "active", CreatedAt: createdAt, UpdatedAt: createdAt.Add(time.Hour),
	}
}

func mediaRows(items ...Media) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "public_key", "gfs_file_id", "original_name", "mime_type", "file_size", "width", "height", "state", "created_at", "updated_at"})
	for _, item := range items {
		rows.AddRow(item.ID, item.PublicKey, item.GFSFileID, item.OriginalName, item.MIMEType, item.FileSize, item.Width, item.Height, item.State, item.CreatedAt, item.UpdatedAt)
	}
	return rows
}
