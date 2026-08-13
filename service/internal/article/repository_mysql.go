package article

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/dbtable"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
)

const (
	articleInsertStatement         = "INSERT INTO articles (id, slug, draft_revision_id, published_revision_id, state, created_at, updated_at) VALUES (?, ?, NULL, NULL, 'active', ?, ?)"
	initialRevisionInsertStatement = "INSERT INTO article_revisions (id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at) VALUES (?, ?, 1, 'editing', 'draft', '', '', NULL, '', ?, 1, ?, ?)"
	draftPointerUpdateStatement    = "UPDATE articles SET draft_revision_id = ?, updated_at = ? WHERE id = ? AND draft_revision_id IS NULL"
	articleSelectStatement         = "SELECT id, slug, draft_revision_id, published_revision_id, state, created_at, updated_at FROM articles WHERE id = ?"
	articleListStatement           = "SELECT a.id, a.slug, a.draft_revision_id, a.published_revision_id, a.state, a.created_at, a.updated_at, r.title, r.updated_at FROM articles a JOIN article_revisions r ON r.id = a.draft_revision_id WHERE a.state = ? ORDER BY r.updated_at DESC, a.id ASC"
	articleTrashStatement          = "UPDATE articles SET state = 'trashed', updated_at = ? WHERE id = ? AND state = 'active' AND published_revision_id IS NULL"
	articleUntrashStatement        = "UPDATE articles SET state = 'active', updated_at = ? WHERE id = ? AND state = 'trashed'"
)

var (
	articleSlugPattern       = regexp.MustCompile(`^[a-z0-9_-]{12}$`)
	articleSlugUniquePattern = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?uk_articles_slug['`]")
)

type MySQLRepository struct {
	db      *sql.DB
	ids     *idgen.Generator
	initErr error
}

func NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository {
	repository := &MySQLRepository{db: db, ids: ids}
	if db == nil {
		repository.initErr = errors.New("article database is required")
	} else if ids == nil {
		repository.initErr = errors.New("article ID generator is required")
	}
	return repository
}

func (r *MySQLRepository) Create(ctx context.Context, slug string, at time.Time) (Article, error) {
	if err := r.validate(ctx); err != nil {
		return Article{}, err
	}
	if !articleSlugPattern.MatchString(slug) {
		return Article{}, ErrSlugConflict
	}
	at = at.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Article{}, articleSafeWrap("begin article creation", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	created := Article{Slug: slug, State: StateActive, CreatedAt: at, UpdatedAt: at}
	if err := r.ids.Insert(ctx, dbtable.Articles, func(id int64) error {
		created.ID = id
		_, insertErr := tx.ExecContext(ctx, articleInsertStatement, id, slug, at, at)
		return insertErr
	}); err != nil {
		if isNamedArticleSlugDuplicate(err) {
			return Article{}, articleSafeJoin("create article", ErrSlugConflict, err)
		}
		return Article{}, articleSafeWrap("create article", err)
	}

	emptyHash := revision.ComputeHash(revision.PreparedContent{})
	if err := r.ids.Insert(ctx, dbtable.ArticleRevisions, func(id int64) error {
		created.DraftRevisionID = id
		_, insertErr := tx.ExecContext(ctx, initialRevisionInsertStatement, id, created.ID, emptyHash, at, at)
		return insertErr
	}); err != nil {
		return Article{}, articleSafeWrap("create initial article revision", err)
	}

	result, err := tx.ExecContext(ctx, draftPointerUpdateStatement, created.DraftRevisionID, at, created.ID)
	if err != nil {
		return Article{}, articleSafeWrap("set initial article draft", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Article{}, articleSafeWrap("set initial article draft", err)
	}
	if rowsAffected != 1 {
		return Article{}, articleSafeWrap("set initial article draft", errors.New("conditional draft pointer update failed"))
	}
	if err := tx.Commit(); err != nil {
		return Article{}, articleSafeWrap("commit article creation", err)
	}
	committed = true
	return created, nil
}

func (r *MySQLRepository) FindByID(ctx context.Context, id int64) (Article, error) {
	if err := r.validate(ctx); err != nil {
		return Article{}, err
	}
	if id <= 0 {
		return Article{}, ErrStateConflict
	}
	return scanArticle(r.db.QueryRowContext(ctx, articleSelectStatement, id), "find article")
}

func (r *MySQLRepository) List(ctx context.Context, state State) ([]Summary, error) {
	if err := r.validate(ctx); err != nil {
		return nil, err
	}
	if !validState(state) {
		return nil, ErrStateConflict
	}
	rows, err := r.db.QueryContext(ctx, articleListStatement, state)
	if err != nil {
		return nil, articleSafeWrap("list articles", err)
	}
	defer rows.Close()

	items := make([]Summary, 0)
	for rows.Next() {
		var item Summary
		var published sql.NullInt64
		if err := rows.Scan(
			&item.ID,
			&item.Slug,
			&item.DraftRevisionID,
			&published,
			&item.State,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.DraftTitle,
			&item.DraftUpdatedAt,
		); err != nil {
			return nil, articleSafeWrap("list articles", err)
		}
		if published.Valid {
			item.PublishedRevisionID = articleInt64(published.Int64)
		}
		if !validState(item.State) {
			return nil, articleSafeWrap("list articles", errors.New("stored article state is invalid"))
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, articleSafeWrap("list articles", err)
	}
	return items, nil
}

func (r *MySQLRepository) SetState(ctx context.Context, id int64, from, to State, at time.Time) error {
	if err := r.validate(ctx); err != nil {
		return err
	}
	if id <= 0 {
		return ErrStateConflict
	}
	var statement string
	switch {
	case from == StateActive && to == StateTrashed:
		statement = articleTrashStatement
	case from == StateTrashed && to == StateActive:
		statement = articleUntrashStatement
	default:
		return ErrStateConflict
	}
	result, err := r.db.ExecContext(ctx, statement, at.UTC(), id)
	if err != nil {
		return articleSafeWrap("update article state", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return articleSafeWrap("update article state", err)
	}
	if rowsAffected == 1 {
		return nil
	}
	if rowsAffected != 0 {
		return articleSafeWrap("update article state", errors.New("unexpected affected row count"))
	}

	current, err := scanArticle(r.db.QueryRowContext(ctx, articleSelectStatement, id), "reload article state")
	if err != nil {
		return err
	}
	if from == StateActive && to == StateTrashed && current.PublishedRevisionID != nil {
		return ErrMustBeUnpublished
	}
	return ErrStateConflict
}

func (r *MySQLRepository) validate(ctx context.Context) error {
	if r == nil {
		return errors.New("article repository is required")
	}
	if r.initErr != nil {
		return r.initErr
	}
	if r.db == nil || r.ids == nil {
		return errors.New("article repository is not configured")
	}
	if nilArticleInterface(ctx) {
		return ErrStateConflict
	}
	return nil
}

type articleScanner interface {
	Scan(...any) error
}

func scanArticle(scanner articleScanner, operation string) (Article, error) {
	var item Article
	var published sql.NullInt64
	if err := scanner.Scan(
		&item.ID,
		&item.Slug,
		&item.DraftRevisionID,
		&published,
		&item.State,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Article{}, ErrNotFound
		}
		return Article{}, articleSafeWrap(operation, err)
	}
	if published.Valid {
		item.PublishedRevisionID = articleInt64(published.Int64)
	}
	if !validState(item.State) {
		return Article{}, articleSafeWrap(operation, errors.New("stored article state is invalid"))
	}
	return item, nil
}

func articleInt64(value int64) *int64 {
	return &value
}

func isNamedArticleSlugDuplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && articleSlugUniquePattern.MatchString(mysqlErr.Message)
}

type articleSanitizedJoinedError struct {
	operation string
	domain    error
	cause     error
}

func (e *articleSanitizedJoinedError) Error() string   { return e.operation + " failed" }
func (e *articleSanitizedJoinedError) Unwrap() []error { return []error{e.domain, e.cause} }

func articleSafeJoin(operation string, domain, cause error) error {
	return &articleSanitizedJoinedError{operation: operation, domain: domain, cause: cause}
}

var _ Repository = (*MySQLRepository)(nil)
