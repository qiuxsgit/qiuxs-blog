package revision

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/dbtable"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

const (
	storedEditingDraftSelect      = "SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE article_id = ? AND status = 'editing'"
	storedEditingDraftAtSelect    = "SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE id = ? AND article_id = ? AND status = 'editing'"
	storedDraftTagsSelect         = "SELECT tag_id, tag_name, tag_slug, position FROM article_revision_tags WHERE revision_id = ? ORDER BY position ASC"
	storedDraftMediaSelect        = "SELECT arm.media_id, m.public_key, arm.purpose, arm.position FROM article_revision_media arm JOIN media m ON m.id = arm.media_id WHERE arm.revision_id = ? ORDER BY arm.position ASC"
	draftUpdateStatement          = "UPDATE article_revisions SET title = ?, summary = ?, cover_media_id = ?, content_md = ?, content_hash = ?, lock_version = lock_version + 1, updated_at = ? WHERE article_id = ? AND status = 'editing' AND lock_version = ?"
	savedDraftIdentitySelect      = "SELECT id, lock_version, revision_no, created_at FROM article_revisions WHERE article_id = ? AND status = 'editing'"
	draftTagsDeleteStatement      = "DELETE FROM article_revision_tags WHERE revision_id = ?"
	draftTagInsertStatement       = "INSERT INTO article_revision_tags (id, revision_id, tag_id, tag_name, tag_slug, position, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
	draftMediaDeleteStatement     = "DELETE FROM article_revision_media WHERE revision_id = ?"
	draftMediaInsertStatement     = "INSERT INTO article_revision_media (id, revision_id, media_id, purpose, position, created_at) VALUES (?, ?, ?, ?, ?, ?)"
	activeArticleTouchStatement   = "UPDATE articles SET updated_at = ? WHERE id = ? AND state = 'active'"
	articleStateForUpdateSelect   = "SELECT state FROM articles WHERE id = ? FOR UPDATE"
	currentDraftForUpdateSelect   = "SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE id = ? AND article_id = ? AND status = 'editing' FOR UPDATE"
	manualVersionFreezeStatement  = "UPDATE article_revisions SET status = 'frozen', reason = 'manual_version', updated_at = ? WHERE id = ? AND status = 'editing' AND lock_version = ?"
	editingVersionInsertStatement = "INSERT INTO article_revisions (id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at) VALUES (?, ?, ?, 'editing', 'draft', ?, ?, ?, ?, ?, 1, ?, ?)"
	draftPointerReplaceStatement  = "UPDATE articles SET draft_revision_id = ?, updated_at = ? WHERE id = ? AND draft_revision_id = ? AND state = 'active'"
	articlePointerForUpdateSelect = "SELECT state, draft_revision_id FROM articles WHERE id = ? FOR UPDATE"
	frozenVersionSelect           = "SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE id = ? AND article_id = ? AND status = 'frozen'"
	frozenVersionsListSelect      = "SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE article_id = ? AND status = 'frozen' ORDER BY revision_no DESC"
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
	return r.getDraft(ctx, storedEditingDraftSelect, []any{articleID}, ErrNotFound, "get article draft")
}

func (r *MySQLRepository) GetDraftAt(ctx context.Context, articleID, revisionID int64) (Draft, error) {
	if err := r.validate(ctx); err != nil {
		return Draft{}, err
	}
	if articleID <= 0 || revisionID <= 0 {
		return Draft{}, ErrInvalidContent
	}
	return r.getDraft(ctx, storedEditingDraftAtSelect, []any{revisionID, articleID}, ErrConflict, "get article draft at pointer")
}

func (r *MySQLRepository) getDraft(ctx context.Context, statement string, arguments []any, missingError error, operation string) (Draft, error) {
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

	draft, err := scanStoredRevision(tx.QueryRowContext(ctx, statement, arguments...), StatusEditing, missingError, operation)
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

	current, err := lockActiveEditingDraft(ctx, tx, articleID, 0, "lock current draft for save")
	if err != nil {
		return Draft{}, err
	}
	if current.LockVersion != lockVersion {
		return Draft{}, ErrConflict
	}

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
	if draftID != current.ID || savedLock != lockVersion+1 || revisionNo != current.RevisionNo || !createdAt.Equal(current.CreatedAt) {
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

func (r *MySQLRepository) CreateVersion(ctx context.Context, articleID, currentRevisionID, lockVersion int64, at time.Time) (Version, Draft, error) {
	if err := r.validate(ctx); err != nil {
		return Version{}, Draft{}, err
	}
	if articleID <= 0 || currentRevisionID <= 0 || lockVersion <= 0 {
		return Version{}, Draft{}, ErrInvalidContent
	}
	at = at.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Version{}, Draft{}, revisionSafeWrap("begin manual version", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, err := lockActiveEditingDraft(ctx, tx, articleID, currentRevisionID, "lock current draft for version")
	if err != nil {
		return Version{}, Draft{}, err
	}
	if current.LockVersion != lockVersion {
		return Version{}, Draft{}, ErrConflict
	}
	if err := r.loadAssociations(ctx, tx, &current); err != nil {
		return Version{}, Draft{}, err
	}
	result, err := tx.ExecContext(ctx, manualVersionFreezeStatement, at, current.ID, lockVersion)
	if err != nil {
		return Version{}, Draft{}, revisionSafeWrap("freeze current manual version", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Version{}, Draft{}, revisionSafeWrap("freeze current manual version", err)
	}
	if rowsAffected == 0 {
		return Version{}, Draft{}, ErrConflict
	}
	if rowsAffected != 1 {
		return Version{}, Draft{}, revisionSafeWrap("freeze current manual version", errors.New("unexpected affected row count"))
	}

	next, err := r.insertEditingCopy(ctx, tx, current, current.RevisionNo+1, at)
	if err != nil {
		return Version{}, Draft{}, err
	}
	if err := replaceDraftPointer(ctx, tx, articleID, current.ID, next.ID, at); err != nil {
		return Version{}, Draft{}, err
	}
	if err := tx.Commit(); err != nil {
		return Version{}, Draft{}, revisionSafeWrap("commit manual version", err)
	}
	committed = true

	frozen := cloneDraft(current)
	frozen.Status = StatusFrozen
	frozen.Reason = ReasonManualVersion
	frozen.UpdatedAt = at
	return Version{Draft: frozen}, next, nil
}

func (r *MySQLRepository) ListVersions(ctx context.Context, articleID int64) ([]Version, error) {
	if err := r.validate(ctx); err != nil {
		return nil, err
	}
	if articleID <= 0 {
		return nil, ErrInvalidContent
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, revisionSafeWrap("begin version history read", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, frozenVersionsListSelect, articleID)
	if err != nil {
		return nil, revisionSafeWrap("list frozen versions", err)
	}
	versions := make([]Version, 0)
	for rows.Next() {
		stored, scanErr := scanStoredRevision(rows, StatusFrozen, ErrNotFound, "scan frozen version")
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		versions = append(versions, Version{Draft: stored})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, revisionSafeWrap("list frozen versions", err)
	}
	if err := rows.Close(); err != nil {
		return nil, revisionSafeWrap("list frozen versions", err)
	}
	for index := range versions {
		if err := r.loadAssociations(ctx, tx, &versions[index].Draft); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, revisionSafeWrap("commit version history read", err)
	}
	committed = true
	return versions, nil
}

func (r *MySQLRepository) RestoreVersion(ctx context.Context, articleID, revisionID, currentRevisionID, lockVersion int64, at time.Time) (Draft, error) {
	if err := r.validate(ctx); err != nil {
		return Draft{}, err
	}
	if articleID <= 0 || revisionID <= 0 || currentRevisionID <= 0 || lockVersion <= 0 {
		return Draft{}, ErrInvalidContent
	}
	at = at.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Draft{}, revisionSafeWrap("begin version restore", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, err := lockActiveEditingDraft(ctx, tx, articleID, currentRevisionID, "lock current draft for restore")
	if err != nil {
		return Draft{}, err
	}
	if current.LockVersion != lockVersion {
		return Draft{}, ErrConflict
	}
	target, err := scanStoredRevision(
		tx.QueryRowContext(ctx, frozenVersionSelect, revisionID, articleID),
		StatusFrozen, ErrNotFrozen, "read frozen restore target",
	)
	if err != nil {
		return Draft{}, err
	}
	if err := r.loadAssociations(ctx, tx, &target); err != nil {
		return Draft{}, err
	}
	result, err := tx.ExecContext(ctx, manualVersionFreezeStatement, at, current.ID, lockVersion)
	if err != nil {
		return Draft{}, revisionSafeWrap("freeze current draft before restore", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Draft{}, revisionSafeWrap("freeze current draft before restore", err)
	}
	if rowsAffected == 0 {
		return Draft{}, ErrConflict
	}
	if rowsAffected != 1 {
		return Draft{}, revisionSafeWrap("freeze current draft before restore", errors.New("unexpected affected row count"))
	}

	restored, err := r.insertEditingCopy(ctx, tx, target, current.RevisionNo+1, at)
	if err != nil {
		return Draft{}, err
	}
	if err := replaceDraftPointer(ctx, tx, articleID, current.ID, restored.ID, at); err != nil {
		return Draft{}, err
	}
	if err := tx.Commit(); err != nil {
		return Draft{}, revisionSafeWrap("commit version restore", err)
	}
	committed = true
	return restored, nil
}

func (r *MySQLRepository) loadAssociations(ctx context.Context, queryer revisionQueryer, stored *Draft) error {
	var err error
	stored.Tags, err = r.loadTags(ctx, queryer, stored.ID)
	if err != nil {
		return err
	}
	stored.Media, err = r.loadMedia(ctx, queryer, stored.ID)
	if err != nil {
		return err
	}
	if stored.CoverMediaID == nil {
		if len(stored.Media) > 0 && stored.Media[0].Purpose == "cover" {
			return invalidStoredAssociations()
		}
		return nil
	}
	if *stored.CoverMediaID <= 0 || len(stored.Media) == 0 || stored.Media[0].Purpose != "cover" || stored.Media[0].MediaID != *stored.CoverMediaID {
		return invalidStoredAssociations()
	}
	return nil
}

func lockActiveEditingDraft(ctx context.Context, tx *sql.Tx, articleID, expectedRevisionID int64, operation string) (Draft, error) {
	var state string
	var currentID sql.NullInt64
	if err := tx.QueryRowContext(ctx, articlePointerForUpdateSelect, articleID).Scan(&state, &currentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Draft{}, ErrArticleInactive
		}
		return Draft{}, revisionSafeWrap(operation, err)
	}
	if state != "active" {
		return Draft{}, ErrArticleInactive
	}
	if !currentID.Valid || currentID.Int64 <= 0 || expectedRevisionID > 0 && currentID.Int64 != expectedRevisionID {
		return Draft{}, ErrConflict
	}
	return scanStoredRevision(
		tx.QueryRowContext(ctx, currentDraftForUpdateSelect, currentID.Int64, articleID),
		StatusEditing, ErrConflict, operation,
	)
}

func (r *MySQLRepository) insertEditingCopy(ctx context.Context, tx *sql.Tx, source Draft, revisionNo int64, at time.Time) (Draft, error) {
	var revisionID int64
	if err := r.ids.Insert(ctx, dbtable.ArticleRevisions, func(id int64) error {
		revisionID = id
		_, insertErr := tx.ExecContext(ctx, editingVersionInsertStatement,
			id, source.ArticleID, revisionNo, source.Title, source.Summary, nullableRevisionID(source.CoverMediaID),
			source.ContentMD, source.ContentHash, at, at,
		)
		return insertErr
	}); err != nil {
		return Draft{}, revisionSafeWrap("insert new editing revision", err)
	}
	if err := r.copyAssociations(ctx, tx, revisionID, source.Tags, source.Media, at); err != nil {
		return Draft{}, err
	}
	next := cloneDraft(source)
	next.ID = revisionID
	next.RevisionNo = revisionNo
	next.LockVersion = 1
	next.Status = StatusEditing
	next.Reason = ReasonDraft
	next.CreatedAt = at
	next.UpdatedAt = at
	return next, nil
}

func (r *MySQLRepository) copyAssociations(ctx context.Context, tx *sql.Tx, revisionID int64, tags []tag.Snapshot, references []media.Reference, at time.Time) error {
	for _, snapshot := range tags {
		item := snapshot
		if err := r.ids.Insert(ctx, dbtable.ArticleRevisionTags, func(id int64) error {
			_, insertErr := tx.ExecContext(ctx, draftTagInsertStatement,
				id, revisionID, item.TagID, item.Name, item.Slug, item.Position, at,
			)
			return insertErr
		}); err != nil {
			return revisionSafeWrap("copy revision tags", err)
		}
	}
	for _, reference := range references {
		item := reference
		if err := r.ids.Insert(ctx, dbtable.ArticleRevisionMedia, func(id int64) error {
			_, insertErr := tx.ExecContext(ctx, draftMediaInsertStatement,
				id, revisionID, item.MediaID, item.Purpose, item.Position, at,
			)
			return insertErr
		}); err != nil {
			return revisionSafeWrap("copy revision media", err)
		}
	}
	return nil
}

func replaceDraftPointer(ctx context.Context, tx *sql.Tx, articleID, oldRevisionID, newRevisionID int64, at time.Time) error {
	result, err := tx.ExecContext(ctx, draftPointerReplaceStatement, newRevisionID, at, articleID, oldRevisionID)
	if err != nil {
		return revisionSafeWrap("replace active article draft pointer", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return revisionSafeWrap("replace active article draft pointer", err)
	}
	if rowsAffected == 0 {
		var state string
		var currentDraftID sql.NullInt64
		if err := tx.QueryRowContext(ctx, articlePointerForUpdateSelect, articleID).Scan(&state, &currentDraftID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrArticleInactive
			}
			return revisionSafeWrap("verify active article draft pointer", err)
		}
		if state != "active" {
			return ErrArticleInactive
		}
		if !currentDraftID.Valid || currentDraftID.Int64 != oldRevisionID {
			return ErrConflict
		}
		return revisionSafeWrap("verify active article draft pointer", errors.New("zero affected rows for expected active draft pointer"))
	}
	if rowsAffected != 1 {
		return revisionSafeWrap("replace active article draft pointer", errors.New("unexpected affected row count"))
	}
	return nil
}

func (r *MySQLRepository) loadTags(ctx context.Context, queryer revisionQueryer, revisionID int64) ([]tag.Snapshot, error) {
	rows, err := queryer.QueryContext(ctx, storedDraftTagsSelect, revisionID)
	if err != nil {
		return nil, revisionSafeWrap("load draft tag snapshots", err)
	}
	defer rows.Close()
	snapshots := make([]tag.Snapshot, 0)
	seenIDs := make(map[int64]struct{})
	for rows.Next() {
		var snapshot tag.Snapshot
		if err := rows.Scan(&snapshot.TagID, &snapshot.Name, &snapshot.Slug, &snapshot.Position); err != nil {
			return nil, revisionSafeWrap("load draft tag snapshots", err)
		}
		if len(snapshots) == MaxTagCount || snapshot.TagID <= 0 || strings.TrimSpace(snapshot.Name) == "" || strings.TrimSpace(snapshot.Slug) == "" || snapshot.Position != len(snapshots) {
			return nil, invalidStoredAssociations()
		}
		if _, exists := seenIDs[snapshot.TagID]; exists {
			return nil, invalidStoredAssociations()
		}
		seenIDs[snapshot.TagID] = struct{}{}
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
	seenContentIDs := make(map[int64]struct{})
	seenContentKeys := make(map[string]struct{})
	coverSeen := false
	contentCount := 0
	for rows.Next() {
		var reference media.Reference
		if err := rows.Scan(&reference.MediaID, &reference.PublicKey, &reference.Purpose, &reference.Position); err != nil {
			return nil, revisionSafeWrap("load draft media references", err)
		}
		if reference.MediaID <= 0 || strings.TrimSpace(reference.PublicKey) == "" || reference.Position != len(references) {
			return nil, invalidStoredAssociations()
		}
		switch reference.Purpose {
		case "cover":
			if coverSeen || len(references) != 0 {
				return nil, invalidStoredAssociations()
			}
			coverSeen = true
		case "content":
			if contentCount == MaxBodyMediaCount {
				return nil, invalidStoredAssociations()
			}
			if _, exists := seenContentIDs[reference.MediaID]; exists {
				return nil, invalidStoredAssociations()
			}
			if _, exists := seenContentKeys[reference.PublicKey]; exists {
				return nil, invalidStoredAssociations()
			}
			seenContentIDs[reference.MediaID] = struct{}{}
			seenContentKeys[reference.PublicKey] = struct{}{}
			contentCount++
		default:
			return nil, invalidStoredAssociations()
		}
		if len(references) == MaxBodyMediaCount+1 {
			return nil, invalidStoredAssociations()
		}
		references = append(references, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, revisionSafeWrap("load draft media references", err)
	}
	return references, nil
}

func invalidStoredAssociations() error {
	return revisionSafeWrap("validate stored revision associations", errors.New("stored revision associations are invalid"))
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
	return scanStoredRevision(scanner, StatusEditing, ErrNotFound, operation)
}

func scanStoredRevision(scanner revisionScanner, expectedStatus Status, missingError error, operation string) (Draft, error) {
	var draft Draft
	var cover sql.NullInt64
	if err := scanner.Scan(
		&draft.ID, &draft.ArticleID, &draft.RevisionNo, &draft.Status, &draft.Reason,
		&draft.Title, &draft.Summary, &cover, &draft.ContentMD, &draft.ContentHash,
		&draft.LockVersion, &draft.CreatedAt, &draft.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Draft{}, missingError
		}
		return Draft{}, revisionSafeWrap(operation, err)
	}
	if cover.Valid {
		draft.CoverMediaID = revisionInt64(cover.Int64)
	}
	validReason := expectedStatus == StatusEditing && draft.Reason == ReasonDraft ||
		expectedStatus == StatusFrozen && (draft.Reason == ReasonManualVersion || draft.Reason == ReasonPublishSnapshot)
	if draft.ID <= 0 || draft.ArticleID <= 0 || draft.RevisionNo <= 0 || draft.LockVersion <= 0 || draft.Status != expectedStatus ||
		!validReason || !contentHashPattern.MatchString(draft.ContentHash) {
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

func nullableRevisionID(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
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

func cloneDraft(source Draft) Draft {
	cloned := source
	if source.CoverMediaID != nil {
		cloned.CoverMediaID = revisionInt64(*source.CoverMediaID)
	}
	cloned.Tags = make([]tag.Snapshot, len(source.Tags))
	copy(cloned.Tags, source.Tags)
	cloned.Media = make([]media.Reference, len(source.Media))
	copy(cloned.Media, source.Media)
	return cloned
}

var _ Repository = (*MySQLRepository)(nil)
