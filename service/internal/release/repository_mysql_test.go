package release

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/stretchr/testify/require"
)

const (
	testSiteStateForUpdateSQL      = "SELECT current_release_id, active_publish_job_id FROM site_state WHERE singleton_key = ? FOR UPDATE"
	testInsertSiteStateSQL         = "INSERT INTO site_state (id, singleton_key, current_release_id, active_publish_job_id) VALUES (?, 1, NULL, NULL)"
	testInsertReleaseSQL           = "INSERT INTO releases (id, site_snapshot_json, checksum, status) VALUES (?, ?, ?, 'queued')"
	testInsertReleaseArticleSQL    = "INSERT INTO release_articles (id, release_id, article_id, revision_id, slug, title, summary, content_md, content_hash, published_at, tags_snapshot_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	testInsertPublishJobSQL        = "INSERT INTO publish_jobs (id, release_id, builder_id, status, stage, error_summary) VALUES (?, ?, ?, 'pending', 'pending', '')"
	testSetActiveJobSQL            = "UPDATE site_state SET active_publish_job_id = ? WHERE singleton_key = ? AND active_publish_job_id IS NULL"
	testReleaseSelectSQL           = "SELECT id, site_snapshot_json, checksum, status, created_at, completed_at FROM releases WHERE id = ?"
	testReleaseForUpdateSQL        = "SELECT id, site_snapshot_json, checksum, status, created_at, completed_at FROM releases WHERE id = ? FOR UPDATE"
	testReleaseListSQL             = "SELECT id, site_snapshot_json, checksum, status, created_at, completed_at FROM releases ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	testReleaseArticlesSelectSQL   = "SELECT article_id, revision_id, slug, title, summary, content_md, content_hash, published_at, tags_snapshot_json FROM release_articles WHERE release_id = ? ORDER BY article_id ASC"
	testJobsSelectSQL              = "SELECT id, release_id, builder_id, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE release_id = ? ORDER BY created_at DESC, id DESC"
	testJobForUpdateSQL            = "SELECT id, release_id, builder_id, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE id = ? AND release_id = ? FOR UPDATE"
	testJobByBuildForUpdateSQL     = "SELECT id, release_id, builder_id, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE release_id = ? AND build_number = ? ORDER BY created_at DESC, id DESC LIMIT 1 FOR UPDATE"
	testPriorJobCountForUpdateSQL  = "SELECT COUNT(*) FROM publish_jobs WHERE release_id = ? AND id <> ? FOR UPDATE"
	testLatestFinalJobForUpdateSQL = "SELECT id, release_id, builder_id, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE release_id = ? AND status IN ('success', 'failed') ORDER BY created_at DESC, id DESC LIMIT 1 FOR UPDATE"
	testUpdateJobSQL               = "UPDATE publish_jobs SET status = ?, stage = ?, build_number = ?, error_summary = ?, finished_at = ? WHERE id = ? AND status = ?"
	testUpdateReleaseFinalSQL      = "UPDATE releases SET status = ?, completed_at = ? WHERE id = ?"
	testPublishArticlePointersSQL  = "UPDATE articles a LEFT JOIN release_articles ra ON ra.release_id = ? AND ra.article_id = a.id SET a.published_revision_id = ra.revision_id, a.updated_at = ? WHERE a.published_revision_id IS NOT NULL OR ra.revision_id IS NOT NULL"
	testCompleteSiteStateSQL       = "UPDATE site_state SET current_release_id = ?, active_publish_job_id = NULL WHERE singleton_key = ? AND active_publish_job_id = ?"
	testFailSiteStateSQL           = "UPDATE site_state SET active_publish_job_id = NULL WHERE singleton_key = ? AND active_publish_job_id = ?"
	testMaxReleaseIDSQL            = "SELECT MAX(id) FROM releases"
)

func TestMySQLRepositoryCreateLockedPersistsPreparedImmutableSnapshotWithSharedIDs(t *testing.T) {
	repo, mock, source, counter := newRepositoryTest(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	snapshot := validPreparedSnapshot(now)
	source.prepared = snapshot
	siteJSON, articleTagsJSON := snapshotJSON(t, snapshot)

	mock.ExpectBegin()
	mock.ExpectQuery(testSiteStateForUpdateSQL).WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"current_release_id", "active_publish_job_id"}).AddRow(nil, nil))
	mock.ExpectExec(testInsertReleaseSQL).WithArgs(int64(1), siteJSON, snapshot.Checksum).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testInsertReleaseArticleSQL).WithArgs(
		int64(1), int64(1), int64(41), int64(71), "article_slug", "Title", "Summary", "Body",
		"sha256:"+strings.Repeat("b", 64), now, articleTagsJSON,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testInsertPublishJobSQL).WithArgs(int64(1), int64(1), int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testSetActiveJobSQL).WithArgs(int64(1), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseQuery(mock, 1, snapshot, now, ReleaseQueued, nil)
	expectJobsQuery(mock, 1, []jobRow{{id: 1, releaseID: 1, builderID: 9, status: JobPending, stage: "pending", createdAt: now}})
	mock.ExpectCommit()

	created, job, err := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9, RequestedBy: 3})

	require.NoError(t, err)
	require.Equal(t, int64(1), created.ID)
	require.Equal(t, snapshot.Checksum, created.Checksum)
	require.Equal(t, int64(1), job.ID)
	require.Equal(t, int64(1), job.ReleaseID)
	require.Equal(t, []string{"idseq:releases", "idseq:release_articles", "idseq:publish_jobs"}, counter.keys)
	require.Len(t, source.requests, 1)
	require.Equal(t, PublishArticle, source.requests[0].Mode)
	require.Equal(t, int64(41), source.requests[0].ArticleID)
	require.Zero(t, source.requests[0].CurrentReleaseID)
	require.Empty(t, source.requests[0].Base.Articles)
}

func TestMySQLRepositoryCreateLockedCreatesMissingSingletonInsideTransaction(t *testing.T) {
	repo, mock, source, counter := newRepositoryTest(t)
	source.err = errors.New("snapshot-source-secret")
	mock.ExpectBegin()
	mock.ExpectQuery(testSiteStateForUpdateSQL).WithArgs(1).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(testInsertSiteStateSQL).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(testSiteStateForUpdateSQL).WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"current_release_id", "active_publish_job_id"}).AddRow(nil, nil))
	mock.ExpectRollback()

	_, _, err := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishSettings, BuilderID: 9})

	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), "snapshot-source-secret")
	require.Equal(t, []string{"idseq:site_state"}, counter.keys)
}

func TestMySQLRepositoryCreateLockedRejectsBusyAndInvalidPreparedSnapshotBeforeIDs(t *testing.T) {
	t.Run("busy", func(t *testing.T) {
		repo, mock, source, counter := newRepositoryTest(t)
		mock.ExpectBegin()
		mock.ExpectQuery(testSiteStateForUpdateSQL).WithArgs(1).
			WillReturnRows(sqlmock.NewRows([]string{"current_release_id", "active_publish_job_id"}).AddRow(nil, int64(17)))
		mock.ExpectRollback()
		_, _, err := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishSettings, BuilderID: 9})
		require.ErrorIs(t, err, ErrBusy)
		require.Empty(t, source.requests)
		require.Empty(t, counter.keys)
	})

	t.Run("checksum", func(t *testing.T) {
		repo, mock, source, counter := newRepositoryTest(t)
		source.prepared = validPreparedSnapshot(time.Now())
		source.prepared.Checksum = "sha256:CHECKSUM-SECRET"
		mock.ExpectBegin()
		mock.ExpectQuery(testSiteStateForUpdateSQL).WithArgs(1).
			WillReturnRows(sqlmock.NewRows([]string{"current_release_id", "active_publish_job_id"}).AddRow(nil, nil))
		mock.ExpectRollback()
		_, _, err := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9})
		require.ErrorIs(t, err, ErrInvalidSnapshot)
		require.NotContains(t, err.Error(), "CHECKSUM-SECRET")
		require.Empty(t, counter.keys)
	})
}

func TestMySQLRepositoryCreateLockedHandlesIDFailuresHealingAndBusinessUniqueErrors(t *testing.T) {
	t.Run("ID counter failure happens before insert", func(t *testing.T) {
		repo, mock, source, counter := newRepositoryTest(t)
		source.prepared = emptyPreparedSnapshot()
		counter.err = errors.New("redis-counter-secret")
		mock.ExpectBegin()
		expectSiteState(mock, nil, nil)
		mock.ExpectRollback()

		_, _, err := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishSettings, BuilderID: 9})

		require.ErrorIs(t, err, ErrDependencyUnavailable)
		require.NotContains(t, err.Error(), "redis-counter-secret")
		require.Equal(t, []string{"idseq:releases"}, counter.keys)
	})

	t.Run("PRIMARY conflict heals and retries shared release ID", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, mock.ExpectationsWereMet())
			_ = db.Close()
		})
		counter := &tableCounter{}
		ids, err := idgen.New(counter, db, 1, 1, true)
		require.NoError(t, err)
		source := &snapshotSourceFake{prepared: emptyPreparedSnapshot()}
		repo := NewMySQLRepository(db, ids, source)
		now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
		siteJSON, _ := json.Marshal(source.prepared.Site)
		mock.ExpectBegin()
		expectSiteState(mock, nil, nil)
		mock.ExpectExec(testInsertReleaseSQL).WithArgs(int64(1), string(siteJSON), source.prepared.Checksum).WillReturnError(primaryDuplicate())
		mock.ExpectQuery(testMaxReleaseIDSQL).WillReturnRows(sqlmock.NewRows([]string{"MAX(id)"}).AddRow(int64(10)))
		mock.ExpectExec(testInsertReleaseSQL).WithArgs(int64(11), string(siteJSON), source.prepared.Checksum).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(testInsertPublishJobSQL).WithArgs(int64(1), int64(11), int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(testSetActiveJobSQL).WithArgs(int64(1), 1).WillReturnResult(sqlmock.NewResult(0, 1))
		expectReleaseQuery(mock, 11, source.prepared, now, ReleaseQueued, nil)
		expectJobsQuery(mock, 11, []jobRow{{id: 1, releaseID: 11, builderID: 9, status: JobPending, stage: "pending", createdAt: now}})
		mock.ExpectCommit()

		created, _, err := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishSettings, BuilderID: 9})

		require.NoError(t, err)
		require.Equal(t, int64(11), created.ID)
		require.Equal(t, []raiseCall{{key: "idseq:releases", floor: 10}}, counter.raises)
	})

	t.Run("named article unique does not heal", func(t *testing.T) {
		repo, mock, source, counter := newRepositoryTest(t)
		now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
		source.prepared = validPreparedSnapshot(now)
		siteJSON, tagsJSON := snapshotJSON(t, source.prepared)
		mock.ExpectBegin()
		expectSiteState(mock, nil, nil)
		mock.ExpectExec(testInsertReleaseSQL).WithArgs(int64(1), siteJSON, source.prepared.Checksum).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(testInsertReleaseArticleSQL).WithArgs(
			int64(1), int64(1), int64(41), int64(71), "article_slug", "Title", "Summary", "Body",
			"sha256:"+strings.Repeat("b", 64), now, tagsJSON,
		).WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry for key 'uk_release_articles_article'"})
		mock.ExpectRollback()

		_, _, err := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9})

		require.ErrorIs(t, err, ErrDependencyUnavailable)
		require.Empty(t, counter.raises)
	})
}

func TestValidatePreparedSnapshotEnforcesModePreservationAndDeepCopies(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	first := validPreparedSnapshot(now).Articles[0]
	second := first
	second.ArticleID, second.RevisionID, second.Slug = 42, 72, "second_slug_"
	base := PreparedSnapshot{Site: SiteSnapshot{Name: "old"}, Articles: []ArticleSnapshot{first, second}, Checksum: "sha256:" + strings.Repeat("a", 64)}

	settings := clonePreparedSnapshot(base)
	settings.Site.Name = "new"
	settings.Checksum = "sha256:" + strings.Repeat("c", 64)
	require.NoError(t, validatePreparedSnapshot(CreateCommand{Mode: PublishSettings, BuilderID: 9}, base, settings))

	unpublish := clonePreparedSnapshot(settings)
	unpublish.Articles = []ArticleSnapshot{second}
	require.NoError(t, validatePreparedSnapshot(CreateCommand{Mode: UnpublishArticle, ArticleID: 41, BuilderID: 9}, base, unpublish))

	publish := clonePreparedSnapshot(settings)
	publish.Articles[0].RevisionID = 73
	require.NoError(t, validatePreparedSnapshot(CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9}, base, publish))

	for name, prepared := range map[string]PreparedSnapshot{
		"settings changes article": publish,
		"unpublish keeps target":   settings,
		"publish changes other": func() PreparedSnapshot {
			value := clonePreparedSnapshot(publish)
			value.Articles[1].RevisionID++
			return value
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			command := CreateCommand{Mode: PublishSettings, BuilderID: 9}
			if strings.HasPrefix(name, "unpublish") {
				command.Mode, command.ArticleID = UnpublishArticle, 41
			} else if strings.HasPrefix(name, "publish") {
				command.Mode, command.ArticleID = PublishArticle, 41
			}
			require.ErrorIs(t, validatePreparedSnapshot(command, base, prepared), ErrInvalidSnapshot)
		})
	}

	copy := clonePreparedSnapshot(base)
	copy.Site.SocialLinks = append(copy.Site.SocialLinks, SocialLink{Label: "X", URL: "https://example.com"})
	copy.Articles[0].Tags[0].Name = "changed"
	require.Empty(t, base.Site.SocialLinks)
	require.Equal(t, "Go", base.Articles[0].Tags[0].Name)
}

func TestPreparedSnapshotNormalizesArraysButStoredNullArraysAreRejected(t *testing.T) {
	prepared := validPreparedSnapshot(time.Now().UTC())
	prepared.Site.SocialLinks = nil
	prepared.Articles[0].Tags = nil
	prepared = normalizePreparedSnapshot(prepared)
	require.NotNil(t, prepared.Site.SocialLinks)
	require.NotNil(t, prepared.Articles)
	require.NotNil(t, prepared.Articles[0].Tags)

	var site SiteSnapshot
	require.Error(t, decodeSiteSnapshot([]byte(`{"name":"Blog","socialLinks":null}`), &site))
	var tags []TagSnapshot
	require.Error(t, decodeTags([]byte(`null`), &tags))
}

func TestMySQLRepositoryCreateRetryLockedReturnsUpdatedAggregateFromOneTransaction(t *testing.T) {
	repo, mock, _, counter := newRepositoryTest(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	snapshot := validPreparedSnapshot(now)
	failedAt := now.Add(-time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery(testSiteStateForUpdateSQL).WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"current_release_id", "active_publish_job_id"}).AddRow(nil, nil))
	expectReleaseQuery(mock, 7, snapshot, now.Add(-time.Hour), ReleaseFailed, &failedAt)
	expectJobsQuery(mock, 7, []jobRow{{id: 11, releaseID: 7, builderID: 9, status: JobFailed, stage: "deploy", createdAt: now.Add(-time.Hour), finishedAt: &failedAt}})
	mock.ExpectExec(testInsertPublishJobSQL).WithArgs(int64(1), int64(7), int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testSetActiveJobSQL).WithArgs(int64(1), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseQuery(mock, 7, snapshot, now.Add(-time.Hour), ReleaseFailed, &failedAt)
	expectJobsQuery(mock, 7, []jobRow{
		{id: 1, releaseID: 7, builderID: 9, status: JobPending, stage: "pending", createdAt: now},
		{id: 11, releaseID: 7, builderID: 9, status: JobFailed, stage: "deploy", createdAt: now.Add(-time.Hour), finishedAt: &failedAt},
	})
	mock.ExpectCommit()

	aggregate, created, err := repo.CreateRetryLocked(context.Background(), 7)

	require.NoError(t, err)
	require.NoError(t, aggregate.ValidateRetry(created))
	require.Equal(t, []int64{1, 11}, []int64{aggregate.Jobs[0].ID, aggregate.Jobs[1].ID})
	require.Equal(t, []string{"idseq:publish_jobs"}, counter.keys)
}

func TestMySQLRepositoryApplyCallbackLockedTransitionsAndAdvancesPointersOnlyOnSuccess(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	t.Run("nonfinal", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		build := int64(44)
		mock.ExpectBegin()
		expectSiteState(mock, nil, int64(12))
		expectJobForUpdate(mock, jobRow{id: 12, releaseID: 7, builderID: 9, status: JobQueued, stage: "queue", buildNumber: &build, createdAt: now.Add(-time.Minute)})
		mock.ExpectExec(testUpdateJobSQL).WithArgs(JobBuilding, "build", int64(44), "safe summary", nil, int64(12), JobQueued).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		job, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, BuildNumber: 44, Stage: "build", Status: JobBuilding, ErrorSummary: "safe\nsummary", Timestamp: now})
		require.NoError(t, err)
		require.False(t, duplicate)
		require.Equal(t, JobBuilding, job.Status)
		require.Equal(t, "safe summary", job.ErrorSummary)
	})

	t.Run("success", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		build := int64(44)
		mock.ExpectBegin()
		expectSiteState(mock, nil, int64(12))
		expectJobForUpdate(mock, jobRow{id: 12, releaseID: 7, builderID: 9, status: JobDeploying, stage: "deploy", buildNumber: &build, createdAt: now.Add(-time.Minute)})
		mock.ExpectExec(testUpdateJobSQL).WithArgs(JobSuccess, "deploy", int64(44), "", now, int64(12), JobDeploying).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(testUpdateReleaseFinalSQL).WithArgs(ReleaseSuccess, now, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(testPublishArticlePointersSQL).WithArgs(int64(7), now).WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectExec(testCompleteSiteStateSQL).WithArgs(int64(7), 1, int64(12)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		job, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, BuildNumber: 44, Stage: "deploy", Status: JobSuccess, Timestamp: now})
		require.NoError(t, err)
		require.False(t, duplicate)
		require.Equal(t, JobSuccess, job.Status)
	})

	t.Run("failure", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, int64(3), int64(12))
		expectJobForUpdate(mock, jobRow{id: 12, releaseID: 7, builderID: 9, status: JobPending, stage: "pending", createdAt: now.Add(-time.Minute)})
		longSummary := strings.Repeat("密", 520)
		mock.ExpectQuery(testPriorJobCountForUpdateSQL).WithArgs(int64(7), int64(12)).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
		mock.ExpectExec(testUpdateJobSQL).WithArgs(JobFailed, "trigger", nil, strings.Repeat("密", 512), now, int64(12), JobPending).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(testUpdateReleaseFinalSQL).WithArgs(ReleaseFailed, now, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(testFailSiteStateSQL).WithArgs(1, int64(12)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		_, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, Stage: "trigger", Status: JobFailed, ErrorSummary: longSummary, Timestamp: now})
		require.NoError(t, err)
		require.False(t, duplicate)
	})
}

func TestMySQLRepositoryApplyCallbackLockedIsIdempotentAndRejectsInvalidOrder(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	build := int64(44)
	t.Run("duplicate", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, nil, int64(12))
		expectJobForUpdate(mock, jobRow{id: 12, releaseID: 7, builderID: 9, status: JobBuilding, stage: "build", buildNumber: &build, errorSummary: "same", createdAt: now.Add(-time.Minute)})
		mock.ExpectCommit()
		_, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, BuildNumber: 44, Stage: "build", Status: JobBuilding, ErrorSummary: "same", Timestamp: now})
		require.NoError(t, err)
		require.True(t, duplicate)
	})

	t.Run("final replay with unspecified build preserves stored build", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, int64(7), nil)
		mock.ExpectRollback()

		job, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, Stage: "deploy", Status: JobSuccess, Timestamp: now})
		require.ErrorIs(t, err, ErrConflict)
		require.False(t, duplicate)
		require.Zero(t, job.ID)
	})

	t.Run("invalid", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, nil, int64(12))
		expectJobForUpdate(mock, jobRow{id: 12, releaseID: 7, builderID: 9, status: JobQueued, stage: "queue", buildNumber: &build, createdAt: now.Add(-time.Minute)})
		mock.ExpectRollback()
		_, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, BuildNumber: 44, Stage: "deploy", Status: JobSuccess, Timestamp: now})
		require.ErrorIs(t, err, ErrConflict)
		require.False(t, duplicate)
	})

	t.Run("old failed job callback does not finalize active retry", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, nil, int64(13))
		expectJobForUpdate(mock, jobRow{id: 13, releaseID: 7, builderID: 9, status: JobPending, stage: "pending", createdAt: now})
		mock.ExpectQuery(testJobByBuildForUpdateSQL).WithArgs(int64(7), int64(44)).WillReturnRows(
			sqlmock.NewRows([]string{"id", "release_id", "builder_id", "status", "stage", "build_number", "error_summary", "created_at", "finished_at"}).
				AddRow(int64(12), int64(7), int64(9), JobFailed, "deploy", int64(44), "failed", now.Add(-time.Hour), now.Add(-time.Minute)),
		)
		mock.ExpectCommit()

		job, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, BuildNumber: 44, Stage: "deploy", Status: JobFailed, ErrorSummary: "failed", Timestamp: now.Add(-time.Minute)})
		require.NoError(t, err)
		require.True(t, duplicate)
		require.Equal(t, int64(12), job.ID)
	})

	t.Run("old buildless trigger failure does not finalize active retry", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		oldEventAt := now.Add(-time.Hour)
		mock.ExpectBegin()
		expectSiteState(mock, nil, int64(13))
		expectJobForUpdate(mock, jobRow{id: 13, releaseID: 7, builderID: 9, status: JobPending, stage: "pending", createdAt: now})
		mock.ExpectQuery(testPriorJobCountForUpdateSQL).WithArgs(int64(7), int64(13)).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(1))
		mock.ExpectRollback()

		job, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, Stage: "trigger", Status: JobFailed, ErrorSummary: "failed", Timestamp: oldEventAt})
		require.ErrorIs(t, err, ErrConflict)
		require.False(t, duplicate)
		require.Zero(t, job.ID)
	})

	t.Run("old buildless nonfinal callback cannot advance active retry", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, nil, int64(13))
		expectJobForUpdate(mock, jobRow{id: 13, releaseID: 7, builderID: 9, status: JobPending, stage: "pending", createdAt: now})
		mock.ExpectQuery(testPriorJobCountForUpdateSQL).WithArgs(int64(7), int64(13)).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(1))
		mock.ExpectRollback()

		_, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, Stage: "queue", Status: JobQueued, Timestamp: now.Add(-time.Hour)})
		require.ErrorIs(t, err, ErrConflict)
		require.False(t, duplicate)
	})

	t.Run("old positive-build final callback remains idempotent after later job", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		oldFinished := now.Add(-time.Hour)
		mock.ExpectBegin()
		expectSiteState(mock, int64(7), nil)
		mock.ExpectQuery(testJobByBuildForUpdateSQL).WithArgs(int64(7), int64(44)).WillReturnRows(
			sqlmock.NewRows([]string{"id", "release_id", "builder_id", "status", "stage", "build_number", "error_summary", "created_at", "finished_at"}).
				AddRow(int64(12), int64(7), int64(9), JobFailed, "deploy", int64(44), "failed", oldFinished.Add(-time.Minute), oldFinished),
		)
		mock.ExpectCommit()

		job, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, BuildNumber: 44, Stage: "deploy", Status: JobFailed, ErrorSummary: "failed", Timestamp: oldFinished})
		require.NoError(t, err)
		require.True(t, duplicate)
		require.Equal(t, int64(12), job.ID)
	})
}

func TestMySQLRepositoryApplyCallbackLockedAcceptsExactNoChangeReleaseFinalization(t *testing.T) {
	inputAt := time.Date(2026, 8, 14, 12, 0, 0, 123456789, time.UTC)
	now := inputAt.Truncate(time.Microsecond)
	completed := now
	repo, mock, _, _ := newRepositoryTest(t)
	mock.ExpectBegin()
	expectSiteState(mock, int64(3), int64(13))
	expectJobForUpdate(mock, jobRow{id: 13, releaseID: 7, builderID: 9, status: JobPending, stage: "pending", createdAt: now.Add(-time.Minute)})
	mock.ExpectQuery(testPriorJobCountForUpdateSQL).WithArgs(int64(7), int64(13)).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
	mock.ExpectExec(testUpdateJobSQL).WithArgs(JobFailed, "trigger", nil, "", now, int64(13), JobPending).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testUpdateReleaseFinalSQL).WithArgs(ReleaseFailed, now, int64(7)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(testReleaseForUpdateSQL).WithArgs(int64(7)).WillReturnRows(
		releaseRowsNoTest(7, validPreparedSnapshot(now), now.Add(-time.Hour), ReleaseFailed, &completed),
	)
	mock.ExpectExec(testFailSiteStateSQL).WithArgs(1, int64(13)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	_, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, Stage: "trigger", Status: JobFailed, Timestamp: inputAt})
	require.NoError(t, err)
	require.False(t, duplicate)
}

func TestMySQLRepositoryReconcileLockedKeepsDependencyFailuresDistinct(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	snapshot := validPreparedSnapshot(now.Add(-time.Hour))
	repo, mock, _, _ := newRepositoryTest(t)
	mock.ExpectBegin()
	expectSiteState(mock, int64(3), int64(12))
	mock.ExpectQuery(testReleaseForUpdateSQL).WithArgs(int64(7)).WillReturnRows(releaseRowsNoTest(7, snapshot, now.Add(-time.Hour), ReleaseQueued, nil))
	mock.ExpectQuery(testJobForUpdateSQL).WithArgs(int64(12), int64(7)).WillReturnError(errors.New("mysql-private-host"))
	mock.ExpectRollback()

	changed, err := repo.ReconcileLocked(context.Background(), Artifact{ReleaseID: 7, Checksum: snapshot.Checksum, BuildNumber: 44, DeployedAt: now})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotErrorIs(t, err, ErrReconciliationRequired)
	require.NotContains(t, err.Error(), "mysql-private-host")
	require.False(t, changed)
}

func TestMySQLRepositoryFindAndListReturnValidatedDeterministicAggregates(t *testing.T) {
	repo, mock, _, _ := newRepositoryTest(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	snapshot := validPreparedSnapshot(now)
	mock.ExpectBegin()
	expectReleaseQuery(mock, 7, snapshot, now, ReleaseQueued, nil)
	expectJobsQuery(mock, 7, []jobRow{{id: 12, releaseID: 7, builderID: 9, status: JobBuilding, stage: "build", createdAt: now}})
	mock.ExpectCommit()
	got, err := repo.FindRelease(context.Background(), 7)
	require.NoError(t, err)
	require.NoError(t, got.Validate())

	mock.ExpectBegin()
	mock.ExpectQuery(testReleaseListSQL).WithArgs(20, 0).WillReturnRows(releaseRows(t,
		releaseRow{id: 8, snapshot: snapshot, status: ReleaseQueued, createdAt: now.Add(time.Minute)},
		releaseRow{id: 7, snapshot: snapshot, status: ReleaseQueued, createdAt: now},
	))
	expectJobsQuery(mock, 8, []jobRow{{id: 13, releaseID: 8, builderID: 9, status: JobPending, stage: "pending", createdAt: now.Add(time.Minute)}})
	expectJobsQuery(mock, 7, []jobRow{{id: 12, releaseID: 7, builderID: 9, status: JobBuilding, stage: "build", createdAt: now}})
	mock.ExpectCommit()
	items, err := repo.ListReleases(context.Background(), ListQuery{Limit: 20})
	require.NoError(t, err)
	require.Equal(t, []int64{8, 7}, []int64{items[0].Release.ID, items[1].Release.ID})
}

func TestMySQLRepositoryReconcileLockedAdvancesADeployedReleaseAtomically(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	snapshot := validPreparedSnapshot(now.Add(-time.Hour))
	build := int64(44)
	repo, mock, _, _ := newRepositoryTest(t)
	mock.ExpectBegin()
	expectSiteState(mock, int64(3), int64(12))
	mock.ExpectQuery(testReleaseForUpdateSQL).WithArgs(int64(7)).WillReturnRows(releaseRowsNoTest(7, snapshot, now.Add(-time.Hour), ReleaseQueued, nil))
	expectJobForUpdate(mock, jobRow{id: 12, releaseID: 7, builderID: 9, status: JobDeploying, stage: "deploy", buildNumber: &build, createdAt: now.Add(-time.Minute)})
	mock.ExpectExec(testUpdateJobSQL).WithArgs(JobSuccess, "deploy", int64(44), "", now, int64(12), JobDeploying).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testUpdateReleaseFinalSQL).WithArgs(ReleaseSuccess, now, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testPublishArticlePointersSQL).WithArgs(int64(7), now).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testCompleteSiteStateSQL).WithArgs(int64(7), 1, int64(12)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	changed, err := repo.ReconcileLocked(context.Background(), Artifact{ReleaseID: 7, Checksum: snapshot.Checksum, BuildNumber: 44, DeployedAt: now})

	require.NoError(t, err)
	require.True(t, changed)
}

func TestMySQLRepositoryReconcileLockedIsIdempotentAndRejectsMismatch(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	snapshot := validPreparedSnapshot(now.Add(-time.Hour))
	build := int64(44)
	completed := now.Add(-time.Minute)

	t.Run("already current", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, int64(7), nil)
		mock.ExpectQuery(testReleaseForUpdateSQL).WithArgs(int64(7)).WillReturnRows(releaseRowsNoTest(7, snapshot, now.Add(-time.Hour), ReleaseSuccess, &completed))
		expectJobsQuery(mock, 7, []jobRow{{id: 12, releaseID: 7, builderID: 9, status: JobSuccess, stage: "deploy", buildNumber: &build, createdAt: now.Add(-time.Hour), finishedAt: &completed}})
		mock.ExpectCommit()

		changed, err := repo.ReconcileLocked(context.Background(), Artifact{ReleaseID: 7, Checksum: snapshot.Checksum, BuildNumber: 44, DeployedAt: now})
		require.NoError(t, err)
		require.False(t, changed)
	})

	t.Run("checksum mismatch", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, int64(3), int64(12))
		mock.ExpectQuery(testReleaseForUpdateSQL).WithArgs(int64(7)).WillReturnRows(releaseRowsNoTest(7, snapshot, now.Add(-time.Hour), ReleaseQueued, nil))
		mock.ExpectRollback()

		changed, err := repo.ReconcileLocked(context.Background(), Artifact{ReleaseID: 7, Checksum: "sha256:" + strings.Repeat("c", 64), BuildNumber: 44, DeployedAt: now})
		require.ErrorIs(t, err, ErrReconciliationRequired)
		require.False(t, changed)
	})
}

func TestMySQLRepositoryRejectsNilDependenciesAndSanitizesFailures(t *testing.T) {
	db, _, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	validIDs, err := idgen.New(&tableCounter{}, nil, 1, 1, false)
	require.NoError(t, err)
	validSource := &snapshotSourceFake{}
	var typedNilSource *snapshotSourceFake
	for name, repo := range map[string]*MySQLRepository{
		"nil receiver": nil,
		"nil db":       NewMySQLRepository(nil, validIDs, validSource),
		"nil ids":      NewMySQLRepository(db, nil, validSource),
		"zero ids":     NewMySQLRepository(db, &idgen.Generator{}, validSource),
		"nil source":   NewMySQLRepository(db, validIDs, nil),
		"typed source": NewMySQLRepository(db, validIDs, typedNilSource),
		"zero value":   {},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, callErr := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishSettings, BuilderID: 9})
			require.Error(t, callErr)
			require.NotPanics(t, func() { _, _ = repo.FindRelease(context.Background(), 1) })
		})
	}
}

type snapshotSourceFake struct {
	mu       sync.Mutex
	prepared PreparedSnapshot
	err      error
	requests []SnapshotRequest
}

func (s *snapshotSourceFake) PrepareSnapshot(_ context.Context, request SnapshotRequest) (PreparedSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, cloneSnapshotRequest(request))
	if s.err != nil {
		return PreparedSnapshot{}, s.err
	}
	return clonePreparedSnapshot(s.prepared), nil
}

type tableCounter struct {
	mu     sync.Mutex
	keys   []string
	next   map[string]int64
	err    error
	raises []raiseCall
}

type raiseCall struct {
	key   string
	floor int64
}

func (c *tableCounter) Increment(_ context.Context, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = append(c.keys, key)
	if c.err != nil {
		return 0, c.err
	}
	if c.next == nil {
		c.next = map[string]int64{}
	}
	c.next[key]++
	return c.next[key], nil
}

func (c *tableCounter) Raise(_ context.Context, key string, floor int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raises = append(c.raises, raiseCall{key: key, floor: floor})
	if c.next == nil {
		c.next = map[string]int64{}
	}
	if c.next[key] < floor {
		c.next[key] = floor
	}
	return c.next[key], nil
}

func newRepositoryTest(t *testing.T) (*MySQLRepository, sqlmock.Sqlmock, *snapshotSourceFake, *tableCounter) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	counter := &tableCounter{}
	ids, err := idgen.New(counter, nil, 1, 1, false)
	require.NoError(t, err)
	source := &snapshotSourceFake{}
	return NewMySQLRepository(db, ids, source), mock, source, counter
}

type releaseRow struct {
	id        int64
	snapshot  PreparedSnapshot
	status    ReleaseStatus
	createdAt time.Time
	completed *time.Time
}

func releaseRows(t *testing.T, values ...releaseRow) *sqlmock.Rows {
	t.Helper()
	rows := sqlmock.NewRows([]string{"id", "site_snapshot_json", "checksum", "status", "created_at", "completed_at"})
	for _, value := range values {
		siteJSON, _ := snapshotJSON(t, value.snapshot)
		rows.AddRow(value.id, siteJSON, value.snapshot.Checksum, value.status, value.createdAt, value.completed)
	}
	return rows
}

func expectReleaseQuery(mock sqlmock.Sqlmock, id int64, snapshot PreparedSnapshot, createdAt time.Time, status ReleaseStatus, completed *time.Time) {
	mock.ExpectQuery(testReleaseSelectSQL).WithArgs(id).
		WillReturnRows(releaseRowsNoTest(id, snapshot, createdAt, status, completed))
}

func releaseRowsNoTest(id int64, snapshot PreparedSnapshot, createdAt time.Time, status ReleaseStatus, completed *time.Time) *sqlmock.Rows {
	siteJSON, _ := json.Marshal(snapshot.Site)
	return sqlmock.NewRows([]string{"id", "site_snapshot_json", "checksum", "status", "created_at", "completed_at"}).
		AddRow(id, string(siteJSON), snapshot.Checksum, status, createdAt, completed)
}

type jobRow struct {
	id, releaseID, builderID int64
	status                   JobStatus
	stage, errorSummary      string
	buildNumber              *int64
	createdAt                time.Time
	finishedAt               *time.Time
}

func expectJobsQuery(mock sqlmock.Sqlmock, releaseID int64, jobs []jobRow) {
	rows := sqlmock.NewRows([]string{"id", "release_id", "builder_id", "status", "stage", "build_number", "error_summary", "created_at", "finished_at"})
	for _, job := range jobs {
		rows.AddRow(job.id, job.releaseID, job.builderID, job.status, job.stage, job.buildNumber, job.errorSummary, job.createdAt, job.finishedAt)
	}
	mock.ExpectQuery(testJobsSelectSQL).WithArgs(releaseID).WillReturnRows(rows)
}

func expectSiteState(mock sqlmock.Sqlmock, current any, active any) {
	mock.ExpectQuery(testSiteStateForUpdateSQL).WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"current_release_id", "active_publish_job_id"}).AddRow(current, active))
}

func expectJobForUpdate(mock sqlmock.Sqlmock, job jobRow) {
	mock.ExpectQuery(testJobForUpdateSQL).WithArgs(job.id, job.releaseID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "release_id", "builder_id", "status", "stage", "build_number", "error_summary", "created_at", "finished_at"}).
			AddRow(job.id, job.releaseID, job.builderID, job.status, job.stage, job.buildNumber, job.errorSummary, job.createdAt, job.finishedAt))
}

func validPreparedSnapshot(now time.Time) PreparedSnapshot {
	return PreparedSnapshot{
		Site: SiteSnapshot{Name: "Blog", AuthorBio: "Bio", AboutMarkdown: "About", FilingName: "ICP", FilingNumber: "ICP-1", SocialLinks: []SocialLink{}},
		Articles: []ArticleSnapshot{{
			ArticleID: 41, RevisionID: 71, Slug: "article_slug", Title: "Title", Summary: "Summary",
			ContentMarkdown: "Body", ContentHash: "sha256:" + strings.Repeat("b", 64), PublishedAt: now,
			Tags: []TagSnapshot{{ID: 5, Name: "Go", Slug: "go"}},
		}},
		Checksum: "sha256:" + strings.Repeat("a", 64),
	}
}

func emptyPreparedSnapshot() PreparedSnapshot {
	return PreparedSnapshot{
		Site:     SiteSnapshot{Name: "Blog", SocialLinks: []SocialLink{}},
		Articles: []ArticleSnapshot{},
		Checksum: "sha256:" + strings.Repeat("a", 64),
	}
}

func snapshotJSON(t *testing.T, snapshot PreparedSnapshot) (string, string) {
	t.Helper()
	siteJSON, err := json.Marshal(snapshot.Site)
	require.NoError(t, err)
	tagsJSON, err := json.Marshal(snapshot.Articles[0].Tags)
	require.NoError(t, err)
	return string(siteJSON), string(tagsJSON)
}

func primaryDuplicate() error {
	return &mysql.MySQLError{Number: 1062, Message: "Duplicate entry '1' for key 'PRIMARY'"}
}
