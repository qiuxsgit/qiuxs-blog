package release

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
	"github.com/stretchr/testify/require"
)

func TestReleaseTagSlugMatchesRandomKeyAndDDLContract(t *testing.T) {
	generator, err := randomkey.New(bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}))
	require.NoError(t, err)
	slug, err := generator.TagSlug()
	require.NoError(t, err)
	require.Equal(t, "t_abcdefghijkl", slug)

	snapshot := validPreparedSnapshot(time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	snapshot.Articles[0].Tags[0].Slug = slug
	require.NoError(t, validatePreparedSnapshot(CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9}, PreparedSnapshot{Articles: []ArticleSnapshot{}}, snapshot))

	snapshot.Articles[0].Tags[0].Slug = "tag_slug_001"
	require.ErrorIs(t, validatePreparedSnapshot(CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9}, PreparedSnapshot{Articles: []ArticleSnapshot{}}, snapshot), ErrInvalidSnapshot)
}

func TestCallbackRequiresCorrelatedJobAndPositiveBuildBeforeDatabaseAccess(t *testing.T) {
	repo, _, _, _ := newRepositoryTest(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	for name, event := range map[string]CallbackEvent{
		"missing job":   {ReleaseID: 7, BuildNumber: 44, Stage: "queue", Status: JobQueued, Timestamp: now},
		"missing build": {ReleaseID: 7, PublishJobID: 12, Stage: "queue", Status: JobQueued, Timestamp: now},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := repo.ApplyCallbackLocked(context.Background(), event)
			require.ErrorIs(t, err, ErrConflict)
		})
	}
}

const (
	testSiteStateForUpdateSQL     = "SELECT current_release_id, active_publish_job_id FROM site_state WHERE singleton_key = ? FOR UPDATE"
	testInsertSiteStateSQL        = "INSERT INTO site_state (id, singleton_key, current_release_id, active_publish_job_id) VALUES (?, 1, NULL, NULL)"
	testInsertReleaseSQL          = "INSERT INTO releases (id, site_snapshot_json, checksum, status) VALUES (?, ?, ?, 'queued')"
	testInsertReleaseArticleSQL   = "INSERT INTO release_articles (id, release_id, article_id, revision_id, slug, title, summary, content_md, content_hash, published_at, tags_snapshot_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	testInsertPublishJobSQL       = "INSERT INTO publish_jobs (id, release_id, builder_id, status, stage, error_summary) VALUES (?, ?, ?, 'pending', 'pending', '')"
	testSetActiveJobSQL           = "UPDATE site_state SET active_publish_job_id = ? WHERE singleton_key = ? AND active_publish_job_id IS NULL"
	testReleaseSelectSQL          = "SELECT id, site_snapshot_json, checksum, status, created_at, completed_at FROM releases WHERE id = ?"
	testReleaseForUpdateSQL       = "SELECT id, site_snapshot_json, checksum, status, created_at, completed_at FROM releases WHERE id = ? FOR UPDATE"
	testReleaseListSQL            = "SELECT id, site_snapshot_json, checksum, status, created_at, completed_at FROM releases ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	testReleaseArticlesSelectSQL  = "SELECT article_id, revision_id, slug, title, summary, content_md, content_hash, published_at, tags_snapshot_json FROM release_articles WHERE release_id = ? ORDER BY article_id ASC LIMIT 100001"
	testJobsSelectSQL             = "SELECT id, release_id, builder_id, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE release_id = ? ORDER BY created_at DESC, id DESC"
	testJobForUpdateSQL           = "SELECT id, release_id, builder_id, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE id = ? AND release_id = ? FOR UPDATE"
	testJobByIDForUpdateSQL       = "SELECT id, release_id, builder_id, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE id = ? FOR UPDATE"
	testUpdateJobSQL              = "UPDATE publish_jobs SET status = ?, stage = ?, build_number = ?, error_summary = ?, finished_at = ? WHERE id = ? AND status = ?"
	testUpdateReleaseFinalSQL     = "UPDATE releases SET status = ?, completed_at = ? WHERE id = ?"
	testPublishArticlePointersSQL = "UPDATE articles a LEFT JOIN release_articles ra ON ra.release_id = ? AND ra.article_id = a.id SET a.published_revision_id = ra.revision_id, a.updated_at = ? WHERE a.published_revision_id IS NOT NULL OR ra.revision_id IS NOT NULL"
	testCompleteSiteStateSQL      = "UPDATE site_state SET current_release_id = ?, active_publish_job_id = NULL WHERE singleton_key = ? AND active_publish_job_id = ?"
	testFailSiteStateSQL          = "UPDATE site_state SET active_publish_job_id = NULL WHERE singleton_key = ? AND active_publish_job_id = ?"
	testMaxReleaseIDSQL           = "SELECT MAX(id) FROM releases"
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
	require.IsType(t, (*sql.Tx)(nil), source.executors[0])
	require.Equal(t, PublishArticle, source.requests[0].Mode)
	require.Equal(t, int64(41), source.requests[0].ArticleID)
	require.Zero(t, source.requests[0].CurrentReleaseID)
	require.Empty(t, source.requests[0].Base.Articles)
}

func TestMySQLRepositoryCreateLockedSnapshotSourceUsesSameTransactionAndRollsBackItsWrites(t *testing.T) {
	repo, mock, source, counter := newRepositoryTest(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	source.prepared = validPreparedSnapshot(now)
	source.execSQL = "UPDATE article_revisions SET status = 'frozen' WHERE id = ? AND status = 'editing'"
	source.execArgs = []any{int64(71)}
	counter.errOnKey = "idseq:publish_jobs"
	mock.ExpectBegin()
	expectSiteState(mock, nil, nil)
	mock.ExpectExec(source.execSQL).WithArgs(int64(71)).WillReturnResult(sqlmock.NewResult(0, 1))
	siteJSON, tagsJSON := snapshotJSON(t, source.prepared)
	mock.ExpectExec(testInsertReleaseSQL).WithArgs(int64(1), siteJSON, source.prepared.Checksum).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testInsertReleaseArticleSQL).WithArgs(
		int64(1), int64(1), int64(41), int64(71), "article_slug", "Title", "Summary", "Body",
		"sha256:"+strings.Repeat("b", 64), now, tagsJSON,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	_, _, err := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9})

	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.Len(t, source.executors, 1)
	require.IsType(t, (*sql.Tx)(nil), source.executors[0])
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
		source.prepared = validPreparedSnapshot(time.Now().UTC().Truncate(time.Microsecond))
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

	t.Run("well formed checksum mismatch", func(t *testing.T) {
		repo, mock, source, counter := newRepositoryTest(t)
		source.prepared = validPreparedSnapshot(time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
		source.prepared.Checksum = "sha256:" + strings.Repeat("f", 64)
		mock.ExpectBegin()
		expectSiteState(mock, nil, nil)
		mock.ExpectRollback()

		_, _, err := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9})

		require.ErrorIs(t, err, ErrInvalidSnapshot)
		require.NotContains(t, err.Error(), source.prepared.Checksum)
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
	base := PreparedSnapshot{Site: SiteSnapshot{Name: "Old", FilingName: "ICP", FilingNumber: "ICP-1", SocialLinks: []SocialLink{}}, Articles: []ArticleSnapshot{first, second}, Checksum: "sha256:" + strings.Repeat("a", 64)}

	settings := clonePreparedSnapshot(base)
	settings.Site.Name = "New"
	settings.Checksum = "sha256:" + strings.Repeat("c", 64)
	require.NoError(t, validatePreparedSnapshot(CreateCommand{Mode: PublishSettings, BuilderID: 9}, base, settings))

	unpublish := clonePreparedSnapshot(base)
	unpublish.Checksum = "sha256:" + strings.Repeat("c", 64)
	unpublish.Articles = []ArticleSnapshot{second}
	require.NoError(t, validatePreparedSnapshot(CreateCommand{Mode: UnpublishArticle, ArticleID: 41, BuilderID: 9}, base, unpublish))

	publish := clonePreparedSnapshot(base)
	publish.Checksum = "sha256:" + strings.Repeat("c", 64)
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

func TestValidatePreparedSnapshotEnforcesModeIsolation(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	first := validPreparedSnapshot(now).Articles[0]
	second := first
	second.ArticleID, second.RevisionID, second.Slug = 42, 72, "second_slug_"
	base := PreparedSnapshot{
		Site:     SiteSnapshot{Name: "Blog", AuthorBio: "Bio", FilingName: "ICP", FilingNumber: "ICP-1", SocialLinks: []SocialLink{}},
		Articles: []ArticleSnapshot{first, second}, Checksum: "sha256:" + strings.Repeat("a", 64),
	}

	for name, commandAndEdit := range map[string]struct {
		command CreateCommand
		edit    func(*PreparedSnapshot)
	}{
		"publish changes site":        {CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9}, func(value *PreparedSnapshot) { value.Site.Name = "other" }},
		"unpublish changes site":      {CreateCommand{Mode: UnpublishArticle, ArticleID: 41, BuilderID: 9}, func(value *PreparedSnapshot) { value.Site.Name = "other" }},
		"publish changes target slug": {CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9}, func(value *PreparedSnapshot) { value.Articles[0].Slug = "changed_slug" }},
		"publish changes unrelated":   {CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9}, func(value *PreparedSnapshot) { value.Articles[1].Title = "changed" }},
	} {
		t.Run(name, func(t *testing.T) {
			prepared := clonePreparedSnapshot(base)
			commandAndEdit.edit(&prepared)
			if commandAndEdit.command.Mode == UnpublishArticle {
				prepared.Articles = prepared.Articles[1:]
			}
			prepared.Checksum = "sha256:" + strings.Repeat("c", 64)
			require.ErrorIs(t, validatePreparedSnapshot(commandAndEdit.command, base, prepared), ErrInvalidSnapshot)
		})
	}
}

func TestValidatePreparedSnapshotRejectsStrictBoundaryViolations(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	base := PreparedSnapshot{Site: SiteSnapshot{SocialLinks: []SocialLink{}}, Articles: []ArticleSnapshot{}}
	valid := validPreparedSnapshot(now)
	valid.Articles[0].Tags = []TagSnapshot{{ID: 5, Name: "Go", Slug: "t_abcdefghijkl"}}

	mutations := map[string]func(*PreparedSnapshot){
		"missing filing":        func(value *PreparedSnapshot) { value.Site.FilingNumber = "" },
		"blank title":           func(value *PreparedSnapshot) { value.Articles[0].Title = "" },
		"content over 2 MiB":    func(value *PreparedSnapshot) { value.Articles[0].ContentMarkdown = strings.Repeat("x", 2*1024*1024+1) },
		"invalid UTF-8 content": func(value *PreparedSnapshot) { value.Articles[0].ContentMarkdown = string([]byte{0xff}) },
		"invalid UTF-8 site":    func(value *PreparedSnapshot) { value.Site.AuthorBio = string([]byte{0xff}) },
		"invalid UTF-8 tag":     func(value *PreparedSnapshot) { value.Articles[0].Tags[0].Name = string([]byte{0xff}) },
		"too many tags":         func(value *PreparedSnapshot) { value.Articles[0].Tags = make([]TagSnapshot, 33) },
		"tag order": func(value *PreparedSnapshot) {
			value.Articles[0].Tags = []TagSnapshot{{ID: 2, Name: "B", Slug: "t_abcdefghijkm"}, {ID: 1, Name: "A", Slug: "t_abcdefghijkl"}}
		},
		"duplicate tag slug": func(value *PreparedSnapshot) {
			value.Articles[0].Tags = []TagSnapshot{{ID: 1, Name: "A", Slug: "t_abcdefghijkl"}, {ID: 2, Name: "B", Slug: "t_abcdefghijkl"}}
		},
		"duplicate article slug": func(value *PreparedSnapshot) {
			second := value.Articles[0]
			second.ArticleID, second.RevisionID = 42, 72
			value.Articles = append(value.Articles, second)
		},
		"timestamp offset":    func(value *PreparedSnapshot) { value.Articles[0].PublishedAt = now.In(time.FixedZone("offset", 3600)) },
		"sub micro timestamp": func(value *PreparedSnapshot) { value.Articles[0].PublishedAt = now.Add(time.Nanosecond) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := clonePreparedSnapshot(valid)
			mutate(&value)
			require.ErrorIs(t, validatePreparedSnapshot(CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9}, base, value), ErrInvalidSnapshot)
		})
	}
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

func TestStoredSnapshotJSONRejectsDuplicateUnknownMissingNullAndTrailingValues(t *testing.T) {
	for name, raw := range map[string]string{
		"duplicate site": `{"name":"Blog","name":"Other","authorBio":"","aboutMarkdown":"","filingName":"ICP","filingNumber":"ICP-1","socialLinks":[]}`,
		"unknown site":   `{"name":"Blog","authorBio":"","aboutMarkdown":"","filingName":"ICP","filingNumber":"ICP-1","socialLinks":[],"secret":"x"}`,
		"missing site":   `{"name":"Blog","authorBio":"","aboutMarkdown":"","filingName":"ICP","filingNumber":"ICP-1"}`,
		"null site":      `null`,
		"null prose":     `{"name":"Blog","authorBio":null,"aboutMarkdown":"","filingName":"ICP","filingNumber":"ICP-1","socialLinks":[]}`,
		"trailing site":  `{"name":"Blog","authorBio":"","aboutMarkdown":"","filingName":"ICP","filingNumber":"ICP-1","socialLinks":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			var site SiteSnapshot
			require.Error(t, decodeSiteSnapshot([]byte(raw), &site))
		})
	}
	for name, raw := range map[string]string{
		"duplicate tag": `[{"ID":1,"ID":2,"Name":"Go","Slug":"t_abcdefghijkl"}]`,
		"unknown tag":   `[{"ID":1,"Name":"Go","Slug":"t_abcdefghijkl","secret":"x"}]`,
		"missing tag":   `[{"ID":1,"Name":"Go"}]`,
		"null tag text": `[{"ID":1,"Name":null,"Slug":"t_abcdefghijkl"}]`,
		"null tags":     `null`,
		"trailing tags": `[] {}`,
	} {
		t.Run(name, func(t *testing.T) {
			var tags []TagSnapshot
			require.Error(t, decodeTags([]byte(raw), &tags))
		})
	}
}

func TestStoredSnapshotJSONRejectsInvalidUTF8BeforeDecoding(t *testing.T) {
	badSite := append([]byte(`{"name":"`), 0xff)
	badSite = append(badSite, []byte(`","authorBio":"","aboutMarkdown":"","filingName":"ICP","filingNumber":"ICP-1","socialLinks":[]}`)...)
	var site SiteSnapshot
	require.Error(t, decodeSiteSnapshot(badSite, &site))

	badTags := append([]byte(`[{"ID":1,"Name":"`), 0xff)
	badTags = append(badTags, []byte(`","Slug":"t_abcdefghijkl"}]`)...)
	var tags []TagSnapshot
	require.Error(t, decodeTags(badTags, &tags))
}

func TestStoredSiteSocialJSONIsStrictAndBounded(t *testing.T) {
	prefix := `{"name":"Blog","authorBio":"","aboutMarkdown":"","filingName":"ICP","filingNumber":"ICP-1","socialLinks":`
	for name, socialJSON := range map[string]string{
		"duplicate field": `[{"label":"Git","label":"Other","url":"https://example.com"}]`,
		"unknown field":   `[{"label":"Git","url":"https://example.com","token":"secret"}]`,
		"missing field":   `[{"label":"Git"}]`,
		"null entry":      `[null]`,
		"invalid URL":     `[{"label":"Git","url":"http://example.com"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			var site SiteSnapshot
			require.Error(t, decodeSiteSnapshot([]byte(prefix+socialJSON+`}`), &site))
		})
	}
	entries := make([]string, 17)
	for index := range entries {
		entries[index] = fmt.Sprintf(`{"label":"L%d","url":"https://example.com/%d"}`, index, index)
	}
	var site SiteSnapshot
	require.Error(t, decodeSiteSnapshot([]byte(prefix+`[`+strings.Join(entries, ",")+`]}`), &site))
	require.LessOrEqual(t, len(site.SocialLinks), 16)
}

func TestStoredTagDecoderBoundsBeforeAppending(t *testing.T) {
	parts := make([]string, maxReleaseTagsPerArticle+1)
	for index := range parts {
		parts[index] = fmt.Sprintf(`{"ID":%d,"Name":"Tag","Slug":"t_%012d"}`, index+1, index+1)
	}
	var tags []TagSnapshot
	require.Error(t, decodeTags([]byte("["+strings.Join(parts, ",")+"]"), &tags))
	require.LessOrEqual(t, len(tags), maxReleaseTagsPerArticle)
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
		job, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, PublishJobID: 12, BuildNumber: 44, Stage: "build", Status: JobBuilding, ErrorSummary: "safe\nsummary", Timestamp: now})
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
		job, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, PublishJobID: 12, BuildNumber: 44, Stage: "deploy", Status: JobSuccess, Timestamp: now})
		require.NoError(t, err)
		require.False(t, duplicate)
		require.Equal(t, JobSuccess, job.Status)
	})

}

func TestMySQLRepositoryApplyCallbackLockedUsesExactAttemptIdentity(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	build := int64(44)
	t.Run("duplicate", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, nil, int64(12))
		expectJobForUpdate(mock, jobRow{id: 12, releaseID: 7, builderID: 9, status: JobBuilding, stage: "build", buildNumber: &build, errorSummary: "same", createdAt: now.Add(-time.Minute)})
		mock.ExpectCommit()
		_, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, PublishJobID: 12, BuildNumber: 44, Stage: "build", Status: JobBuilding, ErrorSummary: "same", Timestamp: now})
		require.NoError(t, err)
		require.True(t, duplicate)
	})

	t.Run("invalid", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, nil, int64(12))
		expectJobForUpdate(mock, jobRow{id: 12, releaseID: 7, builderID: 9, status: JobQueued, stage: "queue", buildNumber: &build, createdAt: now.Add(-time.Minute)})
		mock.ExpectRollback()
		_, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, PublishJobID: 12, BuildNumber: 44, Stage: "deploy", Status: JobSuccess, Timestamp: now})
		require.ErrorIs(t, err, ErrConflict)
		require.False(t, duplicate)
	})

	t.Run("old failed job callback does not finalize active retry", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		mock.ExpectBegin()
		expectSiteState(mock, nil, int64(13))
		mock.ExpectQuery(testJobForUpdateSQL).WithArgs(int64(12), int64(7)).WillReturnRows(
			sqlmock.NewRows([]string{"id", "release_id", "builder_id", "status", "stage", "build_number", "error_summary", "created_at", "finished_at"}).
				AddRow(int64(12), int64(7), int64(9), JobFailed, "deploy", int64(44), "failed", now.Add(-time.Hour), now.Add(-time.Minute)),
		)
		mock.ExpectCommit()

		job, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, PublishJobID: 12, BuildNumber: 44, Stage: "deploy", Status: JobFailed, ErrorSummary: "failed", Timestamp: now.Add(-time.Minute)})
		require.NoError(t, err)
		require.True(t, duplicate)
		require.Equal(t, int64(12), job.ID)
	})

	t.Run("old positive-build final callback remains idempotent after later job", func(t *testing.T) {
		repo, mock, _, _ := newRepositoryTest(t)
		oldFinished := now.Add(-time.Hour)
		mock.ExpectBegin()
		expectSiteState(mock, int64(7), nil)
		mock.ExpectQuery(testJobForUpdateSQL).WithArgs(int64(12), int64(7)).WillReturnRows(
			sqlmock.NewRows([]string{"id", "release_id", "builder_id", "status", "stage", "build_number", "error_summary", "created_at", "finished_at"}).
				AddRow(int64(12), int64(7), int64(9), JobFailed, "deploy", int64(44), "failed", oldFinished.Add(-time.Minute), oldFinished),
		)
		mock.ExpectCommit()

		job, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, PublishJobID: 12, BuildNumber: 44, Stage: "deploy", Status: JobFailed, ErrorSummary: "failed", Timestamp: oldFinished})
		require.NoError(t, err)
		require.True(t, duplicate)
		require.Equal(t, int64(12), job.ID)
	})
}

func TestMySQLRepositoryApplyCallbackLockedRejectsDelayedOldAttemptAgainstRetry(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repo, mock, _, _ := newRepositoryTest(t)
	mock.ExpectBegin()
	expectSiteState(mock, nil, int64(13))
	mock.ExpectQuery(testJobForUpdateSQL).WithArgs(int64(12), int64(7)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "release_id", "builder_id", "status", "stage", "build_number", "error_summary", "created_at", "finished_at"}).
			AddRow(int64(12), int64(7), int64(9), JobFailed, "trigger", nil, "failed", now.Add(-time.Hour), now.Add(-time.Minute)),
	)
	mock.ExpectRollback()

	_, duplicate, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, PublishJobID: 12, BuildNumber: 44, Stage: "build", Status: JobBuilding, Timestamp: now})

	require.ErrorIs(t, err, ErrConflict)
	require.False(t, duplicate)
}

func TestMySQLRepositoryApplyCallbackLockedMapsNamedBuildConflict(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repo, mock, _, _ := newRepositoryTest(t)
	mock.ExpectBegin()
	expectSiteState(mock, nil, int64(13))
	expectJobForUpdate(mock, jobRow{id: 13, releaseID: 7, builderID: 9, status: JobPending, stage: "pending", createdAt: now.Add(-time.Minute)})
	mock.ExpectExec(testUpdateJobSQL).WithArgs(JobQueued, "queue", int64(44), "", nil, int64(13), JobPending).
		WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'private-build' for key 'uk_publish_jobs_release_build'"})
	mock.ExpectRollback()

	_, _, err := repo.ApplyCallbackLocked(context.Background(), CallbackEvent{ReleaseID: 7, PublishJobID: 13, BuildNumber: 44, Stage: "queue", Status: JobQueued, Timestamp: now})

	require.ErrorIs(t, err, ErrConflict)
	require.NotContains(t, err.Error(), "private-build")
}

func TestMySQLRepositoryFailTriggerLockedFinalizesExactActiveRetry(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repo, mock, _, _ := newRepositoryTest(t)
	mock.ExpectBegin()
	expectSiteState(mock, nil, int64(13))
	expectJobByIDForUpdate(mock, jobRow{id: 13, releaseID: 7, builderID: 9, status: JobPending, stage: "pending", createdAt: now.Add(-time.Minute)})
	mock.ExpectExec(testUpdateJobSQL).WithArgs(JobFailed, "trigger", nil, "safe summary", now, int64(13), JobPending).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testUpdateReleaseFinalSQL).WithArgs(ReleaseFailed, now, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testFailSiteStateSQL).WithArgs(1, int64(13)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	job, duplicate, err := repo.FailTriggerLocked(context.Background(), 13, "safe\nsummary", now)

	require.NoError(t, err)
	require.False(t, duplicate)
	require.Equal(t, int64(13), job.ID)
}

func TestMySQLRepositoryFailTriggerLockedReplaysOnlyExactFinishedAttempt(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repo, mock, _, _ := newRepositoryTest(t)
	mock.ExpectBegin()
	expectSiteState(mock, int64(3), nil)
	expectJobByIDForUpdate(mock, jobRow{id: 13, releaseID: 7, builderID: 9, status: JobFailed, stage: "trigger", errorSummary: "safe summary", createdAt: now.Add(-time.Minute), finishedAt: &now})
	mock.ExpectCommit()

	job, duplicate, err := repo.FailTriggerLocked(context.Background(), 13, "safe\nsummary", now)

	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, int64(13), job.ID)
}

func TestMySQLRepositoryApplyCallbackLockedAcceptsExactNoChangeReleaseFinalization(t *testing.T) {
	inputAt := time.Date(2026, 8, 14, 12, 0, 0, 123456789, time.UTC)
	now := inputAt.Truncate(time.Microsecond)
	completed := now
	repo, mock, _, _ := newRepositoryTest(t)
	mock.ExpectBegin()
	expectSiteState(mock, int64(3), int64(12))
	expectJobByIDForUpdate(mock, jobRow{id: 12, releaseID: 7, builderID: 9, status: JobPending, stage: "pending", createdAt: now.Add(-time.Minute)})
	mock.ExpectExec(testUpdateJobSQL).WithArgs(JobFailed, "trigger", nil, "", now, int64(12), JobPending).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testUpdateReleaseFinalSQL).WithArgs(ReleaseFailed, now, int64(7)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(testReleaseForUpdateSQL).WithArgs(int64(7)).WillReturnRows(
		releaseRowsNoTest(7, validPreparedSnapshot(now), now.Add(-time.Hour), ReleaseFailed, &completed),
	)
	mock.ExpectExec(testFailSiteStateSQL).WithArgs(1, int64(12)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	_, duplicate, err := repo.FailTriggerLocked(context.Background(), 12, "", inputAt)
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

func TestMySQLRepositoryReconcileLockedAllowsExactActiveFailedRetry(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	snapshot := validPreparedSnapshot(now.Add(-time.Hour))
	build := int64(45)
	failedAt := now.Add(-time.Minute)
	repo, mock, _, _ := newRepositoryTest(t)
	mock.ExpectBegin()
	expectSiteState(mock, int64(3), int64(13))
	mock.ExpectQuery(testReleaseForUpdateSQL).WithArgs(int64(7)).WillReturnRows(releaseRowsNoTest(7, snapshot, now.Add(-time.Hour), ReleaseFailed, &failedAt))
	expectJobForUpdate(mock, jobRow{id: 13, releaseID: 7, builderID: 9, status: JobDeploying, stage: "deploy", buildNumber: &build, createdAt: now.Add(-time.Minute)})
	mock.ExpectExec(testUpdateJobSQL).WithArgs(JobSuccess, "deploy", int64(45), "", now, int64(13), JobDeploying).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testUpdateReleaseFinalSQL).WithArgs(ReleaseSuccess, now, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testPublishArticlePointersSQL).WithArgs(int64(7), now).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(testCompleteSiteStateSQL).WithArgs(int64(7), 1, int64(13)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	changed, err := repo.ReconcileLocked(context.Background(), Artifact{ReleaseID: 7, Checksum: snapshot.Checksum, BuildNumber: 45, DeployedAt: now})

	require.NoError(t, err)
	require.True(t, changed)
}

func TestMySQLRepositoryReconcileLockedRejectsFailedReleaseWithoutExactActiveRetry(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	snapshot := validPreparedSnapshot(now.Add(-time.Hour))
	failedAt := now.Add(-time.Minute)
	repo, mock, _, _ := newRepositoryTest(t)
	mock.ExpectBegin()
	expectSiteState(mock, int64(3), nil)
	mock.ExpectQuery(testReleaseForUpdateSQL).WithArgs(int64(7)).WillReturnRows(releaseRowsNoTest(7, snapshot, now.Add(-time.Hour), ReleaseFailed, &failedAt))
	mock.ExpectRollback()

	changed, err := repo.ReconcileLocked(context.Background(), Artifact{ReleaseID: 7, Checksum: snapshot.Checksum, BuildNumber: 45, DeployedAt: now})

	require.ErrorIs(t, err, ErrReconciliationRequired)
	require.False(t, changed)
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
	mu        sync.Mutex
	prepared  PreparedSnapshot
	err       error
	requests  []SnapshotRequest
	executors []SnapshotExecutor
	execSQL   string
	execArgs  []any
}

func (s *snapshotSourceFake) PrepareSnapshot(ctx context.Context, executor SnapshotExecutor, request SnapshotRequest) (PreparedSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, cloneSnapshotRequest(request))
	s.executors = append(s.executors, executor)
	if s.execSQL != "" {
		if _, err := executor.ExecContext(ctx, s.execSQL, s.execArgs...); err != nil {
			return PreparedSnapshot{}, err
		}
	}
	if s.err != nil {
		return PreparedSnapshot{}, s.err
	}
	return clonePreparedSnapshot(s.prepared), nil
}

type tableCounter struct {
	mu       sync.Mutex
	keys     []string
	next     map[string]int64
	err      error
	errOnKey string
	raises   []raiseCall
}

type raiseCall struct {
	key   string
	floor int64
}

func (c *tableCounter) Increment(_ context.Context, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = append(c.keys, key)
	if c.err != nil || c.errOnKey == key {
		if c.err == nil {
			return 0, errors.New("configured counter failure")
		}
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

func expectJobByIDForUpdate(mock sqlmock.Sqlmock, job jobRow) {
	mock.ExpectQuery(testJobByIDForUpdateSQL).WithArgs(job.id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "release_id", "builder_id", "status", "stage", "build_number", "error_summary", "created_at", "finished_at"}).
			AddRow(job.id, job.releaseID, job.builderID, job.status, job.stage, job.buildNumber, job.errorSummary, job.createdAt, job.finishedAt))
}

func validPreparedSnapshot(now time.Time) PreparedSnapshot {
	snapshot := PreparedSnapshot{
		Site: SiteSnapshot{Name: "Blog", AuthorBio: "Bio", AboutMarkdown: "About", FilingName: "ICP", FilingNumber: "ICP-1", SocialLinks: []SocialLink{}},
		Articles: []ArticleSnapshot{{
			ArticleID: 41, RevisionID: 71, Slug: "article_slug", Title: "Title", Summary: "Summary",
			ContentMarkdown: "Body", ContentHash: "sha256:" + strings.Repeat("b", 64), PublishedAt: now,
			Tags: []TagSnapshot{{ID: 5, Name: "Go", Slug: "t_abcdefghijkl"}},
		}},
	}
	checksum, err := preparedSnapshotChecksum(snapshot)
	if err != nil {
		panic(err)
	}
	snapshot.Checksum = checksum
	return snapshot
}

func emptyPreparedSnapshot() PreparedSnapshot {
	snapshot := PreparedSnapshot{
		Site:     SiteSnapshot{Name: "Blog", FilingName: "ICP", FilingNumber: "ICP-1", SocialLinks: []SocialLink{}},
		Articles: []ArticleSnapshot{},
	}
	checksum, err := preparedSnapshotChecksum(snapshot)
	if err != nil {
		panic(err)
	}
	snapshot.Checksum = checksum
	return snapshot
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
