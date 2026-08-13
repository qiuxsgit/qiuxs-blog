package tag

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

const tagColumns = "id, name, slug, created_at, updated_at"

var (
	tagNameUniquePattern = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?uk_tags_name['`]")
	tagSlugUniquePattern = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?uk_tags_slug['`]")
)

type MySQLRepository struct {
	db      *sql.DB
	ids     *idgen.Generator
	initErr error
}

func NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository {
	repository := &MySQLRepository{db: db, ids: ids}
	if db == nil {
		repository.initErr = errors.New("tag database is required")
	} else if ids == nil {
		repository.initErr = errors.New("tag ID generator is required")
	}
	return repository
}

func (r *MySQLRepository) Create(ctx context.Context, name, slug string, at time.Time) (Tag, error) {
	if err := r.validate(ctx); err != nil {
		return Tag{}, err
	}
	if name == "" {
		return Tag{}, ErrInvalidName
	}
	if slug == "" {
		return Tag{}, ErrInvalidSelection
	}
	at = at.UTC()
	created := Tag{Name: name, Slug: slug, CreatedAt: at, UpdatedAt: at}
	err := r.ids.Insert(ctx, dbtable.Tags, func(id int64) error {
		created.ID = id
		_, insertErr := r.db.ExecContext(
			ctx,
			"INSERT INTO tags (id, name, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
			id,
			name,
			slug,
			at,
			at,
		)
		return insertErr
	})
	if err != nil {
		switch {
		case isNamedDuplicate(err, tagNameUniquePattern):
			return Tag{}, sanitizedJoin("create tag", ErrNameConflict, err)
		case isNamedDuplicate(err, tagSlugUniquePattern):
			return Tag{}, sanitizedJoin("create tag", ErrSlugConflict, err)
		default:
			return Tag{}, safeWrap("create tag", err)
		}
	}
	return created, nil
}

func (r *MySQLRepository) List(ctx context.Context) ([]Tag, error) {
	if err := r.validate(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+tagColumns+" FROM tags ORDER BY name ASC, id ASC")
	if err != nil {
		return nil, safeWrap("list tags", err)
	}
	defer rows.Close()
	return scanTags(rows, "list tags")
}

func (r *MySQLRepository) FindByIDs(ctx context.Context, ids []int64) ([]Tag, error) {
	if err := r.validate(ctx); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, ErrInvalidSelection
	}
	arguments := make([]any, len(ids))
	markers := make([]string, len(ids))
	for index, id := range ids {
		if id <= 0 {
			return nil, ErrInvalidSelection
		}
		arguments[index] = id
		markers[index] = "?"
	}
	query := "SELECT " + tagColumns + " FROM tags WHERE id IN (" + strings.Join(markers, ", ") + ") ORDER BY id ASC"
	rows, err := r.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, safeWrap("find tags by IDs", err)
	}
	defer rows.Close()
	return scanTags(rows, "find tags by IDs")
}

func (r *MySQLRepository) Rename(ctx context.Context, id int64, name string, at time.Time) (Tag, error) {
	if err := r.validate(ctx); err != nil {
		return Tag{}, err
	}
	if id <= 0 {
		return Tag{}, ErrInvalidSelection
	}
	if name == "" {
		return Tag{}, ErrInvalidName
	}
	at = at.UTC()
	result, err := r.db.ExecContext(ctx, "UPDATE tags SET name = ?, updated_at = ? WHERE id = ?", name, at, id)
	if err != nil {
		if isNamedDuplicate(err, tagNameUniquePattern) {
			return Tag{}, sanitizedJoin("rename tag", ErrNameConflict, err)
		}
		return Tag{}, safeWrap("rename tag", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Tag{}, safeWrap("rename tag", err)
	}
	if rowsAffected == 0 {
		return Tag{}, ErrNotFound
	}
	return scanTag(r.db.QueryRowContext(ctx, "SELECT "+tagColumns+" FROM tags WHERE id = ?", id), "find renamed tag")
}

func (r *MySQLRepository) validate(ctx context.Context) error {
	if r == nil {
		return errors.New("tag repository is required")
	}
	if r.initErr != nil {
		return r.initErr
	}
	if r.db == nil || r.ids == nil {
		return errors.New("tag repository is not configured")
	}
	if nilInterface(ctx) {
		return ErrInvalidSelection
	}
	return nil
}

func scanTags(rows *sql.Rows, operation string) ([]Tag, error) {
	tags := make([]Tag, 0)
	for rows.Next() {
		var item Tag
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, safeWrap(operation, err)
		}
		tags = append(tags, item)
	}
	if err := rows.Err(); err != nil {
		return nil, safeWrap(operation, err)
	}
	return tags, nil
}

func scanTag(row *sql.Row, operation string) (Tag, error) {
	var item Tag
	if err := row.Scan(&item.ID, &item.Name, &item.Slug, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Tag{}, ErrNotFound
		}
		return Tag{}, safeWrap(operation, err)
	}
	return item, nil
}

func isNamedDuplicate(err error, pattern *regexp.Regexp) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && pattern.MatchString(mysqlErr.Message)
}

type sanitizedJoinedError struct {
	operation string
	domain    error
	cause     error
}

func (e *sanitizedJoinedError) Error() string   { return e.operation + " failed" }
func (e *sanitizedJoinedError) Unwrap() []error { return []error{e.domain, e.cause} }

func sanitizedJoin(operation string, domain, cause error) error {
	return &sanitizedJoinedError{operation: operation, domain: domain, cause: cause}
}

var _ Repository = (*MySQLRepository)(nil)
