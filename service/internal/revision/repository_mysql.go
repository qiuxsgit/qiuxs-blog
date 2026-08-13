package revision

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/dbtable"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

const (
	storedEditingDraftSelect    = "SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE article_id = ? AND status = 'editing'"
	storedDraftTagsSelect       = "SELECT tag_id, tag_name, tag_slug, position FROM article_revision_tags WHERE revision_id = ? ORDER BY position ASC"
	storedDraftMediaSelect      = "SELECT arm.media_id, m.public_key, arm.purpose, arm.position FROM article_revision_media arm JOIN media m ON m.id = arm.media_id WHERE arm.revision_id = ? ORDER BY arm.position ASC"
	draftUpdateStatement        = "UPDATE article_revisions SET title = ?, summary = ?, cover_media_id = ?, content_md = ?, content_hash = ?, lock_version = lock_version + 1, updated_at = ? WHERE article_id = ? AND status = 'editing' AND lock_version = ?"
	savedDraftIdentitySelect    = "SELECT id, lock_version, revision_no, created_at FROM article_revisions WHERE article_id = ? AND status = 'editing'"
	draftTagsDeleteStatement    = "DELETE FROM article_revision_tags WHERE revision_id = ?"
	draftTagInsertStatement     = "INSERT INTO article_revision_tags (id, revision_id, tag_id, tag_name, tag_slug, position, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
	draftMediaDeleteStatement   = "DELETE FROM article_revision_media WHERE revision_id = ?"
	draftMediaInsertStatement   = "INSERT INTO article_revision_media (id, revision_id, media_id, purpose, position, created_at) VALUES (?, ?, ?, ?, ?, ?)"
	activeArticleTouchStatement = "UPDATE articles SET updated_at = ? WHERE id = ? AND state = 'active'"
	articleStateForUpdateSelect = "SELECT state FROM articles WHERE id = ? FOR UPDATE"
)

var contentHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type MySQLRepository struct {
	db      *sql.DB
	ids     *idgen.Generator
	initErr error
}

func NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository {
	repository := &MySQLRepository{db: db, ids: ids}
	if db == nil {
		repository.initErr = errors.New("revision database is required")
	} else if ids == nil {
		repository.initErr = errors.New("revision ID generator is required")
	}
	return repository
}

func (r *MySQLRepository) GetDraft(ctx context.Context, articleID int64) (Draft, error) {
	if err := r.validate(ctx); err != nil {
		return Draft{}, err
	}
	if articleID <= 0 {
		return Draft{}, ErrInvalidContent
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return Draft{}, revisionSafeWrap("begin draft read", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	draft, err := scanDraft(tx.QueryRowContext(ctx, storedEditingDraftSelect, articleID), "get article draft")
	if err != nil {
		return Draft{}, err
	}
	draft.Tags, err = r.loadTags(ctx, tx, draft.ID)
	if err != nil {
		return Draft{}, err
	}
	draft.Media, err = r.loadMedia(ctx, tx, draft.ID)
	if err != nil {
		return Draft{}, err
	}
	if err := tx.Commit(); err != nil {
		return Draft{}, revisionSafeWrap("commit draft read", err)
	}
	committed = true
	return draft, nil
}

func (r *MySQLRepository) SaveDraft(ctx context.Context, articleID, lockVersion int64, content PreparedContent, at time.Time) (Draft, error) {
	if err := r.validate(ctx); err != nil {
		return Draft{}, err
	}
	if articleID <= 0 || lockVersion <= 0 || !validPreparedContent(content) {
		return Draft{}, ErrInvalidContent
	}
	at = at.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Draft{}, revisionSafeWrap("begin draft save", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, draftUpdateStatement,
		content.Title, content.Summary, preparedCoverID(content.Cover), content.ContentMD, content.ContentHash,
		at, articleID, lockVersion,
	)
	if err != nil {
		return Draft{}, revisionSafeWrap("update editing draft", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Draft{}, revisionSafeWrap("update editing draft", err)
	}
	if rowsAffected == 0 {
		return Draft{}, ErrConflict
	}
	if rowsAffected != 1 {
		return Draft{}, revisionSafeWrap("update editing draft", errors.New("unexpected affected row count"))
	}

	var draftID, savedLock, revisionNo int64
	var createdAt time.Time
	if err := tx.QueryRowContext(ctx, savedDraftIdentitySelect, articleID).Scan(&draftID, &savedLock, &revisionNo, &createdAt); err != nil {
		return Draft{}, revisionSafeWrap("read saved draft identity", err)
	}
	if savedLock != lockVersion+1 || draftID <= 0 || revisionNo <= 0 {
		return Draft{}, revisionSafeWrap("read saved draft identity", errors.New("saved draft identity mismatch"))
	}

	if _, err := tx.ExecContext(ctx, draftTagsDeleteStatement, draftID); err != nil {
		return Draft{}, revisionSafeWrap("replace draft tags", err)
	}
	for _, snapshot := range content.Tags {
		item := snapshot
		if err := r.ids.Insert(ctx, dbtable.ArticleRevisionTags, func(id int64) error {
			_, insertErr := tx.ExecContext(ctx, draftTagInsertStatement,
				id, draftID, item.TagID, item.Name, item.Slug, item.Position, at,
			)
			return insertErr
		}); err != nil {
			return Draft{}, revisionSafeWrap("replace draft tags", err)
		}
	}

	if _, err := tx.ExecContext(ctx, draftMediaDeleteStatement, draftID); err != nil {
		return Draft{}, revisionSafeWrap("replace draft media", err)
	}
	for _, reference := range content.Media {
		item := reference
		if err := r.ids.Insert(ctx, dbtable.ArticleRevisionMedia, func(id int64) error {
			_, insertErr := tx.ExecContext(ctx, draftMediaInsertStatement,
				id, draftID, item.MediaID, item.Purpose, item.Position, at,
			)
			return insertErr
		}); err != nil {
			return Draft{}, revisionSafeWrap("replace draft media", err)
		}
	}

	result, err = tx.ExecContext(ctx, activeArticleTouchStatement, at, articleID)
	if err != nil {
		return Draft{}, revisionSafeWrap("touch active article after draft save", err)
	}
	rowsAffected, err = result.RowsAffected()
	if err != nil {
		return Draft{}, revisionSafeWrap("touch active article after draft save", err)
	}
	if rowsAffected == 0 {
		var state string
		if err := tx.QueryRowContext(ctx, articleStateForUpdateSelect, articleID).Scan(&state); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Draft{}, ErrArticleInactive
			}
			return Draft{}, revisionSafeWrap("verify active article after draft save", err)
		}
		if state != "active" {
			return Draft{}, ErrArticleInactive
		}
	}
	if rowsAffected > 1 {
		return Draft{}, revisionSafeWrap("touch active article after draft save", errors.New("unexpected affected row count"))
	}
	if err := tx.Commit(); err != nil {
		return Draft{}, revisionSafeWrap("commit draft save", err)
	}
	committed = true

	return draftFromPrepared(articleID, draftID, revisionNo, savedLock, content, createdAt, at), nil
}

func (r *MySQLRepository) loadTags(ctx context.Context, queryer revisionQueryer, revisionID int64) ([]tag.Snapshot, error) {
	rows, err := queryer.QueryContext(ctx, storedDraftTagsSelect, revisionID)
	if err != nil {
		return nil, revisionSafeWrap("load draft tag snapshots", err)
	}
	defer rows.Close()
	snapshots := make([]tag.Snapshot, 0)
	for rows.Next() {
		var snapshot tag.Snapshot
		if err := rows.Scan(&snapshot.TagID, &snapshot.Name, &snapshot.Slug, &snapshot.Position); err != nil {
			return nil, revisionSafeWrap("load draft tag snapshots", err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, revisionSafeWrap("load draft tag snapshots", err)
	}
	return snapshots, nil
}

func (r *MySQLRepository) loadMedia(ctx context.Context, queryer revisionQueryer, revisionID int64) ([]media.Reference, error) {
	rows, err := queryer.QueryContext(ctx, storedDraftMediaSelect, revisionID)
	if err != nil {
		return nil, revisionSafeWrap("load draft media references", err)
	}
	defer rows.Close()
	references := make([]media.Reference, 0)
	for rows.Next() {
		var reference media.Reference
		if err := rows.Scan(&reference.MediaID, &reference.PublicKey, &reference.Purpose, &reference.Position); err != nil {
			return nil, revisionSafeWrap("load draft media references", err)
		}
		references = append(references, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, revisionSafeWrap("load draft media references", err)
	}
	return references, nil
}

func (r *MySQLRepository) validate(ctx context.Context) error {
	if r == nil {
		return errors.New("revision repository is required")
	}
	if r.initErr != nil {
		return r.initErr
	}
	if r.db == nil || r.ids == nil {
		return errors.New("revision repository is not configured")
	}
	if nilRevisionDependency(ctx) {
		return ErrInvalidContent
	}
	return nil
}

type revisionScanner interface {
	Scan(...any) error
}

type revisionQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func scanDraft(scanner revisionScanner, operation string) (Draft, error) {
	var draft Draft
	var cover sql.NullInt64
	if err := scanner.Scan(
		&draft.ID, &draft.ArticleID, &draft.RevisionNo, &draft.Status, &draft.Reason,
		&draft.Title, &draft.Summary, &cover, &draft.ContentMD, &draft.ContentHash,
		&draft.LockVersion, &draft.CreatedAt, &draft.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Draft{}, ErrNotFound
		}
		return Draft{}, revisionSafeWrap(operation, err)
	}
	if cover.Valid {
		draft.CoverMediaID = revisionInt64(cover.Int64)
	}
	if draft.ID <= 0 || draft.ArticleID <= 0 || draft.RevisionNo <= 0 || draft.LockVersion <= 0 || draft.Status != StatusEditing ||
		(draft.Reason != ReasonDraft && draft.Reason != ReasonManualVersion && draft.Reason != ReasonPublishSnapshot) || !contentHashPattern.MatchString(draft.ContentHash) {
		return Draft{}, revisionSafeWrap(operation, errors.New("stored draft is invalid"))
	}
	return draft, nil
}

func validPreparedContent(content PreparedContent) bool {
	if len(content.Tags) > MaxTagCount || len(content.Media) > MaxBodyMediaCount+1 {
		return false
	}
	if !contentHashPattern.MatchString(content.ContentHash) || ComputeHash(content) != content.ContentHash {
		return false
	}
	if _, err := ValidateDraft(Content{Title: content.Title, Summary: content.Summary, ContentMD: content.ContentMD}); err != nil {
		return false
	}
	for index, snapshot := range content.Tags {
		if snapshot.TagID <= 0 || snapshot.Name == "" || snapshot.Slug == "" || snapshot.Position != index {
			return false
		}
	}
	for index, reference := range content.Media {
		if reference.MediaID <= 0 || reference.PublicKey == "" || reference.Position != index || reference.Purpose != "cover" && reference.Purpose != "content" {
			return false
		}
	}
	if content.Cover != nil && content.Cover.ID <= 0 {
		return false
	}
	return true
}

func preparedCoverID(cover *media.Media) any {
	if cover == nil {
		return nil
	}
	return cover.ID
}

func draftFromPrepared(articleID, draftID, revisionNo, lockVersion int64, content PreparedContent, createdAt, updatedAt time.Time) Draft {
	var coverID *int64
	if content.Cover != nil {
		coverID = revisionInt64(content.Cover.ID)
	}
	tags := make([]tag.Snapshot, len(content.Tags))
	copy(tags, content.Tags)
	references := make([]media.Reference, len(content.Media))
	copy(references, content.Media)
	return Draft{
		ID: draftID, ArticleID: articleID, RevisionNo: revisionNo, LockVersion: lockVersion,
		Status: StatusEditing, Reason: ReasonDraft,
		Title: content.Title, Summary: content.Summary, CoverMediaID: coverID,
		ContentMD: content.ContentMD, ContentHash: content.ContentHash,
		Tags: tags, Media: references,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}

func revisionInt64(value int64) *int64 { return &value }

var _ Repository = (*MySQLRepository)(nil)
