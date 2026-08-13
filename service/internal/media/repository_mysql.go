package media

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/dbtable"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
)

const storedMediaColumns = "id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at"

var (
	mediaPublicKeyUniquePattern = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?uk_media_public_key['`]")
	mediaGFSFileIDUniquePattern = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?uk_media_gfs_file_id['`]")
)

type MySQLRepository struct {
	db      *sql.DB
	ids     *idgen.Generator
	initErr error
}

func NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository {
	repository := &MySQLRepository{db: db, ids: ids}
	if db == nil {
		repository.initErr = errors.New("media database is required")
	} else if ids == nil {
		repository.initErr = errors.New("media ID generator is required")
	}
	return repository
}

func (r *MySQLRepository) Create(ctx context.Context, input NewMedia, at time.Time) (Media, error) {
	if err := r.validate(ctx); err != nil {
		return Media{}, err
	}
	if !mediaPublicKeyPattern.MatchString(input.PublicKey) || validateMetadata(input.GFSFileID, input.OriginalName, Metadata{
		FileID: input.GFSFileID, FileName: input.OriginalName, ContentType: input.MIMEType,
		FileSize: input.FileSize, Width: input.Width, Height: input.Height,
	}) != nil {
		return Media{}, ErrInvalidMetadata
	}
	at = at.UTC()
	created := Media{
		PublicKey: input.PublicKey, GFSFileID: input.GFSFileID, OriginalName: input.OriginalName,
		MIMEType: input.MIMEType, FileSize: input.FileSize, Width: input.Width, Height: input.Height,
		State: "active", CreatedAt: at, UpdatedAt: at,
	}
	err := r.ids.Insert(ctx, dbtable.Media, func(id int64) error {
		created.ID = id
		_, insertErr := r.db.ExecContext(ctx,
			"INSERT INTO media (id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)",
			id, input.PublicKey, input.GFSFileID, input.OriginalName, input.MIMEType,
			input.FileSize, input.Width, input.Height, at, at,
		)
		return insertErr
	})
	if err != nil {
		switch {
		case isNamedMediaDuplicate(err, mediaPublicKeyUniquePattern):
			return Media{}, mediaDomainError("create media", ErrPublicKeyConflict, err)
		case isNamedMediaDuplicate(err, mediaGFSFileIDUniquePattern):
			return Media{}, mediaDomainError("create media", ErrGFSFileIDConflict, err)
		default:
			return Media{}, mediaDependencyError("create media", err)
		}
	}
	return created, nil
}

func (r *MySQLRepository) FindByGFSFileID(ctx context.Context, id int64) (Media, error) {
	if err := r.validate(ctx); err != nil {
		return Media{}, err
	}
	if id <= 0 {
		return Media{}, ErrInvalidMetadata
	}
	return scanStoredMedia(r.db.QueryRowContext(ctx, "SELECT "+storedMediaColumns+" FROM media WHERE gfs_file_id = ?", id), "find media by GFS file ID")
}

func (r *MySQLRepository) FindActiveByID(ctx context.Context, id int64) (Media, error) {
	if err := r.validate(ctx); err != nil {
		return Media{}, err
	}
	if id <= 0 {
		return Media{}, ErrInvalidMetadata
	}
	return scanStoredMedia(r.db.QueryRowContext(ctx, "SELECT "+storedMediaColumns+" FROM media WHERE id = ? AND state = 'active'", id), "find active media by ID")
}

func (r *MySQLRepository) FindActiveByIDs(ctx context.Context, ids []int64) ([]Media, error) {
	if err := r.validate(ctx); err != nil {
		return nil, err
	}
	arguments := make([]any, len(ids))
	markers := make([]string, len(ids))
	if len(ids) == 0 {
		return nil, ErrInvalidMetadata
	}
	for index, id := range ids {
		if id <= 0 {
			return nil, ErrInvalidMetadata
		}
		arguments[index] = id
		markers[index] = "?"
	}
	query := "SELECT " + storedMediaColumns + " FROM media WHERE id IN (" + strings.Join(markers, ", ") + ") AND state = 'active'"
	return r.queryMedia(ctx, query, arguments, "find active media by IDs")
}

func (r *MySQLRepository) FindActiveByPublicKeys(ctx context.Context, publicKeys []string) ([]Media, error) {
	if err := r.validate(ctx); err != nil {
		return nil, err
	}
	arguments := make([]any, len(publicKeys))
	markers := make([]string, len(publicKeys))
	if len(publicKeys) == 0 {
		return nil, ErrInvalidMetadata
	}
	for index, publicKey := range publicKeys {
		if !mediaPublicKeyPattern.MatchString(publicKey) {
			return nil, ErrInvalidMetadata
		}
		arguments[index] = publicKey
		markers[index] = "?"
	}
	query := "SELECT " + storedMediaColumns + " FROM media WHERE public_key IN (" + strings.Join(markers, ", ") + ") AND state = 'active'"
	return r.queryMedia(ctx, query, arguments, "find active media by public keys")
}

func (r *MySQLRepository) FindActiveByPublicKey(ctx context.Context, publicKey string) (Media, error) {
	if err := r.validate(ctx); err != nil {
		return Media{}, err
	}
	if !mediaPublicKeyPattern.MatchString(publicKey) {
		return Media{}, ErrInvalidMetadata
	}
	return scanStoredMedia(r.db.QueryRowContext(ctx, "SELECT "+storedMediaColumns+" FROM media WHERE public_key = ? AND state = 'active'", publicKey), "find active media by public key")
}

func (r *MySQLRepository) queryMedia(ctx context.Context, query string, arguments []any, operation string) ([]Media, error) {
	rows, err := r.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, mediaDependencyError(operation, err)
	}
	defer rows.Close()
	items := make([]Media, 0)
	for rows.Next() {
		item, scanErr := scanMediaValues(rows.Scan)
		if scanErr != nil {
			return nil, mediaDependencyError(operation, scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, mediaDependencyError(operation, err)
	}
	return items, nil
}

func (r *MySQLRepository) validate(ctx context.Context) error {
	if r == nil {
		return errors.New("media repository is required")
	}
	if r.initErr != nil {
		return r.initErr
	}
	if r.db == nil || r.ids == nil {
		return errors.New("media repository is not configured")
	}
	if nilMediaDependency(ctx) {
		return ErrDependencyUnavailable
	}
	return nil
}

func scanStoredMedia(row *sql.Row, operation string) (Media, error) {
	item, err := scanMediaValues(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Media{}, mediaDomainError(operation, ErrNotFound, err)
		}
		return Media{}, mediaDependencyError(operation, err)
	}
	return item, nil
}

func scanMediaValues(scan func(...any) error) (Media, error) {
	var item Media
	err := scan(
		&item.ID, &item.PublicKey, &item.GFSFileID, &item.OriginalName, &item.MIMEType,
		&item.FileSize, &item.Width, &item.Height, &item.State, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func isNamedMediaDuplicate(err error, pattern *regexp.Regexp) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && pattern.MatchString(mysqlErr.Message)
}

var _ Repository = (*MySQLRepository)(nil)
