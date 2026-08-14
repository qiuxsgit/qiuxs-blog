package release

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
)

const (
	siteStateForUpdateSQL     = "SELECT current_release_id, active_publish_job_id FROM site_state WHERE singleton_key = ? FOR UPDATE"
	insertSiteStateSQL        = "INSERT INTO site_state (id, singleton_key, current_release_id, active_publish_job_id) VALUES (?, 1, NULL, NULL)"
	insertReleaseSQL          = "INSERT INTO releases (id, site_snapshot_json, checksum, status) VALUES (?, ?, ?, 'queued')"
	insertReleaseArticleSQL   = "INSERT INTO release_articles (id, release_id, article_id, revision_id, slug, title, summary, content_md, content_hash, published_at, tags_snapshot_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	insertPublishJobSQL       = "INSERT INTO publish_jobs (id, release_id, builder_id, status, stage, error_summary) VALUES (?, ?, ?, 'pending', 'pending', '')"
	setActiveJobSQL           = "UPDATE site_state SET active_publish_job_id = ? WHERE singleton_key = ? AND active_publish_job_id IS NULL"
	releaseSelectSQL          = "SELECT id, site_snapshot_json, checksum, status, created_at, completed_at FROM releases WHERE id = ?"
	releaseForUpdateSQL       = "SELECT id, site_snapshot_json, checksum, status, created_at, completed_at FROM releases WHERE id = ? FOR UPDATE"
	releaseListSQL            = "SELECT id, site_snapshot_json, checksum, status, created_at, completed_at FROM releases ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	releaseArticlesSelectSQL  = "SELECT article_id, revision_id, slug, title, summary, content_md, content_hash, published_at, tags_snapshot_json FROM release_articles WHERE release_id = ? ORDER BY article_id ASC LIMIT 100001"
	jobsSelectSQL             = "SELECT id, release_id, builder_id, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE release_id = ? ORDER BY created_at DESC, id DESC"
	jobForUpdateSQL           = "SELECT id, release_id, builder_id, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE id = ? AND release_id = ? FOR UPDATE"
	jobByIDForUpdateSQL       = "SELECT id, release_id, builder_id, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE id = ? FOR UPDATE"
	updateJobSQL              = "UPDATE publish_jobs SET status = ?, stage = ?, build_number = ?, error_summary = ?, finished_at = ? WHERE id = ? AND status = ?"
	updateReleaseFinalSQL     = "UPDATE releases SET status = ?, completed_at = ? WHERE id = ?"
	publishArticlePointersSQL = "UPDATE articles a LEFT JOIN release_articles ra ON ra.release_id = ? AND ra.article_id = a.id SET a.published_revision_id = ra.revision_id, a.updated_at = ? WHERE a.published_revision_id IS NOT NULL OR ra.revision_id IS NOT NULL"
	completeSiteStateSQL      = "UPDATE site_state SET current_release_id = ?, active_publish_job_id = NULL WHERE singleton_key = ? AND active_publish_job_id = ?"
	failSiteStateSQL          = "UPDATE site_state SET active_publish_job_id = NULL WHERE singleton_key = ? AND active_publish_job_id = ?"
)

const (
	releasesTable        = "releases"
	releaseArticlesTable = "release_articles"
	publishJobsTable     = "publish_jobs"
	siteStateTable       = "site_state"
)

var (
	releaseChecksumPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	releaseSlugPattern     = regexp.MustCompile(`^[a-z0-9_-]{12}$`)
	releaseTagSlugPattern  = regexp.MustCompile(`^t_[a-z0-9_-]{12}$`)
	publishBuildUnique     = regexp.MustCompile(`(?i)(?:key ['\x60]?(?:[^'\x60.]+\.)?uk_publish_jobs_release_build['\x60]?|constraint ['\x60]?uk_publish_jobs_release_build['\x60]?)`)
)

const (
	maxReleaseTagsPerArticle = 32
	maxReleaseContentBytes   = 2 * 1024 * 1024
	maxReleaseArticles       = 100000
	maxReleaseSocialLinks    = 16
)

type MySQLRepository struct {
	db        *sql.DB
	ids       *idgen.Generator
	snapshots SnapshotSource
	initErr   error
}

func NewMySQLRepository(db *sql.DB, ids *idgen.Generator, snapshots SnapshotSource) *MySQLRepository {
	repository := &MySQLRepository{db: db, ids: ids, snapshots: snapshots}
	switch {
	case db == nil:
		repository.initErr = errors.New("release database is required")
	case ids == nil:
		repository.initErr = errors.New("release ID generator is required")
	case nilReleaseInterface(snapshots):
		repository.initErr = errors.New("release snapshot source is required")
	}
	return repository
}

func (r *MySQLRepository) CreateLocked(ctx context.Context, command CreateCommand) (Release, PublishJob, error) {
	if err := r.validate(ctx); err != nil {
		return Release{}, PublishJob{}, err
	}
	if err := validateCreateCommand(command); err != nil {
		return Release{}, PublishJob{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Release{}, PublishJob{}, releaseDependency("begin release creation", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	state, err := r.ensureLockedSiteState(ctx, tx)
	if err != nil {
		return Release{}, PublishJob{}, err
	}
	if state.active.Valid {
		return Release{}, PublishJob{}, releaseDomain("create release", ErrBusy)
	}

	base := PreparedSnapshot{Articles: make([]ArticleSnapshot, 0)}
	if state.current.Valid {
		base, err = loadPreparedSnapshot(ctx, tx, state.current.Int64)
		if err != nil {
			return Release{}, PublishJob{}, err
		}
	}
	request := SnapshotRequest{
		Mode: command.Mode, ArticleID: command.ArticleID,
		CurrentReleaseID: state.current.Int64, Base: clonePreparedSnapshot(base),
	}
	prepared, err := r.snapshots.PrepareSnapshot(ctx, tx, request)
	if err != nil {
		return Release{}, PublishJob{}, releaseDependency("prepare immutable release snapshot", err)
	}
	prepared = normalizePreparedSnapshot(prepared)
	if err := validatePreparedSnapshot(command, base, prepared); err != nil {
		return Release{}, PublishJob{}, err
	}
	sort.Slice(prepared.Articles, func(i, j int) bool { return prepared.Articles[i].ArticleID < prepared.Articles[j].ArticleID })

	siteJSON, err := json.Marshal(prepared.Site)
	if err != nil {
		return Release{}, PublishJob{}, releaseDependency("encode release site snapshot", err)
	}
	var releaseID int64
	if err := r.ids.Insert(ctx, releasesTable, func(id int64) error {
		releaseID = id
		_, insertErr := tx.ExecContext(ctx, insertReleaseSQL, id, string(siteJSON), prepared.Checksum)
		return insertErr
	}); err != nil {
		return Release{}, PublishJob{}, releaseDependency("insert release", err)
	}
	for _, article := range prepared.Articles {
		tagsJSON, marshalErr := json.Marshal(article.Tags)
		if marshalErr != nil {
			return Release{}, PublishJob{}, releaseDependency("encode release article tags", marshalErr)
		}
		if insertErr := r.ids.Insert(ctx, releaseArticlesTable, func(id int64) error {
			_, execErr := tx.ExecContext(ctx, insertReleaseArticleSQL,
				id, releaseID, article.ArticleID, article.RevisionID, article.Slug, article.Title,
				article.Summary, article.ContentMarkdown, article.ContentHash, article.PublishedAt.UTC(), string(tagsJSON),
			)
			return execErr
		}); insertErr != nil {
			return Release{}, PublishJob{}, releaseDependency("insert release article", insertErr)
		}
	}
	var jobID int64
	if err := r.ids.Insert(ctx, publishJobsTable, func(id int64) error {
		jobID = id
		_, insertErr := tx.ExecContext(ctx, insertPublishJobSQL, id, releaseID, command.BuilderID)
		return insertErr
	}); err != nil {
		return Release{}, PublishJob{}, releaseDependency("insert publish job", err)
	}
	if err := execExactlyOne(ctx, tx, "set active publish job", setActiveJobSQL, jobID, 1); err != nil {
		return Release{}, PublishJob{}, err
	}
	aggregate, err := loadAggregate(ctx, tx, releaseID)
	if err != nil {
		return Release{}, PublishJob{}, err
	}
	created, err := aggregate.LatestJob()
	if err != nil || created.ID != jobID {
		return Release{}, PublishJob{}, releaseDependency("reload created release", ErrInvalidAggregate)
	}
	if err := tx.Commit(); err != nil {
		return Release{}, PublishJob{}, releaseDependency("commit release creation", err)
	}
	committed = true
	return cloneRelease(aggregate.Release), clonePublishJob(created), nil
}

func (r *MySQLRepository) FindRelease(ctx context.Context, id int64) (Aggregate, error) {
	if err := r.validate(ctx); err != nil {
		return Aggregate{}, err
	}
	if id <= 0 {
		return Aggregate{}, releaseDomain("find release", ErrNotFound)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return Aggregate{}, releaseDependency("begin release read", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	aggregate, err := loadAggregate(ctx, tx, id)
	if err != nil {
		return Aggregate{}, err
	}
	if err := tx.Commit(); err != nil {
		return Aggregate{}, releaseDependency("commit release read", err)
	}
	committed = true
	return cloneAggregate(aggregate), nil
}

func (r *MySQLRepository) ListReleases(ctx context.Context, query ListQuery) ([]Aggregate, error) {
	if err := r.validate(ctx); err != nil {
		return nil, err
	}
	if query.Limit < 1 || query.Limit > 100 || query.Offset < 0 {
		return nil, releaseDomain("list releases", ErrInvalidSnapshot)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, releaseDependency("begin release list", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, releaseListSQL, query.Limit, query.Offset)
	if err != nil {
		return nil, releaseDependency("list releases", err)
	}
	releases := make([]Release, 0)
	for rows.Next() {
		item, scanErr := scanRelease(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		releases = append(releases, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, releaseDependency("list releases", err)
	}
	if err := rows.Close(); err != nil {
		return nil, releaseDependency("list releases", err)
	}
	items := make([]Aggregate, 0, len(releases))
	for _, item := range releases {
		jobs, jobsErr := loadJobs(ctx, tx, item.ID)
		if jobsErr != nil {
			return nil, jobsErr
		}
		aggregate := Aggregate{Release: item, Jobs: jobs}
		if err := aggregate.Validate(); err != nil {
			return nil, releaseDependency("validate stored release aggregate", err)
		}
		items = append(items, cloneAggregate(aggregate))
	}
	if err := tx.Commit(); err != nil {
		return nil, releaseDependency("commit release list", err)
	}
	committed = true
	return items, nil
}

func (r *MySQLRepository) LoadBundle(ctx context.Context, id int64) (Bundle, error) {
	if err := r.validate(ctx); err != nil {
		return Bundle{}, err
	}
	if id <= 0 {
		return Bundle{}, releaseDomain("load release bundle", ErrNotFound)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return Bundle{}, releaseDependency("begin release bundle read", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	rel, err := queryRelease(ctx, tx, id)
	if err != nil {
		return Bundle{}, err
	}
	articles, err := loadReleaseArticles(ctx, tx, id)
	if err != nil {
		return Bundle{}, err
	}
	bundle := assembleStoredBundle(rel, articles)
	if err := tx.Commit(); err != nil {
		return Bundle{}, releaseDependency("commit release bundle read", err)
	}
	committed = true
	return bundle, nil
}

func (r *MySQLRepository) CreateRetryLocked(ctx context.Context, releaseID int64) (Aggregate, PublishJob, error) {
	if err := r.validate(ctx); err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	if releaseID <= 0 {
		return Aggregate{}, PublishJob{}, releaseDomain("retry release", ErrNotFound)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Aggregate{}, PublishJob{}, releaseDependency("begin release retry", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	state, err := r.ensureLockedSiteState(ctx, tx)
	if err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	if state.active.Valid {
		return Aggregate{}, PublishJob{}, releaseDomain("retry release", ErrBusy)
	}
	before, err := loadAggregate(ctx, tx, releaseID)
	if err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	latest, err := before.LatestJob()
	if err != nil || before.Release.Status != ReleaseFailed || latest.Status != JobFailed {
		return Aggregate{}, PublishJob{}, releaseDomain("retry release", ErrConflict)
	}
	var jobID int64
	if err := r.ids.Insert(ctx, publishJobsTable, func(id int64) error {
		jobID = id
		_, insertErr := tx.ExecContext(ctx, insertPublishJobSQL, id, releaseID, latest.BuilderID)
		return insertErr
	}); err != nil {
		return Aggregate{}, PublishJob{}, releaseDependency("insert retry publish job", err)
	}
	if err := execExactlyOne(ctx, tx, "set retry active publish job", setActiveJobSQL, jobID, 1); err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	after, err := loadAggregate(ctx, tx, releaseID)
	if err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	created, err := after.LatestJob()
	if err != nil || created.ID != jobID || after.ValidateRetry(created) != nil {
		return Aggregate{}, PublishJob{}, releaseDependency("reload retried release", ErrInvalidAggregate)
	}
	if err := tx.Commit(); err != nil {
		return Aggregate{}, PublishJob{}, releaseDependency("commit release retry", err)
	}
	committed = true
	return cloneAggregate(after), clonePublishJob(created), nil
}

func (r *MySQLRepository) ApplyCallbackLocked(ctx context.Context, event CallbackEvent) (PublishJob, bool, error) {
	if err := r.validate(ctx); err != nil {
		return PublishJob{}, false, err
	}
	event.Timestamp = event.Timestamp.UTC()
	event.Timestamp = event.Timestamp.Truncate(time.Microsecond)
	event.ErrorSummary = sanitizeSummary(event.ErrorSummary)
	if err := validateCallbackEvent(event); err != nil {
		return PublishJob{}, false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return PublishJob{}, false, releaseDependency("begin publish callback", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	state, err := lockSiteState(ctx, tx)
	if err != nil {
		return PublishJob{}, false, err
	}
	job, err := queryJobForUpdate(ctx, tx, event.PublishJobID, event.ReleaseID)
	if err != nil {
		return PublishJob{}, false, err
	}
	if !state.active.Valid || state.active.Int64 != event.PublishJobID {
		if callbackMatches(job, event) {
			if err := tx.Commit(); err != nil {
				return PublishJob{}, false, releaseDependency("commit duplicate publish callback", err)
			}
			committed = true
			return clonePublishJob(job), true, nil
		}
		return PublishJob{}, false, releaseDomain("apply publish callback", ErrConflict)
	}
	if callbackMatches(job, event) {
		if err := tx.Commit(); err != nil {
			return PublishJob{}, false, releaseDependency("commit duplicate publish callback", err)
		}
		committed = true
		return clonePublishJob(job), true, nil
	}
	if !allowedTransition(job.Status, event.Status) {
		return PublishJob{}, false, releaseDomain("apply publish callback", ErrConflict)
	}
	build, err := callbackBuildNumber(job.BuildNumber, event.BuildNumber)
	if err != nil {
		return PublishJob{}, false, err
	}
	var finished any
	if finalJobStatus(event.Status) {
		finished = event.Timestamp
	}
	result, err := tx.ExecContext(ctx, updateJobSQL, event.Status, event.Stage, nullableInt64(build), event.ErrorSummary, finished, job.ID, job.Status)
	if err != nil {
		if isPublishBuildDuplicate(err) {
			return PublishJob{}, false, releaseDomain("assign publish build", ErrConflict)
		}
		return PublishJob{}, false, releaseDependency("update publish job", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return PublishJob{}, false, releaseDependency("update publish job", err)
	}
	if rows == 0 {
		current, reloadErr := queryJobForUpdate(ctx, tx, job.ID, event.ReleaseID)
		if reloadErr != nil {
			return PublishJob{}, false, reloadErr
		}
		if callbackMatches(current, event) {
			if err := tx.Commit(); err != nil {
				return PublishJob{}, false, releaseDependency("commit duplicate publish callback", err)
			}
			committed = true
			return clonePublishJob(current), true, nil
		}
		return PublishJob{}, false, releaseDomain("apply publish callback", ErrConflict)
	}
	if rows != 1 {
		return PublishJob{}, false, releaseDependency("update publish job", errors.New("unexpected affected row count"))
	}
	job.Status, job.Stage, job.BuildNumber, job.ErrorSummary = event.Status, event.Stage, cloneInt64(build), event.ErrorSummary
	if finalJobStatus(event.Status) {
		job.FinishedAt = cloneTime(&event.Timestamp)
	}
	if finalJobStatus(event.Status) {
		releaseStatus := ReleaseFailed
		if event.Status == JobSuccess {
			releaseStatus = ReleaseSuccess
		}
		if err := completeRelease(ctx, tx, event.ReleaseID, releaseStatus, event.Timestamp); err != nil {
			return PublishJob{}, false, err
		}
		if event.Status == JobSuccess {
			if err := execAnyRows(ctx, tx, "publish release articles", publishArticlePointersSQL, event.ReleaseID, event.Timestamp); err != nil {
				return PublishJob{}, false, err
			}
			if err := execExactlyOne(ctx, tx, "advance current release", completeSiteStateSQL, event.ReleaseID, 1, job.ID); err != nil {
				return PublishJob{}, false, err
			}
		} else if err := execExactlyOne(ctx, tx, "clear failed publish job", failSiteStateSQL, 1, job.ID); err != nil {
			return PublishJob{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return PublishJob{}, false, releaseDependency("commit publish callback", err)
	}
	committed = true
	return clonePublishJob(job), false, nil
}

func (r *MySQLRepository) FailTriggerLocked(ctx context.Context, publishJobID int64, summary string, at time.Time) (PublishJob, bool, error) {
	if err := r.validate(ctx); err != nil {
		return PublishJob{}, false, err
	}
	at = at.UTC().Truncate(time.Microsecond)
	if publishJobID <= 0 || at.IsZero() {
		return PublishJob{}, false, releaseDomain("fail publish trigger", ErrConflict)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return PublishJob{}, false, releaseDependency("begin trigger failure", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	state, err := lockSiteState(ctx, tx)
	if err != nil {
		return PublishJob{}, false, err
	}
	job, err := queryJobByIDForUpdate(ctx, tx, publishJobID)
	if err != nil {
		return PublishJob{}, false, err
	}
	event := CallbackEvent{ReleaseID: job.ReleaseID, PublishJobID: job.ID, Stage: "trigger", Status: JobFailed, ErrorSummary: sanitizeSummary(summary), Timestamp: at}
	if !state.active.Valid || state.active.Int64 != publishJobID {
		if callbackMatches(job, event) {
			if err := tx.Commit(); err != nil {
				return PublishJob{}, false, releaseDependency("commit duplicate trigger failure", err)
			}
			committed = true
			return clonePublishJob(job), true, nil
		}
		return PublishJob{}, false, releaseDomain("fail publish trigger", ErrConflict)
	}
	if callbackMatches(job, event) {
		if err := tx.Commit(); err != nil {
			return PublishJob{}, false, releaseDependency("commit duplicate trigger failure", err)
		}
		committed = true
		return clonePublishJob(job), true, nil
	}
	if job.Status != JobPending {
		return PublishJob{}, false, releaseDomain("fail publish trigger", ErrConflict)
	}
	if err := updateCallbackJob(ctx, tx, job, event); err != nil {
		return PublishJob{}, false, err
	}
	if err := completeRelease(ctx, tx, job.ReleaseID, ReleaseFailed, at); err != nil {
		return PublishJob{}, false, err
	}
	if err := execExactlyOne(ctx, tx, "clear failed trigger job", failSiteStateSQL, 1, job.ID); err != nil {
		return PublishJob{}, false, err
	}
	job.Status, job.Stage, job.ErrorSummary, job.FinishedAt = JobFailed, "trigger", event.ErrorSummary, cloneTime(&at)
	if err := tx.Commit(); err != nil {
		return PublishJob{}, false, releaseDependency("commit trigger failure", err)
	}
	committed = true
	return clonePublishJob(job), false, nil
}

func (r *MySQLRepository) ReconcileLocked(ctx context.Context, artifact Artifact) (bool, error) {
	if err := r.validate(ctx); err != nil {
		return false, err
	}
	artifact.DeployedAt = artifact.DeployedAt.UTC()
	artifact.DeployedAt = artifact.DeployedAt.Truncate(time.Microsecond)
	if artifact.ReleaseID <= 0 || artifact.BuildNumber <= 0 || !releaseChecksumPattern.MatchString(artifact.Checksum) || artifact.DeployedAt.IsZero() {
		return false, releaseDomain("reconcile release", ErrReconciliationRequired)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, releaseDependency("begin release reconciliation", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	state, err := lockSiteState(ctx, tx)
	if err != nil {
		return false, err
	}
	rel, err := scanRelease(tx.QueryRowContext(ctx, releaseForUpdateSQL, artifact.ReleaseID))
	if errors.Is(err, sql.ErrNoRows) {
		return false, releaseDomain("reconcile release", ErrReconciliationRequired)
	}
	if err != nil {
		return false, err
	}
	if rel.Checksum != artifact.Checksum {
		return false, releaseDomain("reconcile release", ErrReconciliationRequired)
	}
	if state.current.Valid && state.current.Int64 == artifact.ReleaseID {
		jobs, loadErr := loadJobs(ctx, tx, artifact.ReleaseID)
		if loadErr != nil {
			return false, loadErr
		}
		if rel.Status != ReleaseSuccess || !containsSuccessfulBuild(jobs, artifact.BuildNumber) {
			return false, releaseDomain("reconcile release", ErrReconciliationRequired)
		}
		if err := tx.Commit(); err != nil {
			return false, releaseDependency("commit release reconciliation", err)
		}
		committed = true
		return false, nil
	}
	if !state.active.Valid {
		return false, releaseDomain("reconcile release", ErrReconciliationRequired)
	}
	job, err := queryJobForUpdate(ctx, tx, state.active.Int64, artifact.ReleaseID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, releaseDomain("reconcile release", ErrReconciliationRequired)
		}
		return false, err
	}
	if (rel.Status != ReleaseQueued && rel.Status != ReleaseFailed) || job.Status != JobDeploying || job.BuildNumber == nil || *job.BuildNumber != artifact.BuildNumber {
		return false, releaseDomain("reconcile release", ErrReconciliationRequired)
	}
	if err := execExactlyOne(ctx, tx, "update reconciled publish job", updateJobSQL,
		JobSuccess, job.Stage, artifact.BuildNumber, "", artifact.DeployedAt, job.ID, job.Status,
	); err != nil {
		return false, err
	}
	if err := completeRelease(ctx, tx, artifact.ReleaseID, ReleaseSuccess, artifact.DeployedAt); err != nil {
		return false, err
	}
	if err := execAnyRows(ctx, tx, "publish reconciled release articles", publishArticlePointersSQL, artifact.ReleaseID, artifact.DeployedAt); err != nil {
		return false, err
	}
	if err := execExactlyOne(ctx, tx, "advance reconciled current release", completeSiteStateSQL, artifact.ReleaseID, 1, job.ID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, releaseDependency("commit release reconciliation", err)
	}
	committed = true
	return true, nil
}

func containsSuccessfulBuild(jobs []PublishJob, buildNumber int64) bool {
	for _, job := range jobs {
		if job.Status == JobSuccess && job.BuildNumber != nil && *job.BuildNumber == buildNumber {
			return true
		}
	}
	return false
}

type siteState struct{ current, active sql.NullInt64 }

func (r *MySQLRepository) ensureLockedSiteState(ctx context.Context, tx *sql.Tx) (siteState, error) {
	state, err := querySiteState(ctx, tx)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return siteState{}, releaseDependency("lock release state", err)
	}
	if err := r.ids.Insert(ctx, siteStateTable, func(id int64) error {
		_, insertErr := tx.ExecContext(ctx, insertSiteStateSQL, id)
		return insertErr
	}); err != nil {
		return siteState{}, releaseDependency("create release state", err)
	}
	state, err = querySiteState(ctx, tx)
	if err != nil {
		return siteState{}, releaseDependency("reload release state", err)
	}
	return state, nil
}

func lockSiteState(ctx context.Context, tx *sql.Tx) (siteState, error) {
	state, err := querySiteState(ctx, tx)
	if errors.Is(err, sql.ErrNoRows) {
		return siteState{}, releaseDomain("lock release state", ErrNotFound)
	}
	if err != nil {
		return siteState{}, releaseDependency("lock release state", err)
	}
	return state, nil
}

func querySiteState(ctx context.Context, tx *sql.Tx) (siteState, error) {
	var state siteState
	err := tx.QueryRowContext(ctx, siteStateForUpdateSQL, 1).Scan(&state.current, &state.active)
	if err == nil && (state.current.Valid && state.current.Int64 <= 0 || state.active.Valid && state.active.Int64 <= 0) {
		return siteState{}, errors.New("stored release state is invalid")
	}
	return state, err
}

type releaseQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadAggregate(ctx context.Context, queryer interface {
	releaseQueryer
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, id int64) (Aggregate, error) {
	rel, err := queryRelease(ctx, queryer, id)
	if err != nil {
		return Aggregate{}, err
	}
	jobs, err := loadJobs(ctx, queryer, id)
	if err != nil {
		return Aggregate{}, err
	}
	aggregate := Aggregate{Release: rel, Jobs: jobs}
	if err := aggregate.Validate(); err != nil {
		return Aggregate{}, releaseDependency("validate stored release aggregate", err)
	}
	return aggregate, nil
}

func queryRelease(ctx context.Context, queryer releaseQueryer, id int64) (Release, error) {
	item, err := scanRelease(queryer.QueryRowContext(ctx, releaseSelectSQL, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Release{}, releaseDomain("find release", ErrNotFound)
	}
	if err != nil {
		return Release{}, err
	}
	return item, nil
}

type releaseScanner interface{ Scan(...any) error }

func scanRelease(scanner releaseScanner) (Release, error) {
	var item Release
	var siteJSON []byte
	var completed sql.NullTime
	if err := scanner.Scan(&item.ID, &siteJSON, &item.Checksum, &item.Status, &item.CreatedAt, &completed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Release{}, sql.ErrNoRows
		}
		return Release{}, releaseDependency("scan release", err)
	}
	if completed.Valid {
		item.CompletedAt = cloneTime(&completed.Time)
	}
	if err := decodeSiteSnapshot(siteJSON, &item.Site); err != nil ||
		settings.ValidateReleaseSnapshot(toSettingsSite(item.Site)) != nil || !validStoredRelease(item) {
		return Release{}, releaseDependency("scan release", errors.New("stored release is invalid"))
	}
	return item, nil
}

func loadJobs(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, releaseID int64) ([]PublishJob, error) {
	rows, err := queryer.QueryContext(ctx, jobsSelectSQL, releaseID)
	if err != nil {
		return nil, releaseDependency("load release jobs", err)
	}
	defer rows.Close()
	jobs := make([]PublishJob, 0)
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, releaseDependency("load release jobs", err)
	}
	return jobs, nil
}

func queryJobForUpdate(ctx context.Context, tx *sql.Tx, id, releaseID int64) (PublishJob, error) {
	job, err := scanJob(tx.QueryRowContext(ctx, jobForUpdateSQL, id, releaseID))
	if errors.Is(err, sql.ErrNoRows) {
		return PublishJob{}, releaseDomain("find active publish job", ErrNotFound)
	}
	return job, err
}

func queryJobByIDForUpdate(ctx context.Context, tx *sql.Tx, id int64) (PublishJob, error) {
	job, err := scanJob(tx.QueryRowContext(ctx, jobByIDForUpdateSQL, id))
	if errors.Is(err, sql.ErrNoRows) {
		return PublishJob{}, releaseDomain("find publish job", ErrNotFound)
	}
	return job, err
}

func updateCallbackJob(ctx context.Context, tx *sql.Tx, job PublishJob, event CallbackEvent) error {
	var finished any
	if finalJobStatus(event.Status) {
		finished = event.Timestamp
	}
	build, err := callbackBuildNumber(job.BuildNumber, event.BuildNumber)
	if err != nil {
		return err
	}
	return execExactlyOne(ctx, tx, "update publish job", updateJobSQL,
		event.Status, event.Stage, nullableInt64(build), event.ErrorSummary, finished, job.ID, job.Status,
	)
}

func scanJob(scanner releaseScanner) (PublishJob, error) {
	var job PublishJob
	var build sql.NullInt64
	var finished sql.NullTime
	if err := scanner.Scan(&job.ID, &job.ReleaseID, &job.BuilderID, &job.Status, &job.Stage, &build, &job.ErrorSummary, &job.CreatedAt, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PublishJob{}, sql.ErrNoRows
		}
		return PublishJob{}, releaseDependency("scan publish job", err)
	}
	if build.Valid {
		job.BuildNumber = cloneInt64(&build.Int64)
	}
	if finished.Valid {
		job.FinishedAt = cloneTime(&finished.Time)
	}
	if !validStoredJob(job) {
		return PublishJob{}, releaseDependency("scan publish job", errors.New("stored publish job is invalid"))
	}
	return job, nil
}

func loadPreparedSnapshot(ctx context.Context, queryer interface {
	releaseQueryer
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, releaseID int64) (PreparedSnapshot, error) {
	rel, err := queryRelease(ctx, queryer, releaseID)
	if err != nil {
		return PreparedSnapshot{}, err
	}
	articles, err := loadReleaseArticles(ctx, queryer, releaseID)
	if err != nil {
		return PreparedSnapshot{}, err
	}
	return PreparedSnapshot{Site: cloneSiteSnapshot(rel.Site), Articles: articles, Checksum: rel.Checksum}, nil
}

func loadReleaseArticles(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, releaseID int64) ([]ArticleSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, releaseArticlesSelectSQL, releaseID)
	if err != nil {
		return nil, releaseDependency("load release articles", err)
	}
	defer rows.Close()
	items := make([]ArticleSnapshot, 0)
	seen := make(map[int64]struct{})
	for rows.Next() {
		if len(items) == maxReleaseArticles {
			return nil, releaseDependency("load release articles", errors.New("stored release article count exceeds limit"))
		}
		var item ArticleSnapshot
		var tagsJSON []byte
		if err := rows.Scan(&item.ArticleID, &item.RevisionID, &item.Slug, &item.Title, &item.Summary, &item.ContentMarkdown, &item.ContentHash, &item.PublishedAt, &tagsJSON); err != nil {
			return nil, releaseDependency("scan release article", err)
		}
		if _, duplicate := seen[item.ArticleID]; duplicate {
			return nil, releaseDependency("scan release article", errors.New("stored release article is duplicated"))
		}
		seen[item.ArticleID] = struct{}{}
		if err := decodeTags(tagsJSON, &item.Tags); err != nil || !validArticleSnapshot(item) {
			return nil, releaseDependency("scan release article", errors.New("stored release article is invalid"))
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, releaseDependency("load release articles", err)
	}
	return items, nil
}

func validateCreateCommand(command CreateCommand) error {
	if command.BuilderID <= 0 || command.RequestedBy < 0 {
		return releaseDomain("create release", ErrInvalidSnapshot)
	}
	switch command.Mode {
	case PublishSettings:
		if command.ArticleID != 0 {
			return releaseDomain("create release", ErrInvalidSnapshot)
		}
	case PublishArticle, UnpublishArticle:
		if command.ArticleID <= 0 {
			return releaseDomain("create release", ErrInvalidSnapshot)
		}
	default:
		return releaseDomain("create release", ErrInvalidSnapshot)
	}
	return nil
}

func validatePreparedSnapshot(command CreateCommand, base, prepared PreparedSnapshot) error {
	if !releaseChecksumPattern.MatchString(prepared.Checksum) || len(prepared.Articles) > maxReleaseArticles {
		return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
	}
	if err := settings.ValidateReleaseSnapshot(toSettingsSite(prepared.Site)); err != nil {
		return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
	}
	seen := make(map[int64]struct{}, len(prepared.Articles))
	seenSlugs := make(map[string]struct{}, len(prepared.Articles))
	for _, item := range prepared.Articles {
		if !validArticleSnapshot(item) {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
		if _, duplicate := seen[item.ArticleID]; duplicate {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
		seen[item.ArticleID] = struct{}{}
		if _, duplicate := seenSlugs[item.Slug]; duplicate {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
		seenSlugs[item.Slug] = struct{}{}
	}
	baseByID := articlesByID(base.Articles)
	preparedByID := articlesByID(prepared.Articles)
	if baseByID == nil || preparedByID == nil {
		return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
	}
	switch command.Mode {
	case PublishSettings:
		if !articleMapsEqual(baseByID, preparedByID) {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
	case UnpublishArticle:
		if base.Checksum != "" && !reflect.DeepEqual(base.Site, prepared.Site) {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
		if _, present := baseByID[command.ArticleID]; !present {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
		delete(baseByID, command.ArticleID)
		if !articleMapsEqual(baseByID, preparedByID) {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
	case PublishArticle:
		if base.Checksum != "" && !reflect.DeepEqual(base.Site, prepared.Site) {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
		preparedTarget, present := preparedByID[command.ArticleID]
		if !present {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
		if baseTarget, existed := baseByID[command.ArticleID]; existed && baseTarget.Slug != preparedTarget.Slug {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
		delete(baseByID, command.ArticleID)
		delete(preparedByID, command.ArticleID)
		if !articleMapsEqual(baseByID, preparedByID) {
			return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
		}
	default:
		return releaseDomain("validate release snapshot", ErrInvalidSnapshot)
	}
	return nil
}

func normalizePreparedSnapshot(value PreparedSnapshot) PreparedSnapshot {
	value = clonePreparedSnapshot(value)
	if value.Site.SocialLinks == nil {
		value.Site.SocialLinks = make([]SocialLink, 0)
	}
	if value.Articles == nil {
		value.Articles = make([]ArticleSnapshot, 0)
	}
	for index := range value.Articles {
		if value.Articles[index].Tags == nil {
			value.Articles[index].Tags = make([]TagSnapshot, 0)
		}
	}
	return value
}

func validArticleSnapshot(item ArticleSnapshot) bool {
	if item.ArticleID <= 0 || item.RevisionID <= 0 || !releaseSlugPattern.MatchString(item.Slug) ||
		!releaseChecksumPattern.MatchString(item.ContentHash) || item.PublishedAt.IsZero() ||
		!utf8.ValidString(item.Slug) || !utf8.ValidString(item.Title) || !utf8.ValidString(item.Summary) ||
		!utf8.ValidString(item.ContentMarkdown) || !utf8.ValidString(item.ContentHash) ||
		item.PublishedAt.Location() != time.UTC || item.PublishedAt.Nanosecond()%1000 != 0 ||
		strings.TrimSpace(item.Title) == "" || item.Title != strings.TrimSpace(item.Title) ||
		utf8.RuneCountInString(item.Title) > 200 || utf8.RuneCountInString(item.Summary) > 600 ||
		len(item.ContentMarkdown) > maxReleaseContentBytes || len(item.Tags) > maxReleaseTagsPerArticle {
		return false
	}
	seenTags := make(map[int64]struct{}, len(item.Tags))
	seenTagSlugs := make(map[string]struct{}, len(item.Tags))
	var previous TagSnapshot
	for index, tag := range item.Tags {
		if tag.ID <= 0 || !utf8.ValidString(tag.Name) || !utf8.ValidString(tag.Slug) || tag.Name == "" ||
			tag.Name != strings.TrimSpace(tag.Name) || utf8.RuneCountInString(tag.Name) > 64 || !releaseTagSlugPattern.MatchString(tag.Slug) {
			return false
		}
		if _, duplicate := seenTags[tag.ID]; duplicate {
			return false
		}
		if index > 0 && (previous.Slug > tag.Slug || previous.Slug == tag.Slug && previous.ID >= tag.ID) {
			return false
		}
		seenTags[tag.ID] = struct{}{}
		if _, duplicate := seenTagSlugs[tag.Slug]; duplicate {
			return false
		}
		seenTagSlugs[tag.Slug] = struct{}{}
		previous = tag
	}
	return true
}

func toSettingsSite(site SiteSnapshot) settings.ReleaseSnapshot {
	socials := make([]settings.SocialLink, len(site.SocialLinks))
	for index, social := range site.SocialLinks {
		socials[index] = settings.SocialLink{Label: social.Label, URL: social.URL}
	}
	return settings.ReleaseSnapshot{
		SiteName: site.Name, AuthorBio: site.AuthorBio, AboutMD: site.AboutMarkdown,
		SocialLinks: socials, FilingName: site.FilingName, FilingNumber: site.FilingNumber,
	}
}

func articlesByID(items []ArticleSnapshot) map[int64]ArticleSnapshot {
	result := make(map[int64]ArticleSnapshot, len(items))
	for _, item := range items {
		if _, duplicate := result[item.ArticleID]; duplicate {
			return nil
		}
		result[item.ArticleID] = item
	}
	return result
}

func articleMapsEqual(left, right map[int64]ArticleSnapshot) bool {
	if len(left) != len(right) {
		return false
	}
	for id, item := range left {
		other, ok := right[id]
		if !ok || !reflect.DeepEqual(item, other) {
			return false
		}
	}
	return true
}

func validateCallbackEvent(event CallbackEvent) error {
	if event.ReleaseID <= 0 || event.PublishJobID <= 0 || event.BuildNumber <= 0 || event.Timestamp.IsZero() ||
		strings.TrimSpace(event.Stage) == "" || utf8.RuneCountInString(event.Stage) > 64 ||
		(event.Status != JobQueued && event.Status != JobBuilding && event.Status != JobDeploying && event.Status != JobSuccess && event.Status != JobFailed) {
		return releaseDomain("apply publish callback", ErrConflict)
	}
	return nil
}

func isPublishBuildDuplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && publishBuildUnique.MatchString(mysqlErr.Message)
}

func allowedTransition(from, to JobStatus) bool {
	switch from {
	case JobPending:
		return to == JobQueued || to == JobFailed
	case JobQueued:
		return to == JobBuilding
	case JobBuilding:
		return to == JobDeploying
	case JobDeploying:
		return to == JobSuccess || to == JobFailed
	default:
		return false
	}
}

func callbackBuildNumber(stored *int64, supplied int64) (*int64, error) {
	if supplied == 0 {
		return cloneInt64(stored), nil
	}
	if stored != nil && *stored != supplied {
		return nil, releaseDomain("apply publish callback", ErrConflict)
	}
	return cloneInt64(&supplied), nil
}

func callbackMatches(job PublishJob, event CallbackEvent) bool {
	if job.Status != event.Status || job.Stage != event.Stage || job.ErrorSummary != event.ErrorSummary {
		return false
	}
	if finalJobStatus(event.Status) && (job.FinishedAt == nil || !job.FinishedAt.Equal(event.Timestamp)) {
		return false
	}
	if event.BuildNumber > 0 {
		return job.BuildNumber != nil && *job.BuildNumber == event.BuildNumber
	}
	return true
}

func finalJobStatus(status JobStatus) bool { return status == JobSuccess || status == JobFailed }

func validStoredRelease(item Release) bool {
	if item.ID <= 0 || item.CreatedAt.IsZero() || !releaseChecksumPattern.MatchString(item.Checksum) {
		return false
	}
	switch item.Status {
	case ReleaseQueued:
		return item.CompletedAt == nil
	case ReleaseSuccess, ReleaseFailed:
		return item.CompletedAt != nil && !item.CompletedAt.IsZero()
	default:
		return false
	}
}

func validStoredJob(job PublishJob) bool {
	if job.ID <= 0 || job.ReleaseID <= 0 || job.BuilderID <= 0 || strings.TrimSpace(job.Stage) == "" || utf8.RuneCountInString(job.Stage) > 64 || job.CreatedAt.IsZero() || utf8.RuneCountInString(job.ErrorSummary) > 512 {
		return false
	}
	if job.BuildNumber != nil && *job.BuildNumber <= 0 {
		return false
	}
	if finalJobStatus(job.Status) {
		return job.FinishedAt != nil && !job.FinishedAt.IsZero()
	}
	if job.FinishedAt != nil {
		return false
	}
	switch job.Status {
	case JobPending, JobQueued, JobBuilding, JobDeploying:
		return true
	default:
		return false
	}
}

func execExactlyOne(ctx context.Context, tx *sql.Tx, operation, statement string, args ...any) error {
	result, err := tx.ExecContext(ctx, statement, args...)
	if err != nil {
		return releaseDependency(operation, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return releaseDependency(operation, err)
	}
	if rows != 1 {
		return releaseDependency(operation, errors.New("unexpected affected row count"))
	}
	return nil
}

func completeRelease(ctx context.Context, tx *sql.Tx, releaseID int64, status ReleaseStatus, at time.Time) error {
	result, err := tx.ExecContext(ctx, updateReleaseFinalSQL, status, at, releaseID)
	if err != nil {
		return releaseDependency("complete release", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return releaseDependency("complete release", err)
	}
	if rows == 1 {
		return nil
	}
	if rows != 0 {
		return releaseDependency("complete release", errors.New("unexpected affected row count"))
	}
	stored, err := scanRelease(tx.QueryRowContext(ctx, releaseForUpdateSQL, releaseID))
	if errors.Is(err, sql.ErrNoRows) {
		return releaseDomain("complete release", ErrNotFound)
	}
	if err != nil {
		return err
	}
	if stored.Status == status && stored.CompletedAt != nil && stored.CompletedAt.Equal(at) {
		return nil
	}
	return releaseDomain("complete release", ErrConflict)
}

func execAnyRows(ctx context.Context, tx *sql.Tx, operation, statement string, args ...any) error {
	result, err := tx.ExecContext(ctx, statement, args...)
	if err != nil {
		return releaseDependency(operation, err)
	}
	if _, err := result.RowsAffected(); err != nil {
		return releaseDependency(operation, err)
	}
	return nil
}

func sanitizeSummary(value string) string {
	var builder strings.Builder
	previousSpace := false
	count := 0
	for _, character := range value {
		if count == 512 {
			break
		}
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			if builder.Len() == 0 || previousSpace {
				continue
			}
			character, previousSpace = ' ', true
		} else {
			previousSpace = false
		}
		builder.WriteRune(character)
		count++
	}
	return strings.TrimSpace(builder.String())
}

func clonePreparedSnapshot(value PreparedSnapshot) PreparedSnapshot {
	return PreparedSnapshot{Site: cloneSiteSnapshot(value.Site), Articles: cloneArticleSnapshots(value.Articles), Checksum: value.Checksum}
}

func cloneSnapshotRequest(value SnapshotRequest) SnapshotRequest {
	value.Base = clonePreparedSnapshot(value.Base)
	return value
}

func cloneSiteSnapshot(value SiteSnapshot) SiteSnapshot {
	clone := value
	if value.SocialLinks != nil {
		clone.SocialLinks = append(make([]SocialLink, 0, len(value.SocialLinks)), value.SocialLinks...)
	}
	return clone
}

func cloneArticleSnapshots(values []ArticleSnapshot) []ArticleSnapshot {
	clones := make([]ArticleSnapshot, len(values))
	for index, value := range values {
		clones[index] = value
		if value.Tags != nil {
			clones[index].Tags = append(make([]TagSnapshot, 0, len(value.Tags)), value.Tags...)
		}
	}
	return clones
}

func cloneRelease(value Release) Release {
	value.Site = cloneSiteSnapshot(value.Site)
	value.CompletedAt = cloneTime(value.CompletedAt)
	return value
}

func cloneAggregate(value Aggregate) Aggregate {
	clone := Aggregate{Release: cloneRelease(value.Release), Jobs: make([]PublishJob, len(value.Jobs))}
	for index, job := range value.Jobs {
		clone.Jobs[index] = clonePublishJob(job)
	}
	return clone
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func decodeSiteSnapshot(raw []byte, target *SiteSnapshot) error {
	if !utf8.Valid(raw) {
		return errors.New("stored site snapshot is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("stored site snapshot must be an object")
	}
	seen := make(map[string]struct{}, 6)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return tokenErr
		}
		field, ok := token.(string)
		if !ok {
			return errors.New("stored site field is invalid")
		}
		if _, duplicate := seen[field]; duplicate {
			return errors.New("stored site field is duplicated")
		}
		seen[field] = struct{}{}
		switch field {
		case "name":
			err = decodeRequiredJSONText(decoder, &target.Name)
		case "authorBio":
			err = decodeRequiredJSONText(decoder, &target.AuthorBio)
		case "aboutMarkdown":
			err = decodeRequiredJSONText(decoder, &target.AboutMarkdown)
		case "filingName":
			err = decodeRequiredJSONText(decoder, &target.FilingName)
		case "filingNumber":
			err = decodeRequiredJSONText(decoder, &target.FilingNumber)
		case "socialLinks":
			target.SocialLinks, err = decodeSocialLinks(decoder)
		default:
			return errors.New("stored site field is unknown")
		}
		if err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return errors.New("stored site snapshot is malformed")
	}
	if len(seen) != 6 || target.SocialLinks == nil {
		return errors.New("stored site snapshot is incomplete")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("stored site snapshot has trailing data")
	}
	if settings.ValidateReleaseSnapshot(toSettingsSite(*target)) != nil {
		return errors.New("stored site snapshot violates domain limits")
	}
	return nil
}

func decodeSocialLinks(decoder *json.Decoder) ([]SocialLink, error) {
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return nil, errors.New("stored social links must be an array")
	}
	links := make([]SocialLink, 0)
	for decoder.More() {
		if len(links) == maxReleaseSocialLinks {
			return links, errors.New("stored social links exceed limit")
		}
		link, decodeErr := decodeSocialLink(decoder)
		if decodeErr != nil {
			return nil, decodeErr
		}
		links = append(links, link)
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return nil, errors.New("stored social links are malformed")
	}
	return links, nil
}

func decodeSocialLink(decoder *json.Decoder) (SocialLink, error) {
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return SocialLink{}, errors.New("stored social link must be an object")
	}
	var link SocialLink
	seen := make(map[string]struct{}, 2)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return SocialLink{}, tokenErr
		}
		field, ok := token.(string)
		if !ok {
			return SocialLink{}, errors.New("stored social link field is invalid")
		}
		if _, duplicate := seen[field]; duplicate {
			return SocialLink{}, errors.New("stored social link field is duplicated")
		}
		seen[field] = struct{}{}
		switch field {
		case "label":
			err = decodeRequiredJSONText(decoder, &link.Label)
		case "url":
			err = decodeRequiredJSONText(decoder, &link.URL)
		default:
			return SocialLink{}, errors.New("stored social link field is unknown")
		}
		if err != nil {
			return SocialLink{}, err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != 2 {
		return SocialLink{}, errors.New("stored social link is incomplete")
	}
	return link, nil
}

func decodeRequiredJSONText(decoder *json.Decoder, target *string) error {
	var value *string
	if err := decoder.Decode(&value); err != nil || value == nil {
		return errors.New("stored JSON text field is invalid")
	}
	*target = *value
	return nil
}

func decodeTags(raw []byte, target *[]TagSnapshot) error {
	if !utf8.Valid(raw) {
		return errors.New("stored tags are not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return errors.New("stored tags must be an array")
	}
	values := make([]TagSnapshot, 0)
	for decoder.More() {
		if len(values) == maxReleaseTagsPerArticle {
			*target = values
			return errors.New("stored tags exceed limit")
		}
		value, decodeErr := decodeTagSnapshot(decoder)
		if decodeErr != nil {
			return decodeErr
		}
		values = append(values, value)
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return errors.New("stored tags are malformed")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("stored tags have trailing data")
	}
	*target = values
	return nil
}

func decodeTagSnapshot(decoder *json.Decoder) (TagSnapshot, error) {
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return TagSnapshot{}, errors.New("stored tag must be an object")
	}
	var value TagSnapshot
	seen := make(map[string]struct{}, 3)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return TagSnapshot{}, tokenErr
		}
		field, ok := token.(string)
		if !ok {
			return TagSnapshot{}, errors.New("stored tag field is invalid")
		}
		if _, duplicate := seen[field]; duplicate {
			return TagSnapshot{}, errors.New("stored tag field is duplicated")
		}
		seen[field] = struct{}{}
		switch field {
		case "ID":
			err = decoder.Decode(&value.ID)
		case "Name":
			err = decodeRequiredJSONText(decoder, &value.Name)
		case "Slug":
			err = decodeRequiredJSONText(decoder, &value.Slug)
		default:
			return TagSnapshot{}, errors.New("stored tag field is unknown")
		}
		if err != nil {
			return TagSnapshot{}, err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != 3 {
		return TagSnapshot{}, errors.New("stored tag is incomplete")
	}
	return value, nil
}

func assembleStoredBundle(rel Release, articles []ArticleSnapshot) Bundle {
	tagsByID := make(map[int64]TagSnapshot)
	bundleArticles := make([]BundleArticle, 0, len(articles))
	for _, article := range articles {
		tags := append(make([]TagSnapshot, 0, len(article.Tags)), article.Tags...)
		sort.Slice(tags, func(i, j int) bool {
			if tags[i].Slug == tags[j].Slug {
				return tags[i].ID < tags[j].ID
			}
			return tags[i].Slug < tags[j].Slug
		})
		tagSlugs := make([]string, len(tags))
		for index, tag := range tags {
			tagsByID[tag.ID] = tag
			tagSlugs[index] = tag.Slug
		}
		bundleArticles = append(bundleArticles, BundleArticle{ArticleID: article.ArticleID, RevisionID: article.RevisionID, Slug: article.Slug, Title: article.Title, Summary: article.Summary, ContentMarkdown: article.ContentMarkdown, ContentHash: article.ContentHash, PublishedAt: article.PublishedAt, Tags: tagSlugs})
	}
	sort.Slice(bundleArticles, func(i, j int) bool {
		if bundleArticles[i].PublishedAt.Equal(bundleArticles[j].PublishedAt) {
			return bundleArticles[i].ArticleID < bundleArticles[j].ArticleID
		}
		return bundleArticles[i].PublishedAt.Before(bundleArticles[j].PublishedAt)
	})
	tags := make([]BundleTag, 0, len(tagsByID))
	for _, tag := range tagsByID {
		tags = append(tags, BundleTag{ID: tag.ID, Name: tag.Name, Slug: tag.Slug})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Slug == tags[j].Slug {
			return tags[i].ID < tags[j].ID
		}
		return tags[i].Slug < tags[j].Slug
	})
	return Bundle{SchemaVersion: 1, ReleaseID: rel.ID, GeneratedAt: rel.CreatedAt, Site: BundleSite(rel.Site), Tags: tags, Articles: bundleArticles, Checksum: rel.Checksum}
}

func (r *MySQLRepository) validate(ctx context.Context) error {
	if r == nil {
		return errors.New("release repository is required")
	}
	if r.initErr != nil {
		return r.initErr
	}
	if r.db == nil || r.ids == nil || nilReleaseInterface(r.snapshots) {
		return errors.New("release repository is not configured")
	}
	if nilReleaseInterface(ctx) {
		return releaseDomain("use release repository", ErrConflict)
	}
	return nil
}

func nilReleaseInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

type releaseSafeError struct {
	operation     string
	domain, cause error
}

func (e *releaseSafeError) Error() string { return e.operation + " failed" }
func (e *releaseSafeError) Unwrap() []error {
	errors := make([]error, 0, 2)
	if e.domain != nil {
		errors = append(errors, e.domain)
	}
	if e.cause != nil {
		errors = append(errors, e.cause)
	}
	return errors
}
func releaseDomain(operation string, domain error) error {
	return &releaseSafeError{operation: operation, domain: domain}
}
func releaseDependency(operation string, cause error) error {
	return &releaseSafeError{operation: operation, domain: ErrDependencyUnavailable, cause: cause}
}
