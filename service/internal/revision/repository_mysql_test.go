package revision

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/stretchr/testify/require"
)

const (
	selectEditingDraftSQL     = `SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE article_id = ? AND status = 'editing'`
	selectDraftTagsSQL        = `SELECT tag_id, tag_name, tag_slug, position FROM article_revision_tags WHERE revision_id = ? ORDER BY position ASC`
	selectDraftMediaSQL       = `SELECT arm.media_id, m.public_key, arm.purpose, arm.position FROM article_revision_media arm JOIN media m ON m.id = arm.media_id WHERE arm.revision_id = ? ORDER BY arm.position ASC`
	updateEditingDraftSQL     = `UPDATE article_revisions SET title = ?, summary = ?, cover_media_id = ?, content_md = ?, content_hash = ?, lock_version = lock_version + 1, updated_at = ? WHERE article_id = ? AND status = 'editing' AND lock_version = ?`
	selectSavedIdentitySQL    = `SELECT id, lock_version, revision_no, created_at FROM article_revisions WHERE article_id = ? AND status = 'editing'`
	deleteDraftTagsSQL        = `DELETE FROM article_revision_tags WHERE revision_id = ?`
	insertDraftTagSQL         = `INSERT INTO article_revision_tags (id, revision_id, tag_id, tag_name, tag_slug, position, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	deleteDraftMediaSQL       = `DELETE FROM article_revision_media WHERE revision_id = ?`
	insertDraftMediaSQL       = `INSERT INTO article_revision_media (id, revision_id, media_id, purpose, position, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	touchActiveArticleSQL     = `UPDATE articles SET updated_at = ? WHERE id = ? AND state = 'active'`
	selectArticleStateSQL     = `SELECT state FROM articles WHERE id = ? FOR UPDATE`
	selectCurrentForUpdateSQL = `SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE id = ? AND article_id = ? AND status = 'editing' FOR UPDATE`
	freezeManualVersionSQL    = `UPDATE article_revisions SET status = 'frozen', reason = 'manual_version', updated_at = ? WHERE id = ? AND status = 'editing' AND lock_version = ?`
	insertEditingVersionSQL   = `INSERT INTO article_revisions (id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at) VALUES (?, ?, ?, 'editing', 'draft', ?, ?, ?, ?, ?, 1, ?, ?)`
	replaceDraftPointerSQL    = `UPDATE articles SET draft_revision_id = ?, updated_at = ? WHERE id = ? AND draft_revision_id = ? AND state = 'active'`
	selectArticlePointerSQL   = `SELECT state, draft_revision_id FROM articles WHERE id = ? FOR UPDATE`
	selectFrozenVersionSQL    = `SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE id = ? AND article_id = ? AND status = 'frozen'`
	listFrozenVersionsSQL     = `SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE article_id = ? AND status = 'frozen' ORDER BY revision_no DESC`
	testContentHash           = `5b732fcfb7289a73704164ad25aaae5be4b188172d1a47932428a8d1cdc7d2dc`
)

func TestMySQLRepositoryCreateVersionFreezesAndCopiesCurrentDraftAtomically(t *testing.T) {
	repository, mock, counter := newRevisionRepositoryTest(t, 2, 3)
	createdAt := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	at := time.Date(2026, 8, 14, 12, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	mock.ExpectBegin()
	expectCurrentDraftForUpdate(mock, 3, createdAt, updatedAt)
	expectStoredVersionAssociations(mock, 21)
	mock.ExpectExec(freezeManualVersionSQL).WithArgs(at.UTC(), int64(21), int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertEditingVersionSQL).WithArgs(
		int64(2), int64(11), int64(3), "Title", "Summary", int64(91), "body", testContentHash, at.UTC(), at.UTC(),
	).WillReturnResult(sqlmock.NewResult(0, 1))
	expectCopiedVersionAssociations(mock, 2, at.UTC(), 2, 5, 2, 5)
	mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(2), at.UTC(), int64(11), int64(21)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	version, draft, err := repository.CreateVersion(context.Background(), 11, 21, 3, at)

	require.NoError(t, err)
	require.Equal(t, Version{Draft: Draft{
		ID: 21, ArticleID: 11, RevisionNo: 2, LockVersion: 3, Status: StatusFrozen, Reason: ReasonManualVersion,
		Title: "Title", Summary: "Summary", CoverMediaID: revisionInt64Pointer(91), ContentMD: "body", ContentHash: testContentHash,
		Tags: versionTagSnapshots(), Media: versionMediaReferences(), CreatedAt: createdAt, UpdatedAt: at.UTC(),
	}}, version)
	require.Equal(t, Draft{
		ID: 2, ArticleID: 11, RevisionNo: 3, LockVersion: 1, Status: StatusEditing, Reason: ReasonDraft,
		Title: "Title", Summary: "Summary", CoverMediaID: revisionInt64Pointer(91), ContentMD: "body", ContentHash: testContentHash,
		Tags: versionTagSnapshots(), Media: versionMediaReferences(), CreatedAt: at.UTC(), UpdatedAt: at.UTC(),
	}, draft)
	require.Equal(t, []string{
		"idseq:article_revisions", "idseq:article_revision_tags", "idseq:article_revision_tags",
		"idseq:article_revision_media", "idseq:article_revision_media",
	}, counter.keys)
	draft.Tags[0].Name = "mutated draft"
	draft.Media[0].PublicKey = "mutated-media"
	*draft.CoverMediaID = 999
	require.Equal(t, "Historic Go", version.Tags[0].Name)
	require.Equal(t, firstMediaKey, version.Media[0].PublicKey)
	require.Equal(t, int64(91), *version.CoverMediaID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryVersionMutationsAcceptExactStoredAssociationLimits(t *testing.T) {
	for _, operation := range []string{"create", "restore"} {
		t.Run(operation, func(t *testing.T) {
			repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
			at := time.Date(2026, 8, 14, 14, 0, 0, 0, time.UTC)
			createdAt := at.Add(-2 * time.Hour)
			updatedAt := at.Add(-time.Hour)
			mock.ExpectBegin()
			if operation == "restore" {
				expectFrozenTarget(mock, 15, createdAt.Add(-2*time.Hour), updatedAt.Add(-2*time.Hour))
				expectRestoreCurrentForUpdate(mock, 30, 3, 4, createdAt, updatedAt)
			} else {
				expectCurrentDraftForUpdate(mock, 3, createdAt, updatedAt)
			}
			associationRevisionID := int64(21)
			oldRevisionID := int64(21)
			lockVersion := int64(3)
			revisionNo := int64(3)
			title, summary, body := "Title", "Summary", "body"
			if operation == "restore" {
				associationRevisionID = 15
				oldRevisionID = 30
				lockVersion = 4
				revisionNo = 4
				title, summary, body = "Historic Title", "Historic Summary", "historic body"
			}
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(associationRevisionID).WillReturnRows(exactLimitTagRows())
			mock.ExpectQuery(selectDraftMediaSQL).WithArgs(associationRevisionID).WillReturnRows(exactLimitMediaRows())
			mock.ExpectExec(freezeManualVersionSQL).WithArgs(at, oldRevisionID, lockVersion).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(insertEditingVersionSQL).WithArgs(
				int64(1), int64(11), revisionNo, title, summary, int64(91), body, testContentHash, at, at,
			).WillReturnResult(sqlmock.NewResult(0, 1))
			expectExactLimitAssociationCopies(mock, 1, at)
			mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(1), at, int64(11), oldRevisionID).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			var draft Draft
			if operation == "restore" {
				var err error
				draft, err = repository.RestoreVersion(context.Background(), 11, 15, 30, 4, at)
				require.NoError(t, err)
			} else {
				version, next, err := repository.CreateVersion(context.Background(), 11, 21, 3, at)
				require.NoError(t, err)
				require.Len(t, version.Tags, MaxTagCount)
				require.Len(t, version.Media, MaxBodyMediaCount+1)
				draft = next
			}
			require.Len(t, draft.Tags, MaxTagCount)
			require.Len(t, draft.Media, MaxBodyMediaCount+1)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryVersionMutationsPreserveNonNilEmptyAssociationSlices(t *testing.T) {
	for _, operation := range []string{"create", "restore"} {
		t.Run(operation, func(t *testing.T) {
			repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
			at := time.Date(2026, 8, 14, 14, 30, 0, 0, time.UTC)
			mock.ExpectBegin()
			oldRevisionID := int64(21)
			lockVersion := int64(3)
			revisionNo := int64(3)
			title, summary, body := "Title", "Summary", "body"
			associationRevisionID := int64(21)
			if operation == "restore" {
				mock.ExpectQuery(selectFrozenVersionSQL).WithArgs(int64(15), int64(11)).WillReturnRows(draftScalarRows().AddRow(
					int64(15), int64(11), int64(1), "frozen", "manual_version", "Historic", "", nil, "historic body", testContentHash, int64(2), at.Add(-4*time.Hour), at.Add(-3*time.Hour),
				))
				mock.ExpectQuery(selectCurrentForUpdateSQL).WithArgs(int64(30), int64(11)).WillReturnRows(draftScalarRows().AddRow(
					int64(30), int64(11), int64(3), "editing", "draft", "Current", "", nil, "current body", testContentHash, int64(4), at.Add(-2*time.Hour), at.Add(-time.Hour),
				))
				oldRevisionID = 30
				lockVersion = 4
				revisionNo = 4
				title, summary, body = "Historic", "", "historic body"
				associationRevisionID = 15
			} else {
				mock.ExpectQuery(selectCurrentForUpdateSQL).WithArgs(int64(21), int64(11)).WillReturnRows(draftScalarRows().AddRow(
					int64(21), int64(11), int64(2), "editing", "draft", title, summary, nil, body, testContentHash, lockVersion, at.Add(-2*time.Hour), at.Add(-time.Hour),
				))
			}
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(associationRevisionID).WillReturnRows(emptyStoredTagRows())
			mock.ExpectQuery(selectDraftMediaSQL).WithArgs(associationRevisionID).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}))
			mock.ExpectExec(freezeManualVersionSQL).WithArgs(at, oldRevisionID, lockVersion).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(insertEditingVersionSQL).WithArgs(int64(1), int64(11), revisionNo, title, summary, nil, body, testContentHash, at, at).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(1), at, int64(11), oldRevisionID).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			if operation == "restore" {
				restored, err := repository.RestoreVersion(context.Background(), 11, 15, 30, 4, at)
				require.NoError(t, err)
				require.NotNil(t, restored.Tags)
				require.NotNil(t, restored.Media)
			} else {
				version, next, err := repository.CreateVersion(context.Background(), 11, 21, 3, at)
				require.NoError(t, err)
				require.NotNil(t, version.Tags)
				require.NotNil(t, version.Media)
				require.NotNil(t, next.Tags)
				require.NotNil(t, next.Media)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCloneDraftDoesNotAliasAssociationSlices(t *testing.T) {
	source := Draft{
		CoverMediaID: revisionInt64Pointer(91),
		Tags:         []tag.Snapshot{{TagID: 7, Name: "Go", Slug: "t_go", Position: 0}},
		Media:        []media.Reference{{MediaID: 91, PublicKey: firstMediaKey, Purpose: "cover", Position: 0}},
	}

	cloned := cloneDraft(source)
	cloned.Tags[0].Name = "Changed"
	cloned.Media[0].PublicKey = secondMediaKey
	*cloned.CoverMediaID = 92

	require.Equal(t, "Go", source.Tags[0].Name)
	require.Equal(t, firstMediaKey, source.Media[0].PublicKey)
	require.Equal(t, int64(91), *source.CoverMediaID)
	empty := cloneDraft(Draft{Tags: []tag.Snapshot{}, Media: []media.Reference{}})
	require.NotNil(t, empty.Tags)
	require.NotNil(t, empty.Media)
}

func TestMySQLRepositoryVersionMutationsRejectMalformedStoredAssociationsBeforeFreezeOrAllocation(t *testing.T) {
	tests := []struct {
		name      string
		tags      func() *sqlmock.Rows
		media     func() *sqlmock.Rows
		mediaRead bool
	}{
		{name: "tags over limit", tags: func() *sqlmock.Rows { return storedTagRows(MaxTagCount + 1) }},
		{name: "tag nonpositive ID", tags: func() *sqlmock.Rows { return storedTagRowsWith(0, "Go", "t_go", 0) }},
		{name: "tag position gap", tags: func() *sqlmock.Rows { return storedTagRowsWith(7, "Go", "t_go", 1) }},
		{name: "tag duplicate", tags: func() *sqlmock.Rows {
			return storedTagRowsWith(7, "Go", "t_go", 0).AddRow(int64(7), "Go Again", "t_go_again", 1)
		}},
		{name: "media over limit", tags: emptyStoredTagRows, mediaRead: true, media: func() *sqlmock.Rows { return storedMediaRows(MaxBodyMediaCount + 1) }},
		{name: "media nonpositive ID", tags: emptyStoredTagRows, mediaRead: true, media: func() *sqlmock.Rows {
			return storedMediaRowsWith(0, firstMediaKey, "cover", 0)
		}},
		{name: "media position gap", tags: emptyStoredTagRows, mediaRead: true, media: func() *sqlmock.Rows {
			return storedMediaRowsWith(91, firstMediaKey, "cover", 1)
		}},
		{name: "media invalid purpose", tags: emptyStoredTagRows, mediaRead: true, media: func() *sqlmock.Rows {
			return storedMediaRowsWith(91, firstMediaKey, "thumbnail", 0)
		}},
		{name: "media duplicate content ID", tags: emptyStoredTagRows, mediaRead: true, media: func() *sqlmock.Rows {
			return storedMediaRowsWith(91, firstMediaKey, "cover", 0).
				AddRow(int64(92), secondMediaKey, "content", 1).
				AddRow(int64(92), mediaKeyForIndex(3), "content", 2)
		}},
		{name: "media duplicate content key", tags: emptyStoredTagRows, mediaRead: true, media: func() *sqlmock.Rows {
			return storedMediaRowsWith(91, firstMediaKey, "cover", 0).
				AddRow(int64(92), secondMediaKey, "content", 1).
				AddRow(int64(93), secondMediaKey, "content", 2)
		}},
		{name: "second cover", tags: emptyStoredTagRows, mediaRead: true, media: func() *sqlmock.Rows {
			return storedMediaRowsWith(91, firstMediaKey, "cover", 0).
				AddRow(int64(92), secondMediaKey, "cover", 1)
		}},
		{name: "missing scalar cover reference", tags: emptyStoredTagRows, mediaRead: true, media: func() *sqlmock.Rows {
			return storedMediaRowsWith(92, secondMediaKey, "content", 0)
		}},
		{name: "mismatched scalar cover", tags: emptyStoredTagRows, mediaRead: true, media: func() *sqlmock.Rows {
			return storedMediaRowsWith(92, firstMediaKey, "cover", 0)
		}},
	}

	for _, operation := range []string{"create", "restore"} {
		for _, test := range tests {
			t.Run(operation+"/"+test.name, func(t *testing.T) {
				repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
				at := time.Date(2026, 8, 14, 14, 0, 0, 0, time.UTC)
				mock.ExpectBegin()
				associationRevisionID := int64(21)
				if operation == "restore" {
					expectFrozenTarget(mock, 15, at.Add(-4*time.Hour), at.Add(-3*time.Hour))
					expectRestoreCurrentForUpdate(mock, 30, 3, 4, at.Add(-2*time.Hour), at.Add(-time.Hour))
					associationRevisionID = 15
				} else {
					expectCurrentDraftForUpdate(mock, 3, at.Add(-2*time.Hour), at.Add(-time.Hour))
				}
				mock.ExpectQuery(selectDraftTagsSQL).WithArgs(associationRevisionID).WillReturnRows(test.tags())
				if test.mediaRead {
					mock.ExpectQuery(selectDraftMediaSQL).WithArgs(associationRevisionID).WillReturnRows(test.media())
				}
				mock.ExpectRollback()

				var err error
				if operation == "restore" {
					_, err = repository.RestoreVersion(context.Background(), 11, 15, 30, 4, at)
				} else {
					_, _, err = repository.CreateVersion(context.Background(), 11, 21, 3, at)
				}

				require.EqualError(t, err, "validate stored revision associations failed")
				require.Empty(t, counter.keys)
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestMySQLRepositoryCreateVersionRejectsStaleLockBeforeAssociationReadsOrInserts(t *testing.T) {
	repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
	at := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	expectCurrentDraftForUpdate(mock, 4, at.Add(-2*time.Hour), at.Add(-time.Hour))
	mock.ExpectRollback()

	_, _, err := repository.CreateVersion(context.Background(), 11, 21, 3, at)

	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryCreateVersionRejectsReplacementDraftWithSameLockBeforeAssociationReadsOrInserts(t *testing.T) {
	repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
	at := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(selectCurrentForUpdateSQL).WithArgs(int64(21), int64(11)).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, _, err := repository.CreateVersion(context.Background(), 11, 21, 1, at)

	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryCreateVersionRollsBackConditionalFailures(t *testing.T) {
	at := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	createdAt := at.Add(-2 * time.Hour)
	updatedAt := at.Add(-time.Hour)
	for _, test := range []struct {
		name      string
		setup     func(sqlmock.Sqlmock)
		wantError error
	}{
		{name: "missing current", wantError: ErrConflict, setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectQuery(selectCurrentForUpdateSQL).WithArgs(int64(21), int64(11)).WillReturnError(sql.ErrNoRows)
			mock.ExpectRollback()
		}},
		{name: "freeze lost race", wantError: ErrConflict, setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectCurrentDraftForUpdate(mock, 3, createdAt, updatedAt)
			expectStoredVersionAssociations(mock, 21)
			mock.ExpectExec(freezeManualVersionSQL).WithArgs(at, int64(21), int64(3)).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectRollback()
		}},
		{name: "inactive article pointer", wantError: ErrArticleInactive, setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectCurrentDraftForUpdate(mock, 3, createdAt, updatedAt)
			expectStoredVersionAssociations(mock, 21)
			mock.ExpectExec(freezeManualVersionSQL).WithArgs(at, int64(21), int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(insertEditingVersionSQL).WithArgs(int64(1), int64(11), int64(3), "Title", "Summary", int64(91), "body", testContentHash, at, at).WillReturnResult(sqlmock.NewResult(0, 1))
			expectCopiedVersionAssociations(mock, 1, at, 1, 2, 1, 2)
			mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(1), at, int64(11), int64(21)).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery(selectArticlePointerSQL).WithArgs(int64(11)).WillReturnRows(sqlmock.NewRows([]string{"state", "draft_revision_id"}).AddRow("trashed", int64(21)))
			mock.ExpectRollback()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
			test.setup(mock)

			_, _, err := repository.CreateVersion(context.Background(), 11, 21, 3, at)

			require.ErrorIs(t, err, test.wantError)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryVersionMutationsClassifyZeroPointerUpdatesFromLockedArticleState(t *testing.T) {
	for _, operation := range []string{"create", "restore"} {
		for _, test := range []struct {
			name       string
			rows       func() *sqlmock.Rows
			queryErr   error
			wantDomain error
		}{
			{name: "missing article", queryErr: sql.ErrNoRows, wantDomain: ErrArticleInactive},
			{name: "inactive article", rows: func() *sqlmock.Rows {
				return sqlmock.NewRows([]string{"state", "draft_revision_id"}).AddRow("trashed", int64(21))
			}, wantDomain: ErrArticleInactive},
			{name: "active null pointer", rows: func() *sqlmock.Rows {
				return sqlmock.NewRows([]string{"state", "draft_revision_id"}).AddRow("active", nil)
			}, wantDomain: ErrConflict},
			{name: "active pointer mismatch", rows: func() *sqlmock.Rows {
				return sqlmock.NewRows([]string{"state", "draft_revision_id"}).AddRow("active", int64(999))
			}, wantDomain: ErrConflict},
			{name: "state query failure", queryErr: errors.New("pointer-state-secret")},
			{name: "impossible active expected pointer", rows: func() *sqlmock.Rows {
				return sqlmock.NewRows([]string{"state", "draft_revision_id"}).AddRow("active", int64(21))
			}},
		} {
			t.Run(operation+"/"+test.name, func(t *testing.T) {
				repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
				at := time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC)
				oldRevisionID := int64(21)
				if operation == "restore" {
					oldRevisionID = 30
					expectRestoreThroughCopies(mock, at, at.Add(-4*time.Hour), at.Add(-3*time.Hour), at.Add(-2*time.Hour), at.Add(-time.Hour))
				} else {
					expectCreateVersionThroughCopies(mock, at, at.Add(-2*time.Hour), at.Add(-time.Hour))
				}
				mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(1), at, int64(11), oldRevisionID).WillReturnResult(sqlmock.NewResult(0, 0))
				query := mock.ExpectQuery(selectArticlePointerSQL).WithArgs(int64(11))
				if test.queryErr != nil {
					query.WillReturnError(test.queryErr)
				} else {
					rows := test.rows()
					if test.name == "impossible active expected pointer" && operation == "restore" {
						rows = sqlmock.NewRows([]string{"state", "draft_revision_id"}).AddRow("active", int64(30))
					}
					query.WillReturnRows(rows)
				}
				mock.ExpectRollback()

				var err error
				if operation == "restore" {
					_, err = repository.RestoreVersion(context.Background(), 11, 15, 30, 4, at)
				} else {
					_, _, err = repository.CreateVersion(context.Background(), 11, 21, 3, at)
				}

				if test.wantDomain != nil {
					require.ErrorIs(t, err, test.wantDomain)
				} else {
					require.EqualError(t, err, "verify active article draft pointer failed")
					require.NotContains(t, err.Error(), "secret")
				}
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestMySQLRepositoryCreateVersionRollsBackAndSanitizesEveryDependencyStage(t *testing.T) {
	at := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	createdAt := at.Add(-2 * time.Hour)
	updatedAt := at.Add(-time.Hour)
	tests := []struct {
		name   string
		failAt map[string]map[int]error
		setup  func(sqlmock.Sqlmock)
	}{
		{name: "begin", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin().WillReturnError(errors.New("begin-secret"))
		}},
		{name: "current query", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectQuery(selectCurrentForUpdateSQL).WithArgs(int64(21), int64(11)).WillReturnError(errors.New("current-secret"))
			mock.ExpectRollback()
		}},
		{name: "tag query", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectCurrentDraftForUpdate(mock, 3, createdAt, updatedAt)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnError(errors.New("tag-secret"))
			mock.ExpectRollback()
		}},
		{name: "media query", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectCurrentDraftForUpdate(mock, 3, createdAt, updatedAt)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}))
			mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnError(errors.New("media-secret"))
			mock.ExpectRollback()
		}},
		{name: "freeze exec", setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughAssociations(mock, at, createdAt, updatedAt)
			mock.ExpectExec(freezeManualVersionSQL).WithArgs(at, int64(21), int64(3)).WillReturnError(errors.New("freeze-secret"))
			mock.ExpectRollback()
		}},
		{name: "freeze rows", setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughAssociations(mock, at, createdAt, updatedAt)
			mock.ExpectExec(freezeManualVersionSQL).WithArgs(at, int64(21), int64(3)).WillReturnResult(sqlmock.NewErrorResult(errors.New("freeze-rows-secret")))
			mock.ExpectRollback()
		}},
		{name: "revision allocation", failAt: map[string]map[int]error{"idseq:article_revisions": {1: errors.New("redis-revision-secret")}}, setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughFreeze(mock, at, createdAt, updatedAt)
			mock.ExpectRollback()
		}},
		{name: "revision insert", setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughFreeze(mock, at, createdAt, updatedAt)
			mock.ExpectExec(insertEditingVersionSQL).WithArgs(int64(1), int64(11), int64(3), "Title", "Summary", int64(91), "body", testContentHash, at, at).WillReturnError(errors.New("revision-insert-secret"))
			mock.ExpectRollback()
		}},
		{name: "tag allocation", failAt: map[string]map[int]error{"idseq:article_revision_tags": {1: errors.New("redis-tag-secret")}}, setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughRevisionInsert(mock, at, createdAt, updatedAt)
			mock.ExpectRollback()
		}},
		{name: "tag insert", setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughRevisionInsert(mock, at, createdAt, updatedAt)
			mock.ExpectExec(insertDraftTagSQL).WithArgs(int64(1), int64(1), int64(7), "Historic Go", "t_historic_go", 0, at).WillReturnError(errors.New("tag-insert-secret"))
			mock.ExpectRollback()
		}},
		{name: "media allocation", failAt: map[string]map[int]error{"idseq:article_revision_media": {1: errors.New("redis-media-secret")}}, setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughRevisionInsert(mock, at, createdAt, updatedAt)
			expectCopiedVersionTags(mock, 1, at, 1, 2)
			mock.ExpectRollback()
		}},
		{name: "media insert", setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughRevisionInsert(mock, at, createdAt, updatedAt)
			expectCopiedVersionTags(mock, 1, at, 1, 2)
			mock.ExpectExec(insertDraftMediaSQL).WithArgs(int64(1), int64(1), int64(91), "cover", 0, at).WillReturnError(errors.New("media-insert-secret"))
			mock.ExpectRollback()
		}},
		{name: "pointer exec", setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughCopies(mock, at, createdAt, updatedAt)
			mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(1), at, int64(11), int64(21)).WillReturnError(errors.New("pointer-secret"))
			mock.ExpectRollback()
		}},
		{name: "pointer rows", setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughCopies(mock, at, createdAt, updatedAt)
			mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(1), at, int64(11), int64(21)).WillReturnResult(sqlmock.NewErrorResult(errors.New("pointer-rows-secret")))
			mock.ExpectRollback()
		}},
		{name: "commit", setup: func(mock sqlmock.Sqlmock) {
			expectCreateVersionThroughCopies(mock, at, createdAt, updatedAt)
			mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(1), at, int64(11), int64(21)).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit().WillReturnError(errors.New("commit-secret"))
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
			counter.failAt = test.failAt
			test.setup(mock)

			_, _, err := repository.CreateVersion(context.Background(), 11, 21, 3, at)

			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryRestoreVersionCopiesHistoricSnapshotAndPreservesCurrentWork(t *testing.T) {
	repository, mock, counter := newRevisionRepositoryTest(t, 2, 3)
	targetCreatedAt := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	targetUpdatedAt := targetCreatedAt.Add(time.Hour)
	currentCreatedAt := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	currentUpdatedAt := currentCreatedAt.Add(time.Hour)
	at := time.Date(2026, 8, 14, 13, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	mock.ExpectBegin()
	expectFrozenTarget(mock, 15, targetCreatedAt, targetUpdatedAt)
	expectRestoreCurrentForUpdate(mock, 30, 3, 4, currentCreatedAt, currentUpdatedAt)
	expectStoredVersionAssociations(mock, 15)
	mock.ExpectExec(freezeManualVersionSQL).WithArgs(at.UTC(), int64(30), int64(4)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertEditingVersionSQL).WithArgs(
		int64(2), int64(11), int64(4), "Historic Title", "Historic Summary", int64(91), "historic body", testContentHash, at.UTC(), at.UTC(),
	).WillReturnResult(sqlmock.NewResult(0, 1))
	expectCopiedVersionAssociations(mock, 2, at.UTC(), 2, 5, 2, 5)
	mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(2), at.UTC(), int64(11), int64(30)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	draft, err := repository.RestoreVersion(context.Background(), 11, 15, 30, 4, at)

	require.NoError(t, err)
	require.Equal(t, Draft{
		ID: 2, ArticleID: 11, RevisionNo: 4, LockVersion: 1, Status: StatusEditing, Reason: ReasonDraft,
		Title: "Historic Title", Summary: "Historic Summary", CoverMediaID: revisionInt64Pointer(91),
		ContentMD: "historic body", ContentHash: testContentHash,
		Tags: versionTagSnapshots(), Media: versionMediaReferences(), CreatedAt: at.UTC(), UpdatedAt: at.UTC(),
	}, draft)
	require.Equal(t, []string{
		"idseq:article_revisions", "idseq:article_revision_tags", "idseq:article_revision_tags",
		"idseq:article_revision_media", "idseq:article_revision_media",
	}, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryRestoreVersionRejectsInvalidTargetAndStaleCurrentBeforeInserts(t *testing.T) {
	at := time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC)
	t.Run("target not frozen or wrong article", func(t *testing.T) {
		repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
		mock.ExpectBegin()
		mock.ExpectQuery(selectFrozenVersionSQL).WithArgs(int64(15), int64(11)).WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		_, err := repository.RestoreVersion(context.Background(), 11, 15, 30, 4, at)

		require.ErrorIs(t, err, ErrNotFrozen)
		require.Empty(t, counter.keys)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("stale current lock", func(t *testing.T) {
		repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
		mock.ExpectBegin()
		expectFrozenTarget(mock, 15, at.Add(-4*time.Hour), at.Add(-3*time.Hour))
		expectRestoreCurrentForUpdate(mock, 30, 3, 5, at.Add(-2*time.Hour), at.Add(-time.Hour))
		mock.ExpectRollback()

		_, err := repository.RestoreVersion(context.Background(), 11, 15, 30, 4, at)

		require.ErrorIs(t, err, ErrConflict)
		require.Empty(t, counter.keys)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("replacement draft with same lock", func(t *testing.T) {
		repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
		mock.ExpectBegin()
		expectFrozenTarget(mock, 15, at.Add(-4*time.Hour), at.Add(-3*time.Hour))
		mock.ExpectQuery(selectCurrentForUpdateSQL).WithArgs(int64(30), int64(11)).WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		_, err := repository.RestoreVersion(context.Background(), 11, 15, 30, 1, at)

		require.ErrorIs(t, err, ErrConflict)
		require.Empty(t, counter.keys)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestMySQLRepositoryRestoreVersionRollsBackAndSanitizesEveryDistinctStage(t *testing.T) {
	at := time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC)
	targetCreatedAt := at.Add(-4 * time.Hour)
	targetUpdatedAt := at.Add(-3 * time.Hour)
	currentCreatedAt := at.Add(-2 * time.Hour)
	currentUpdatedAt := at.Add(-time.Hour)
	tests := []struct {
		name      string
		setup     func(sqlmock.Sqlmock)
		wantError error
	}{
		{name: "begin", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin().WillReturnError(errors.New("restore-begin-secret"))
		}},
		{name: "target query", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectQuery(selectFrozenVersionSQL).WithArgs(int64(15), int64(11)).WillReturnError(errors.New("target-secret"))
			mock.ExpectRollback()
		}},
		{name: "current query", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectFrozenTarget(mock, 15, targetCreatedAt, targetUpdatedAt)
			mock.ExpectQuery(selectCurrentForUpdateSQL).WithArgs(int64(30), int64(11)).WillReturnError(errors.New("restore-current-secret"))
			mock.ExpectRollback()
		}},
		{name: "target associations", setup: func(mock sqlmock.Sqlmock) {
			expectRestoreThroughCurrent(mock, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(15)).WillReturnError(errors.New("historic-tag-secret"))
			mock.ExpectRollback()
		}},
		{name: "freeze lost race", wantError: ErrConflict, setup: func(mock sqlmock.Sqlmock) {
			expectRestoreThroughAssociations(mock, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt)
			mock.ExpectExec(freezeManualVersionSQL).WithArgs(at, int64(30), int64(4)).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectRollback()
		}},
		{name: "freeze query", setup: func(mock sqlmock.Sqlmock) {
			expectRestoreThroughAssociations(mock, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt)
			mock.ExpectExec(freezeManualVersionSQL).WithArgs(at, int64(30), int64(4)).WillReturnError(errors.New("restore-freeze-secret"))
			mock.ExpectRollback()
		}},
		{name: "new revision insert", setup: func(mock sqlmock.Sqlmock) {
			expectRestoreThroughFreeze(mock, at, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt)
			mock.ExpectExec(insertEditingVersionSQL).WithArgs(int64(1), int64(11), int64(4), "Historic Title", "Historic Summary", int64(91), "historic body", testContentHash, at, at).WillReturnError(errors.New("restore-insert-secret"))
			mock.ExpectRollback()
		}},
		{name: "inactive pointer", wantError: ErrArticleInactive, setup: func(mock sqlmock.Sqlmock) {
			expectRestoreThroughCopies(mock, at, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt)
			mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(1), at, int64(11), int64(30)).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery(selectArticlePointerSQL).WithArgs(int64(11)).WillReturnRows(sqlmock.NewRows([]string{"state", "draft_revision_id"}).AddRow("trashed", int64(30)))
			mock.ExpectRollback()
		}},
		{name: "commit", setup: func(mock sqlmock.Sqlmock) {
			expectRestoreThroughCopies(mock, at, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt)
			mock.ExpectExec(replaceDraftPointerSQL).WithArgs(int64(1), at, int64(11), int64(30)).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit().WillReturnError(errors.New("restore-commit-secret"))
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
			test.setup(mock)

			_, err := repository.RestoreVersion(context.Background(), 11, 15, 30, 4, at)

			if test.wantError != nil {
				require.ErrorIs(t, err, test.wantError)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryListVersionsReturnsDescendingImmutableSnapshots(t *testing.T) {
	repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
	versionThreeAt := time.Date(2026, 8, 13, 3, 0, 0, 0, time.UTC)
	versionOneAt := time.Date(2026, 8, 11, 1, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(listFrozenVersionsSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().
		AddRow(int64(30), int64(11), int64(3), "frozen", "publish_snapshot", "Published", "Latest", nil, "published body", testContentHash, int64(4), versionThreeAt, versionThreeAt).
		AddRow(int64(15), int64(11), int64(1), "frozen", "manual_version", "Historic", "Original", int64(91), "historic body", testContentHash, int64(2), versionOneAt, versionOneAt),
	)
	mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(30)).WillReturnRows(
		sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).AddRow(int64(7), "Renamed Later", "t_renamed_later", 0),
	)
	mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(30)).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}))
	mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(15)).WillReturnRows(
		sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).AddRow(int64(7), "Historic Go", "t_historic_go", 0),
	)
	mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(15)).WillReturnRows(
		sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}).AddRow(int64(91), firstMediaKey, "cover", 0),
	)
	mock.ExpectCommit()

	versions, err := repository.ListVersions(context.Background(), 11)

	require.NoError(t, err)
	require.Equal(t, []int64{3, 1}, []int64{versions[0].RevisionNo, versions[1].RevisionNo})
	require.Equal(t, "Renamed Later", versions[0].Tags[0].Name)
	require.Equal(t, "Historic Go", versions[1].Tags[0].Name)
	require.Equal(t, "t_historic_go", versions[1].Tags[0].Slug)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryListVersionsReturnsNonNilEmptyHistory(t *testing.T) {
	repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
	mock.ExpectBegin()
	mock.ExpectQuery(listFrozenVersionsSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows())
	mock.ExpectCommit()

	versions, err := repository.ListVersions(context.Background(), 11)

	require.NoError(t, err)
	require.NotNil(t, versions)
	require.Empty(t, versions)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryListVersionsRollsBackAndSanitizesReadFailures(t *testing.T) {
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		setup func(sqlmock.Sqlmock)
	}{
		{name: "begin", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin().WillReturnError(errors.New("history-begin-secret"))
		}},
		{name: "list query", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectQuery(listFrozenVersionsSQL).WithArgs(int64(11)).WillReturnError(errors.New("history-query-secret"))
			mock.ExpectRollback()
		}},
		{name: "scalar scan", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectQuery(listFrozenVersionsSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().AddRow(
				"revision-id-secret", int64(11), int64(1), "frozen", "manual_version", "Title", "", nil, "body", testContentHash, int64(1), at, at,
			))
			mock.ExpectRollback()
		}},
		{name: "rows", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectQuery(listFrozenVersionsSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().
				AddRow(int64(15), int64(11), int64(1), "frozen", "manual_version", "Title", "", nil, "body", testContentHash, int64(1), at, at).
				RowError(0, errors.New("history-rows-secret")),
			)
			mock.ExpectRollback()
		}},
		{name: "tag query", setup: func(mock sqlmock.Sqlmock) {
			expectOneFrozenHistoryScalar(mock, at)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(15)).WillReturnError(errors.New("history-tag-secret"))
			mock.ExpectRollback()
		}},
		{name: "media query", setup: func(mock sqlmock.Sqlmock) {
			expectOneFrozenHistoryScalar(mock, at)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(15)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}))
			mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(15)).WillReturnError(errors.New("history-media-secret"))
			mock.ExpectRollback()
		}},
		{name: "commit", setup: func(mock sqlmock.Sqlmock) {
			expectOneFrozenHistoryScalar(mock, at)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(15)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}))
			mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(15)).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}))
			mock.ExpectCommit().WillReturnError(errors.New("history-commit-secret"))
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
			test.setup(mock)

			_, err := repository.ListVersions(context.Background(), 11)

			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryGetDraftLoadsEditingScalarAndOrderedAssociations(t *testing.T) {
	repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
	createdAt := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	mock.ExpectBegin()
	mock.ExpectQuery(selectEditingDraftSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().AddRow(
		int64(21), int64(11), int64(2), "editing", "draft", "Title", "Summary", int64(91), "body", testContentHash, int64(3), createdAt, updatedAt,
	))
	mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(
		sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).
			AddRow(int64(7), "Go", "t_go", 0).
			AddRow(int64(3), "Web", "t_web", 1),
	)
	mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnRows(
		sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}).
			AddRow(int64(91), firstMediaKey, "cover", 0).
			AddRow(int64(92), secondMediaKey, "content", 1),
	)
	mock.ExpectCommit()

	got, err := repository.GetDraft(context.Background(), 11)

	require.NoError(t, err)
	require.Equal(t, Draft{
		ID: 21, ArticleID: 11, RevisionNo: 2, LockVersion: 3,
		Status: StatusEditing, Reason: ReasonDraft,
		Title: "Title", Summary: "Summary", CoverMediaID: revisionInt64Pointer(91), ContentMD: "body", ContentHash: testContentHash,
		Tags: []tag.Snapshot{
			{TagID: 7, Name: "Go", Slug: "t_go", Position: 0},
			{TagID: 3, Name: "Web", Slug: "t_web", Position: 1},
		},
		Media: []media.Reference{
			{MediaID: 91, PublicKey: firstMediaKey, Purpose: "cover", Position: 0},
			{MediaID: 92, PublicKey: secondMediaKey, Purpose: "content", Position: 1},
		},
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryGetDraftReturnsNonNilEmptyAssociationsAndNullCover(t *testing.T) {
	repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
	at := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(selectEditingDraftSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().AddRow(
		int64(21), int64(11), int64(1), "editing", "draft", "", "", nil, "", ComputeHash(PreparedContent{}), int64(1), at, at,
	))
	mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}))
	mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}))
	mock.ExpectCommit()

	got, err := repository.GetDraft(context.Background(), 11)

	require.NoError(t, err)
	require.Nil(t, got.CoverMediaID)
	require.NotNil(t, got.Tags)
	require.Empty(t, got.Tags)
	require.NotNil(t, got.Media)
	require.Empty(t, got.Media)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryGetDraftMapsMissingAndSanitizesAssociationFailures(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
		mock.ExpectBegin()
		mock.ExpectQuery(selectEditingDraftSQL).WithArgs(int64(11)).WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		_, err := repository.GetDraft(context.Background(), 11)

		require.ErrorIs(t, err, ErrNotFound)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	for _, test := range []struct {
		name  string
		setup func(sqlmock.Sqlmock)
	}{
		{name: "scalar scan", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(selectEditingDraftSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().AddRow(
				"id-secret", int64(11), int64(1), "editing", "draft", "", "", nil, "", ComputeHash(PreparedContent{}), int64(1), time.Now(), time.Now(),
			))
		}},
		{name: "tag query", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnError(errors.New("tag-query-secret"))
		}},
		{name: "tag scan", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(
				sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).AddRow("tag-id-secret", "Go", "t_go", 0),
			)
		}},
		{name: "tag rows", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(
				sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).
					AddRow(int64(7), "Go", "t_go", 0).
					RowError(0, errors.New("tag-rows-secret")),
			)
		}},
		{name: "media query", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			expectNoStoredTags(mock)
			mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnError(errors.New("media-query-secret"))
		}},
		{name: "media scan", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			expectNoStoredTags(mock)
			mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnRows(
				sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}).AddRow("media-id-secret", firstMediaKey, "content", 0),
			)
		}},
		{name: "media rows", setup: func(mock sqlmock.Sqlmock) {
			expectStoredDraftScalar(mock)
			expectNoStoredTags(mock)
			mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnRows(
				sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}).
					AddRow(int64(91), firstMediaKey, "content", 0).
					RowError(0, errors.New("media-rows-secret")),
			)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
			mock.ExpectBegin()
			test.setup(mock)
			mock.ExpectRollback()

			_, err := repository.GetDraft(context.Background(), 11)

			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositoryGetDraftSanitizesTransactionFailures(t *testing.T) {
	t.Run("begin", func(t *testing.T) {
		repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
		mock.ExpectBegin().WillReturnError(errors.New("begin-secret"))

		_, err := repository.GetDraft(context.Background(), 11)

		require.Error(t, err)
		require.NotContains(t, err.Error(), "begin-secret")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("commit", func(t *testing.T) {
		repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
		mock.ExpectBegin()
		expectStoredDraftScalar(mock)
		expectNoStoredTags(mock)
		mock.ExpectQuery(selectDraftMediaSQL).WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}))
		mock.ExpectCommit().WillReturnError(errors.New("commit-secret"))

		_, err := repository.GetDraft(context.Background(), 11)

		require.Error(t, err)
		require.NotContains(t, err.Error(), "commit-secret")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestMySQLRepositorySaveDraftUsesOneTransactionAndSharedAssociationIDs(t *testing.T) {
	repository, mock, counter := newRevisionRepositoryTest(t, 2, 3)
	at := time.Date(2026, 8, 14, 9, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	createdAt := at.UTC().Add(-time.Hour)
	content := preparedRepositoryContent()
	mock.ExpectBegin()
	expectDraftUpdate(mock, content, at.UTC(), sqlmock.NewResult(0, 1))
	mock.ExpectQuery(selectSavedIdentitySQL).WithArgs(int64(11)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(21), int64(4), int64(2), createdAt),
	)
	mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(insertDraftTagSQL).
		WithArgs(int64(2), int64(21), int64(7), "Go", "t_go", 0, at.UTC()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftTagSQL).
		WithArgs(int64(5), int64(21), int64(3), "Web", "t_web", 1, at.UTC()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(2), int64(21), int64(91), "cover", 0, at.UTC()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(5), int64(21), int64(92), "content", 1, at.UTC()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(8), int64(21), int64(93), "content", 2, at.UTC()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(touchActiveArticleSQL).WithArgs(at.UTC(), int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

	require.NoError(t, err)
	require.Equal(t, Draft{
		ID: 21, ArticleID: 11, RevisionNo: 2, LockVersion: 4,
		Status: StatusEditing, Reason: ReasonDraft,
		Title: content.Title, Summary: content.Summary, CoverMediaID: revisionInt64Pointer(91), ContentMD: content.ContentMD, ContentHash: content.ContentHash,
		Tags: content.Tags, Media: content.Media, CreatedAt: createdAt, UpdatedAt: at.UTC(),
	}, got)
	require.Equal(t, []string{
		"idseq:article_revision_tags", "idseq:article_revision_tags",
		"idseq:article_revision_media", "idseq:article_revision_media", "idseq:article_revision_media",
	}, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositorySaveDraftSupportsNullCoverAndEmptyAssociations(t *testing.T) {
	repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	createdAt := at.Add(-time.Hour)
	content := PreparedContent{Title: "Title", ContentMD: "body", Tags: []tag.Snapshot{}, Media: []media.Reference{}}
	content.ContentHash = ComputeHash(content)
	mock.ExpectBegin()
	mock.ExpectExec(updateEditingDraftSQL).
		WithArgs("Title", "", nil, "body", content.ContentHash, at, int64(11), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(selectSavedIdentitySQL).WithArgs(int64(11)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(21), int64(2), int64(1), createdAt),
	)
	mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := repository.SaveDraft(context.Background(), 11, 1, content, at)

	require.NoError(t, err)
	require.Nil(t, got.CoverMediaID)
	require.NotNil(t, got.Tags)
	require.NotNil(t, got.Media)
	require.Empty(t, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositorySaveDraftConflictStopsBeforeAssociationMutation(t *testing.T) {
	repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	content := preparedRepositoryContent()
	mock.ExpectBegin()
	expectDraftUpdate(mock, content, at, sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	_, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, counter.keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositorySaveDraftRollsBackSQLAndAllocationFailures(t *testing.T) {
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	createdAt := at.Add(-time.Hour)
	content := preparedRepositoryContent()
	tests := []struct {
		name   string
		failAt map[string]map[int]error
		setup  func(sqlmock.Sqlmock)
	}{
		{name: "update", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			mock.ExpectExec(updateEditingDraftSQL).
				WithArgs(content.Title, content.Summary, int64(91), content.ContentMD, content.ContentHash, at, int64(11), int64(3)).
				WillReturnError(errors.New("update-secret"))
			mock.ExpectRollback()
		}},
		{name: "update rows", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectDraftUpdate(mock, content, at, sqlmock.NewErrorResult(errors.New("rows-secret")))
			mock.ExpectRollback()
		}},
		{name: "identity query", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectDraftUpdate(mock, content, at, sqlmock.NewResult(0, 1))
			mock.ExpectQuery(selectSavedIdentitySQL).WithArgs(int64(11)).WillReturnError(errors.New("identity-secret"))
			mock.ExpectRollback()
		}},
		{name: "identity lock mismatch", setup: func(mock sqlmock.Sqlmock) {
			mock.ExpectBegin()
			expectDraftUpdate(mock, content, at, sqlmock.NewResult(0, 1))
			mock.ExpectQuery(selectSavedIdentitySQL).WithArgs(int64(11)).WillReturnRows(
				sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(21), int64(9), int64(2), createdAt),
			)
			mock.ExpectRollback()
		}},
		{name: "delete tags", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughIdentity(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnError(errors.New("delete-tags-secret"))
			mock.ExpectRollback()
		}},
		{name: "tag allocation", failAt: map[string]map[int]error{"idseq:article_revision_tags": {1: errors.New("redis-tag-secret")}}, setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughIdentity(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 2))
			mock.ExpectRollback()
		}},
		{name: "tag insert", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughIdentity(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 2))
			mock.ExpectExec(insertDraftTagSQL).
				WithArgs(int64(1), int64(21), int64(7), "Go", "t_go", 0, at).
				WillReturnError(errors.New("insert-tag-secret"))
			mock.ExpectRollback()
		}},
		{name: "delete media", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughTags(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnError(errors.New("delete-media-secret"))
			mock.ExpectRollback()
		}},
		{name: "media allocation", failAt: map[string]map[int]error{"idseq:article_revision_media": {1: errors.New("redis-media-secret")}}, setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughTags(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 3))
			mock.ExpectRollback()
		}},
		{name: "media insert", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughTags(mock, content, at, createdAt)
			mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 3))
			mock.ExpectExec(insertDraftMediaSQL).
				WithArgs(int64(1), int64(21), int64(91), "cover", 0, at).
				WillReturnError(errors.New("insert-media-secret"))
			mock.ExpectRollback()
		}},
		{name: "touch article", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughMedia(mock, content, at, createdAt)
			mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnError(errors.New("touch-secret"))
			mock.ExpectRollback()
		}},
		{name: "touch rows", setup: func(mock sqlmock.Sqlmock) {
			expectSaveThroughMedia(mock, content, at, createdAt)
			mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewErrorResult(errors.New("touch-rows-secret")))
			mock.ExpectRollback()
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, counter := newRevisionRepositoryTest(t, 1, 1)
			counter.failAt = test.failAt
			test.setup(mock)

			_, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositorySaveDraftAcceptsNoopTouchForStillActiveArticle(t *testing.T) {
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	createdAt := at.Add(-time.Hour)
	content := preparedRepositoryContent()
	repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
	expectSaveThroughMedia(mock, content, at, createdAt)
	mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(selectArticleStateSQL).WithArgs(int64(11)).WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow("active"))
	mock.ExpectCommit()

	got, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

	require.NoError(t, err)
	require.Equal(t, int64(4), got.LockVersion)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositorySaveDraftMapsNoopTouchStateFailures(t *testing.T) {
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	createdAt := at.Add(-time.Hour)
	content := preparedRepositoryContent()

	for _, test := range []struct {
		name      string
		stateRows *sqlmock.Rows
		stateErr  error
		domain    error
	}{
		{name: "inactive article", stateRows: sqlmock.NewRows([]string{"state"}).AddRow("trashed"), domain: ErrArticleInactive},
		{name: "missing article", stateErr: sql.ErrNoRows, domain: ErrArticleInactive},
		{name: "state query", stateErr: errors.New("article-state-secret")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
			expectSaveThroughMedia(mock, content, at, createdAt)
			mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewResult(0, 0))
			query := mock.ExpectQuery(selectArticleStateSQL).WithArgs(int64(11))
			if test.stateErr != nil {
				query.WillReturnError(test.stateErr)
			} else {
				query.WillReturnRows(test.stateRows)
			}
			mock.ExpectRollback()

			_, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

			if test.domain != nil {
				require.ErrorIs(t, err, test.domain)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "article-state-secret")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMySQLRepositorySaveDraftSanitizesCommitFailure(t *testing.T) {
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	createdAt := at.Add(-time.Hour)
	content := preparedRepositoryContent()

	repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)
	expectSaveThroughMedia(mock, content, at, createdAt)
	mock.ExpectExec(touchActiveArticleSQL).WithArgs(at, int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(errors.New("commit-secret"))

	_, err := repository.SaveDraft(context.Background(), 11, 3, content, at)

	require.Error(t, err)
	require.NotContains(t, err.Error(), "commit-secret")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryRejectsInvalidAndNilConfigurationSafely(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ids, err := idgen.New(&revisionCounter{}, nil, 1, 1, false)
	require.NoError(t, err)

	for _, repository := range []*MySQLRepository{nil, NewMySQLRepository(nil, ids), NewMySQLRepository(db, nil)} {
		require.NotPanics(t, func() {
			_, getErr := repository.GetDraft(context.Background(), 11)
			require.Error(t, getErr)
			_, _, versionErr := repository.CreateVersion(context.Background(), 11, 21, 1, time.Now())
			require.Error(t, versionErr)
			_, listErr := repository.ListVersions(context.Background(), 11)
			require.Error(t, listErr)
			_, restoreErr := repository.RestoreVersion(context.Background(), 11, 21, 30, 1, time.Now())
			require.Error(t, restoreErr)
		})
	}

	repository := NewMySQLRepository(db, ids)
	_, err = repository.GetDraft(nil, 11)
	require.Error(t, err)
	_, err = repository.GetDraft(context.Background(), 0)
	require.ErrorIs(t, err, ErrInvalidContent)
	_, err = repository.SaveDraft(context.Background(), 11, 0, preparedRepositoryContent(), time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)
	_, _, err = repository.CreateVersion(context.Background(), 11, 21, 0, time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)
	_, _, err = repository.CreateVersion(context.Background(), 11, 0, 1, time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)
	_, err = repository.ListVersions(context.Background(), 0)
	require.ErrorIs(t, err, ErrInvalidContent)
	_, err = repository.RestoreVersion(context.Background(), 11, 0, 30, 1, time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)
	_, err = repository.RestoreVersion(context.Background(), 11, 21, 30, 0, time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)
	_, err = repository.RestoreVersion(context.Background(), 11, 21, 0, 1, time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)
	invalid := preparedRepositoryContent()
	invalid.ContentHash = "not-a-hash-secret"
	_, err = repository.SaveDraft(context.Background(), 11, 1, invalid, time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryRejectsAmplifiedPreparedContentBeforeTransaction(t *testing.T) {
	repository, mock, _ := newRevisionRepositoryTest(t, 1, 1)

	overTags := preparedRepositoryContent()
	overTags.Tags = make([]tag.Snapshot, MaxTagCount+1)
	for index := range overTags.Tags {
		overTags.Tags[index] = tag.Snapshot{TagID: int64(index + 1), Name: "Tag", Slug: "t_tag", Position: index}
	}
	overTags.ContentHash = ComputeHash(overTags)
	_, err := repository.SaveDraft(context.Background(), 11, 1, overTags, time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)

	overMedia := preparedRepositoryContent()
	overMedia.Media = make([]media.Reference, MaxBodyMediaCount+2)
	for index := range overMedia.Media {
		overMedia.Media[index] = media.Reference{MediaID: int64(index + 1), PublicKey: firstMediaKey, Purpose: "content", Position: index}
	}
	overMedia.ContentHash = ComputeHash(overMedia)
	_, err = repository.SaveDraft(context.Background(), 11, 1, overMedia, time.Now())
	require.ErrorIs(t, err, ErrInvalidContent)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMySQLRepositoryZeroIDGeneratorReturnsSanitizedErrorAndRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repository := NewMySQLRepository(db, &idgen.Generator{})
	at := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	createdAt := at.Add(-time.Hour)
	content := preparedRepositoryContent()
	expectSaveThroughIdentity(mock, content, at, createdAt)
	mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectRollback()

	_, err = repository.SaveDraft(context.Background(), 11, 3, content, at)

	require.Error(t, err)
	require.NotContains(t, err.Error(), "configured")
	require.NoError(t, mock.ExpectationsWereMet())
}

type revisionCounter struct {
	raw    map[string]int64
	keys   []string
	calls  map[string]int
	failAt map[string]map[int]error
}

func (c *revisionCounter) Increment(_ context.Context, key string) (int64, error) {
	c.keys = append(c.keys, key)
	if c.calls == nil {
		c.calls = make(map[string]int)
	}
	c.calls[key]++
	if err := c.failAt[key][c.calls[key]]; err != nil {
		return 0, err
	}
	if c.raw == nil {
		c.raw = make(map[string]int64)
	}
	c.raw[key]++
	return c.raw[key], nil
}

func (c *revisionCounter) Raise(_ context.Context, _ string, _ int64) (int64, error) {
	return 0, errors.New("unexpected raise")
}

func newRevisionRepositoryTest(t *testing.T, offset, step int64) (*MySQLRepository, sqlmock.Sqlmock, *revisionCounter) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	counter := &revisionCounter{}
	ids, err := idgen.New(counter, nil, offset, step, false)
	require.NoError(t, err)
	return NewMySQLRepository(db, ids), mock, counter
}

func preparedRepositoryContent() PreparedContent {
	cover := &media.Media{ID: 91, PublicKey: firstMediaKey, State: "active"}
	return PreparedContent{
		Title: "Title", Summary: "Summary", Cover: cover,
		ContentMD: "![first](/img/proxy/" + firstMediaKey + ")\n![second](/img/proxy/" + secondMediaKey + ")",
		Tags: []tag.Snapshot{
			{TagID: 7, Name: "Go", Slug: "t_go", Position: 0},
			{TagID: 3, Name: "Web", Slug: "t_web", Position: 1},
		},
		Media: []media.Reference{
			{MediaID: 91, PublicKey: firstMediaKey, Purpose: "cover", Position: 0},
			{MediaID: 92, PublicKey: firstMediaKey, Purpose: "content", Position: 1},
			{MediaID: 93, PublicKey: secondMediaKey, Purpose: "content", Position: 2},
		},
		ContentHash: testContentHash,
	}
}

func expectDraftUpdate(mock sqlmock.Sqlmock, content PreparedContent, at time.Time, result sql.Result) {
	mock.ExpectExec(updateEditingDraftSQL).
		WithArgs(content.Title, content.Summary, int64(91), content.ContentMD, content.ContentHash, at, int64(11), int64(3)).
		WillReturnResult(result)
}

func expectSaveThroughIdentity(mock sqlmock.Sqlmock, content PreparedContent, at, createdAt time.Time) {
	mock.ExpectBegin()
	expectDraftUpdate(mock, content, at, sqlmock.NewResult(0, 1))
	mock.ExpectQuery(selectSavedIdentitySQL).WithArgs(int64(11)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(21), int64(4), int64(2), createdAt),
	)
}

func expectSaveThroughTags(mock sqlmock.Sqlmock, content PreparedContent, at, createdAt time.Time) {
	expectSaveThroughIdentity(mock, content, at, createdAt)
	mock.ExpectExec(deleteDraftTagsSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(insertDraftTagSQL).
		WithArgs(int64(1), int64(21), int64(7), "Go", "t_go", 0, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftTagSQL).
		WithArgs(int64(2), int64(21), int64(3), "Web", "t_web", 1, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectSaveThroughMedia(mock sqlmock.Sqlmock, content PreparedContent, at, createdAt time.Time) {
	expectSaveThroughTags(mock, content, at, createdAt)
	mock.ExpectExec(deleteDraftMediaSQL).WithArgs(int64(21)).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(1), int64(21), int64(91), "cover", 0, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(2), int64(21), int64(92), "content", 1, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(3), int64(21), int64(93), "content", 2, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func draftScalarRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "article_id", "revision_no", "status", "reason", "title", "summary", "cover_media_id", "content_md", "content_hash", "lock_version", "created_at", "updated_at",
	})
}

func expectStoredDraftScalar(mock sqlmock.Sqlmock) {
	at := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(selectEditingDraftSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().AddRow(
		int64(21), int64(11), int64(1), "editing", "draft", "", "", nil, "", ComputeHash(PreparedContent{}), int64(1), at, at,
	))
}

func expectNoStoredTags(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(selectDraftTagsSQL).WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}))
}

func expectCurrentDraftForUpdate(mock sqlmock.Sqlmock, lockVersion int64, createdAt, updatedAt time.Time) {
	mock.ExpectQuery(selectCurrentForUpdateSQL).WithArgs(int64(21), int64(11)).WillReturnRows(draftScalarRows().AddRow(
		int64(21), int64(11), int64(2), "editing", "draft", "Title", "Summary", int64(91), "body", testContentHash,
		lockVersion, createdAt, updatedAt,
	))
}

func expectFrozenTarget(mock sqlmock.Sqlmock, revisionID int64, createdAt, updatedAt time.Time) {
	mock.ExpectQuery(selectFrozenVersionSQL).WithArgs(revisionID, int64(11)).WillReturnRows(draftScalarRows().AddRow(
		revisionID, int64(11), int64(1), "frozen", "manual_version", "Historic Title", "Historic Summary", int64(91),
		"historic body", testContentHash, int64(2), createdAt, updatedAt,
	))
}

func expectRestoreCurrentForUpdate(mock sqlmock.Sqlmock, revisionID, revisionNo, lockVersion int64, createdAt, updatedAt time.Time) {
	mock.ExpectQuery(selectCurrentForUpdateSQL).WithArgs(revisionID, int64(11)).WillReturnRows(draftScalarRows().AddRow(
		revisionID, int64(11), revisionNo, "editing", "draft", "Unsaved Current", "Current Summary", nil,
		"current body", testContentHash, lockVersion, createdAt, updatedAt,
	))
}

func expectStoredVersionAssociations(mock sqlmock.Sqlmock, revisionID int64) {
	mock.ExpectQuery(selectDraftTagsSQL).WithArgs(revisionID).WillReturnRows(
		sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).
			AddRow(int64(7), "Historic Go", "t_historic_go", 0).
			AddRow(int64(3), "Historic Web", "t_historic_web", 1),
	)
	mock.ExpectQuery(selectDraftMediaSQL).WithArgs(revisionID).WillReturnRows(
		sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}).
			AddRow(int64(91), firstMediaKey, "cover", 0).
			AddRow(int64(92), secondMediaKey, "content", 1),
	)
}

func expectCopiedVersionAssociations(mock sqlmock.Sqlmock, revisionID int64, at time.Time, firstTagID, secondTagID, firstMediaID, secondMediaID int64) {
	expectCopiedVersionTags(mock, revisionID, at, firstTagID, secondTagID)
	expectCopiedVersionMedia(mock, revisionID, at, firstMediaID, secondMediaID)
}

func expectCopiedVersionTags(mock sqlmock.Sqlmock, revisionID int64, at time.Time, firstTagID, secondTagID int64) {
	mock.ExpectExec(insertDraftTagSQL).
		WithArgs(firstTagID, revisionID, int64(7), "Historic Go", "t_historic_go", 0, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftTagSQL).
		WithArgs(secondTagID, revisionID, int64(3), "Historic Web", "t_historic_web", 1, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectCopiedVersionMedia(mock sqlmock.Sqlmock, revisionID int64, at time.Time, firstMediaID, secondMediaID int64) {
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(firstMediaID, revisionID, int64(91), "cover", 0, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(secondMediaID, revisionID, int64(92), "content", 1, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectCreateVersionThroughAssociations(mock sqlmock.Sqlmock, _ time.Time, createdAt, updatedAt time.Time) {
	mock.ExpectBegin()
	expectCurrentDraftForUpdate(mock, 3, createdAt, updatedAt)
	expectStoredVersionAssociations(mock, 21)
}

func expectCreateVersionThroughFreeze(mock sqlmock.Sqlmock, at, createdAt, updatedAt time.Time) {
	expectCreateVersionThroughAssociations(mock, at, createdAt, updatedAt)
	mock.ExpectExec(freezeManualVersionSQL).WithArgs(at, int64(21), int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectCreateVersionThroughRevisionInsert(mock sqlmock.Sqlmock, at, createdAt, updatedAt time.Time) {
	expectCreateVersionThroughFreeze(mock, at, createdAt, updatedAt)
	mock.ExpectExec(insertEditingVersionSQL).WithArgs(int64(1), int64(11), int64(3), "Title", "Summary", int64(91), "body", testContentHash, at, at).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectCreateVersionThroughCopies(mock sqlmock.Sqlmock, at, createdAt, updatedAt time.Time) {
	expectCreateVersionThroughRevisionInsert(mock, at, createdAt, updatedAt)
	expectCopiedVersionAssociations(mock, 1, at, 1, 2, 1, 2)
}

func expectRestoreThroughCurrent(mock sqlmock.Sqlmock, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt time.Time) {
	mock.ExpectBegin()
	expectFrozenTarget(mock, 15, targetCreatedAt, targetUpdatedAt)
	expectRestoreCurrentForUpdate(mock, 30, 3, 4, currentCreatedAt, currentUpdatedAt)
}

func expectRestoreThroughAssociations(mock sqlmock.Sqlmock, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt time.Time) {
	expectRestoreThroughCurrent(mock, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt)
	expectStoredVersionAssociations(mock, 15)
}

func expectRestoreThroughFreeze(mock sqlmock.Sqlmock, at, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt time.Time) {
	expectRestoreThroughAssociations(mock, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt)
	mock.ExpectExec(freezeManualVersionSQL).WithArgs(at, int64(30), int64(4)).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectRestoreThroughCopies(mock sqlmock.Sqlmock, at, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt time.Time) {
	expectRestoreThroughFreeze(mock, at, targetCreatedAt, targetUpdatedAt, currentCreatedAt, currentUpdatedAt)
	mock.ExpectExec(insertEditingVersionSQL).WithArgs(int64(1), int64(11), int64(4), "Historic Title", "Historic Summary", int64(91), "historic body", testContentHash, at, at).WillReturnResult(sqlmock.NewResult(0, 1))
	expectCopiedVersionAssociations(mock, 1, at, 1, 2, 1, 2)
}

func expectOneFrozenHistoryScalar(mock sqlmock.Sqlmock, at time.Time) {
	mock.ExpectBegin()
	mock.ExpectQuery(listFrozenVersionsSQL).WithArgs(int64(11)).WillReturnRows(draftScalarRows().AddRow(
		int64(15), int64(11), int64(1), "frozen", "manual_version", "Title", "", nil, "body", testContentHash, int64(1), at, at,
	))
}

func exactLimitTagRows() *sqlmock.Rows {
	return storedTagRows(MaxTagCount)
}

func storedTagRows(count int) *sqlmock.Rows {
	rows := emptyStoredTagRows()
	for index := 0; index < count; index++ {
		rows.AddRow(int64(index+1), fmt.Sprintf("Tag %d", index), fmt.Sprintf("t_%d", index), index)
	}
	return rows
}

func storedTagRowsWith(tagID int64, name, slug string, position int) *sqlmock.Rows {
	return emptyStoredTagRows().AddRow(tagID, name, slug, position)
}

func emptyStoredTagRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"})
}

func exactLimitMediaRows() *sqlmock.Rows {
	return storedMediaRows(MaxBodyMediaCount)
}

func storedMediaRows(bodyCount int) *sqlmock.Rows {
	rows := storedMediaRowsWith(91, firstMediaKey, "cover", 0)
	for index := 0; index < bodyCount; index++ {
		rows.AddRow(int64(index+100), mediaKeyForIndex(index), "content", index+1)
	}
	return rows
}

func storedMediaRowsWith(mediaID int64, publicKey, purpose string, position int) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}).
		AddRow(mediaID, publicKey, purpose, position)
}

func expectExactLimitAssociationCopies(mock sqlmock.Sqlmock, revisionID int64, at time.Time) {
	for index := 0; index < MaxTagCount; index++ {
		mock.ExpectExec(insertDraftTagSQL).
			WithArgs(int64(index+1), revisionID, int64(index+1), fmt.Sprintf("Tag %d", index), fmt.Sprintf("t_%d", index), index, at).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectExec(insertDraftMediaSQL).
		WithArgs(int64(1), revisionID, int64(91), "cover", 0, at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	for index := 0; index < MaxBodyMediaCount; index++ {
		mock.ExpectExec(insertDraftMediaSQL).
			WithArgs(int64(index+2), revisionID, int64(index+100), "content", index+1, at).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
}

func versionTagSnapshots() []tag.Snapshot {
	return []tag.Snapshot{
		{TagID: 7, Name: "Historic Go", Slug: "t_historic_go", Position: 0},
		{TagID: 3, Name: "Historic Web", Slug: "t_historic_web", Position: 1},
	}
}

func versionMediaReferences() []media.Reference {
	return []media.Reference{
		{MediaID: 91, PublicKey: firstMediaKey, Purpose: "cover", Position: 0},
		{MediaID: 92, PublicKey: secondMediaKey, Purpose: "content", Position: 1},
	}
}

func revisionInt64Pointer(value int64) *int64 { return &value }
