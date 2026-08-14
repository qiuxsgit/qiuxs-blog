package flow_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/app"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/builder"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/httpapi"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/release"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestImmutableReleaseThroughJenkinsCallbackAndRetry proves the composed HTTP
// surface retains an immutable bundle, cannot advance the current pointer on
// failure, accepts an idempotent replayed callback, and retries with a new
// publish job.
func TestImmutableReleaseThroughJenkinsCallbackAndRetry(t *testing.T) {
	system := newReleaseFlow(t)
	created := system.createReleaseAsAdmin(release.PublishArticle, 41)
	first := system.downloadBundle(created.ReleaseID)

	system.mutateDraftAndSettingsOutsideRelease()
	require.Equal(t, 2, system.liveMutations, "draft and settings must mutate through Admin HTTP")
	require.Equal(t, first.IdentityBody, system.downloadBundle(created.ReleaseID).IdentityBody)

	system.triggerCallback(created, "queue", release.JobQueued, 18, "nonce-queued-000001")
	system.triggerCallback(created, "build", release.JobBuilding, 18, "nonce-building-0001")
	system.triggerCallback(created, "deploy", release.JobDeploying, 18, "nonce-deploying-0001")
	system.triggerCallback(created, "deploy", release.JobFailed, 18, "nonce-failed-0001")
	system.assertPublishedPointerPreserved(42, 17)

	retried := system.retryAsAdmin(created.ReleaseID)
	require.Equal(t, created.ReleaseID, retried.ReleaseID)
	require.NotEqual(t, created.JobID, retried.JobID)
	require.Equal(t, []flowJenkinsCall{{ReleaseID: created.ReleaseID, JobID: created.JobID}, {ReleaseID: created.ReleaseID, JobID: retried.JobID}}, system.jenkinsCalls)
	require.Equal(t, first.ETag, system.downloadBundle(created.ReleaseID).ETag)
}

const (
	flowSiteStateForUpdate = "SELECT current_release_id, active_publish_job_id FROM site_state WHERE singleton_key = ? FOR UPDATE"
	flowReleaseSelect      = "SELECT id, site_snapshot_json, checksum, status, created_at, completed_at FROM releases WHERE id = ?"
	flowReleaseArticles    = "SELECT article_id, revision_id, slug, title, summary, content_md, content_hash, published_at, tags_snapshot_json FROM release_articles WHERE release_id = ? ORDER BY article_id ASC LIMIT 100001"
	flowJobsSelect         = "SELECT id, release_id, builder_id, builder_name, builder_base_url, builder_username, builder_job_name, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE release_id = ? ORDER BY created_at DESC, id DESC"
	flowJobForUpdate       = "SELECT id, release_id, builder_id, builder_name, builder_base_url, builder_username, builder_job_name, status, stage, build_number, error_summary, created_at, finished_at FROM publish_jobs WHERE id = ? AND release_id = ? FOR UPDATE"
	flowSnapshotSite       = "SELECT site_name, author_bio, about_md, social_links_json, filing_name, filing_number FROM site_settings WHERE singleton_key = 1 FOR UPDATE"
	flowSnapshotArticle    = "SELECT slug, draft_revision_id FROM articles WHERE id = ? AND state = 'active' FOR UPDATE"
	flowSnapshotDraft      = "SELECT id, revision_no, title, summary, cover_media_id, content_md, content_hash, lock_version FROM article_revisions WHERE id = ? AND article_id = ? AND status = 'editing' FOR UPDATE"
	flowSnapshotTags       = "SELECT tag_id, tag_name, tag_slug, position FROM article_revision_tags WHERE revision_id = ? ORDER BY position ASC FOR UPDATE"
	flowSnapshotMedia      = "SELECT arm.media_id, m.public_key, arm.purpose, arm.position FROM article_revision_media arm JOIN media m ON m.id = arm.media_id AND m.state = 'active' WHERE arm.revision_id = ? ORDER BY arm.position ASC FOR UPDATE"
	flowFreezeSnapshot     = "UPDATE article_revisions SET status = 'frozen', reason = 'publish_snapshot', updated_at = ? WHERE id = ? AND status = 'editing' AND lock_version = ?"
	flowInsertSnapshot     = "INSERT INTO article_revisions (id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at) VALUES (?, ?, ?, 'editing', 'draft', ?, ?, ?, ?, ?, 1, ?, ?)"
	flowReplaceDraft       = "UPDATE articles SET draft_revision_id = ?, updated_at = ? WHERE id = ? AND draft_revision_id = ? AND state = 'active'"
	flowInsertRelease      = "INSERT INTO releases (id, site_snapshot_json, checksum, status) VALUES (?, ?, ?, 'queued')"
	flowInsertReleaseArt   = "INSERT INTO release_articles (id, release_id, article_id, revision_id, slug, title, summary, content_md, content_hash, published_at, tags_snapshot_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	flowInsertPublishJob   = "INSERT INTO publish_jobs (id, release_id, builder_id, builder_name, builder_base_url, builder_username, builder_job_name, status, stage, error_summary) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 'pending', '')"
	flowSetActive          = "UPDATE site_state SET active_publish_job_id = ? WHERE singleton_key = ? AND active_publish_job_id IS NULL"
	flowUpdateJob          = "UPDATE publish_jobs SET status = ?, stage = ?, build_number = ?, error_summary = ?, finished_at = ? WHERE id = ? AND status = ?"
	flowUpdateReleaseFinal = "UPDATE releases SET status = ?, completed_at = ? WHERE id = ?"
	flowFailActive         = "UPDATE site_state SET active_publish_job_id = NULL WHERE singleton_key = ? AND active_publish_job_id = ?"
)

type releaseFlow struct {
	t                                                             *testing.T
	mock                                                          sqlmock.Sqlmock
	redis                                                         *redis.Client
	mini                                                          *miniredis.Miniredis
	client                                                        *http.Client
	base                                                          string
	cookie                                                        *http.Cookie
	cfg                                                           config.Config
	now                                                           time.Time
	passwordHash, encryptedToken, checksum, articleHash, siteJSON string
	releaseID, jobID, oldCurrentReleaseID                         int64
	oldArticlePublishedAt                                         time.Time
	jobStatus                                                     release.JobStatus
	jobStage                                                      string
	jobBuild, jobFinished, releaseDone                            any
	releaseStatus                                                 release.ReleaseStatus
	jenkinsCalls                                                  []flowJenkinsCall
	liveMutations                                                 int
}

type flowReleaseResult struct{ ReleaseID, JobID int64 }
type flowJenkinsCall struct{ ReleaseID, JobID int64 }
type flowBundle struct {
	IdentityBody []byte
	ETag         string
}

func newReleaseFlow(t *testing.T) *releaseFlow {
	t.Helper()
	flow := &releaseFlow{t: t, now: time.Date(2026, 8, 14, 12, 0, 0, 123000000, time.UTC), releaseID: 101, jobID: 101, oldCurrentReleaseID: 7}
	flow.oldArticlePublishedAt = flow.now.Add(-time.Hour)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	flow.mock = mock
	t.Cleanup(func() { _ = db.Close() })
	flow.mini = miniredis.RunT(t)
	flow.redis = redis.NewClient(&redis.Options{Addr: flow.mini.Addr()})
	t.Cleanup(func() { require.NoError(t, flow.redis.Close()); require.NoError(t, mock.ExpectationsWereMet()) })
	flow.cfg = flowConfig(flow.mini.Addr())
	flow.cfg.IDGen.Offset, flow.cfg.IDGen.Step = flow.releaseID, flow.releaseID
	flow.passwordHash, err = auth.DefaultPasswordHasher().Hash(adminPassword)
	require.NoError(t, err)
	box, err := platform.NewSecretBox(flow.cfg.Release.BuilderMasterKey, bytes.NewReader(bytes.Repeat([]byte{7}, 32)))
	require.NoError(t, err)
	flow.encryptedToken, err = box.Seal([]byte("jenkins-token"))
	require.NoError(t, err)
	flow.articleHash = revision.ComputeHash(revision.PreparedContent{Title: "Immutable", Summary: "Snapshot", ContentMD: "Body", Tags: []tag.Snapshot{}, Media: []media.Reference{}})
	flow.siteJSON = `{"name":"Blog","authorBio":"Bio","aboutMarkdown":"About","filingName":"Filing","filingNumber":"Filing-1","socialLinks":[]}`
	// Reviewed checksum for this fixed immutable fixture; the flow does not
	// duplicate the release package's canonicalization implementation.
	flow.checksum = "sha256:b207ebaaa205494636f8db129a1f3bea8831e3acc4301ae6608ea40772cadf1b"
	router, err := app.Build(flow.cfg, app.Dependencies{
		DB: db, Redis: flow.redis, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Random: bytes.NewReader(bytes.Repeat([]byte{3}, 1024)), Now: func() time.Time { return flow.now },
		HTTPClient: &http.Client{Timeout: 5 * time.Second, Transport: http.DefaultTransport}, JenkinsHTTPClient: &http.Client{Transport: flowJenkinsTransport{flow}},
		ReleaseJSONReader: func() (io.ReadCloser, error) { return nil, fs.ErrNotExist },
	})
	require.NoError(t, err)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	flow.client, flow.base = server.Client(), server.URL
	seedFlowAdminSession(t, flow.redis, flow.now, flow.cfg.Session.CookieName, &flow.cookie)
	return flow
}

func (flow *releaseFlow) createReleaseAsAdmin(mode release.PublishMode, articleID int64) flowReleaseResult {
	flow.expectAdmin()
	flow.expectBuilderLoad()
	flow.expectCreate()
	response := flow.adminJSON(http.MethodPost, "/api/admin/v1/releases", map[string]any{"mode": mode, "articleId": articleID})
	require.Equal(flow.t, http.StatusAccepted, response.StatusCode)
	var created httpapi.CreateReleaseResult
	decodeResponse(flow.t, response, &created)
	require.Equal(flow.t, flow.releaseID, created.Release.Id)
	require.Equal(flow.t, flow.jobID, created.Job.Id)
	flow.jobStatus, flow.jobStage, flow.jobBuild, flow.jobFinished = release.JobPending, "pending", nil, nil
	flow.releaseStatus, flow.releaseDone = release.ReleaseQueued, nil
	return flowReleaseResult{created.Release.Id, created.Job.Id}
}

func (flow *releaseFlow) mutateDraftAndSettingsOutsideRelease() {
	changed := revision.PreparedContent{Title: "Changed Draft", Summary: "Changed live summary", ContentMD: "Changed live body", Tags: []tag.Snapshot{}, Media: []media.Reference{}}
	changedHash := revision.ComputeHash(changed)
	flow.expectAdmin()
	flow.mock.ExpectBegin()
	expectReleaseQuery(flow.mock, flowSelectArticlePointer).WithArgs(int64(41)).WillReturnRows(
		sqlmock.NewRows([]string{"state", "draft_revision_id"}).AddRow("active", int64(101)),
	)
	expectReleaseQuery(flow.mock, flowSelectCurrent).WithArgs(int64(101), int64(41)).WillReturnRows(flowRevisionRows().AddRow(
		int64(101), int64(41), int64(2), "editing", "draft", "Immutable", "Snapshot", nil, "Body", flow.articleHash, int64(1), flow.now, flow.now,
	))
	expectReleaseExec(flow.mock, flowUpdateDraft).WithArgs(changed.Title, changed.Summary, nil, changed.ContentMD, changedHash, flow.now, int64(41), int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseQuery(flow.mock, flowSelectSavedIdentity).WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(101), int64(2), int64(2), flow.now))
	expectReleaseExec(flow.mock, flowDeleteDraftTags).WithArgs(int64(101)).WillReturnResult(sqlmock.NewResult(0, 0))
	expectReleaseExec(flow.mock, flowDeleteDraftMedia).WithArgs(int64(101)).WillReturnResult(sqlmock.NewResult(0, 0))
	expectReleaseExec(flow.mock, flowTouchArticle).WithArgs(flow.now, int64(41)).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.mock.ExpectCommit()
	response := flow.adminJSON(http.MethodPut, "/api/admin/v1/articles/41/draft", map[string]any{
		"lockVersion": 1, "title": changed.Title, "summary": changed.Summary, "coverMediaId": nil, "contentMd": changed.ContentMD, "tagIds": []int64{},
	})
	require.Equal(flow.t, http.StatusOK, response.StatusCode)
	var draft httpapi.DraftView
	decodeResponse(flow.t, response, &draft)
	require.Equal(flow.t, changed.Title, draft.Title)
	flow.liveMutations++

	flow.expectAdmin()
	flow.mock.ExpectBegin()
	expectReleaseExec(flow.mock, flowUpdateSite).WithArgs(
		"qiuxs", "qiuxs", "Changed live settings", "shipping", "# About\n", `[{"label":"GitHub","url":"https://github.com/qiuxsgit"}]`,
		"qiuxs blog", "Service content", nil, "长安休息室", "浙ICP备17057726号-1", flow.now, int64(1),
	).WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseQuery(flow.mock, flowSelectSite).WillReturnRows(flowSiteRows(2, "Changed live settings", flow.now))
	flow.mock.ExpectCommit()
	response = flow.adminJSON(http.MethodPut, "/api/admin/v1/settings/site", siteWrite(1, "Changed live settings"))
	require.Equal(flow.t, http.StatusOK, response.StatusCode)
	var site httpapi.SiteSettingsView
	decodeResponse(flow.t, response, &site)
	require.Equal(flow.t, "Changed live settings", site.AuthorBio)
	flow.liveMutations++
}

func (flow *releaseFlow) downloadBundle(releaseID int64) flowBundle {
	flow.expectBundleRead(releaseID)
	identity := flow.bundleRequest(releaseID, "identity")
	flow.expectBundleRead(releaseID)
	gzipped := flow.bundleRequest(releaseID, "gzip")
	require.Equal(flow.t, identity.IdentityBody, gzipped.IdentityBody)
	require.Equal(flow.t, identity.ETag, gzipped.ETag)
	return identity
}

func (flow *releaseFlow) bundleRequest(releaseID int64, encoding string) flowBundle {
	request, err := http.NewRequest(http.MethodGet, flow.base+"/api/internal/v1/releases/"+strconv.FormatInt(releaseID, 10)+"/bundle", nil)
	require.NoError(flow.t, err)
	request.Header.Set("Authorization", "Bearer "+string(flow.cfg.Release.BundleToken))
	request.Header.Set("Accept-Encoding", encoding)
	response, err := flow.client.Do(request)
	require.NoError(flow.t, err)
	require.Equal(flow.t, http.StatusOK, response.StatusCode)
	require.Equal(flow.t, `"`+flow.checksum+`"`, response.Header.Get("ETag"))
	var reader io.Reader = response.Body
	if encoding == "gzip" {
		gzipReader, gzipErr := gzip.NewReader(response.Body)
		require.NoError(flow.t, gzipErr)
		defer gzipReader.Close()
		reader = gzipReader
	}
	body, readErr := io.ReadAll(reader)
	require.NoError(flow.t, readErr)
	require.NoError(flow.t, response.Body.Close())
	return flowBundle{body, flow.checksum}
}

func (flow *releaseFlow) triggerCallback(created flowReleaseResult, stage string, status release.JobStatus, build int64, nonce string) {
	flow.expectCallback(status, stage, build)
	payload := builder.CallbackPayload{ReleaseID: created.ReleaseID, PublishJobID: created.JobID, BuildNumber: build, Stage: stage, Status: status, Timestamp: flow.now, Nonce: nonce}
	raw, err := json.Marshal(payload)
	require.NoError(flow.t, err)
	canonical := append([]byte(strconv.FormatInt(flow.now.Unix(), 10)+"\n"+nonce+"\n"), raw...)
	request, err := http.NewRequest(http.MethodPost, flow.base+"/api/internal/v1/jenkins/callback", bytes.NewReader(raw))
	require.NoError(flow.t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Jenkins-Signature", "sha256="+platform.ComputeHMAC(flow.cfg.Release.CallbackHMACKey, canonical))
	response, err := flow.client.Do(request)
	require.NoError(flow.t, err)
	require.Equal(flow.t, http.StatusNoContent, response.StatusCode)
	require.NoError(flow.t, response.Body.Close())
	if status == release.JobFailed {
		// Redis marks the same canonical body as duplicate, but the request still
		// reaches the database transaction where exact job state proves idempotency.
		flow.expectCallbackDuplicate(status, stage, build)
		replay := request.Clone(context.Background())
		replay.Body, err = request.GetBody()
		require.NoError(flow.t, err)
		response, err = flow.client.Do(replay)
		require.NoError(flow.t, err)
		require.Equal(flow.t, http.StatusNoContent, response.StatusCode)
		require.NoError(flow.t, response.Body.Close())
	}
	flow.jobStatus, flow.jobStage, flow.jobBuild = status, stage, build
	if status == release.JobFailed {
		flow.jobFinished, flow.releaseStatus, flow.releaseDone = flow.now, release.ReleaseFailed, flow.now
	}
}

func (flow *releaseFlow) expectCallbackDuplicate(status release.JobStatus, stage string, build int64) {
	flow.mock.ExpectBegin()
	expectReleaseQuery(flow.mock, flowSiteStateForUpdate).WithArgs(1).WillReturnRows(
		sqlmock.NewRows([]string{"current_release_id", "active_publish_job_id"}).AddRow(flow.oldCurrentReleaseID, nil),
	)
	expectReleaseQuery(flow.mock, flowJobForUpdate).WithArgs(flow.jobID, flow.releaseID).WillReturnRows(
		flow.jobRows(flow.jobID, status, stage, build, flow.now),
	)
	flow.mock.ExpectCommit()
}

func (flow *releaseFlow) retryAsAdmin(releaseID int64) flowReleaseResult {
	flow.expectAdmin()
	flow.expectBuilderLoad()
	flow.expectRetry(releaseID)
	response := flow.adminJSON(http.MethodPost, "/api/admin/v1/releases/"+strconv.FormatInt(releaseID, 10)+"/retry", nil)
	require.Equal(flow.t, http.StatusAccepted, response.StatusCode)
	var retried httpapi.RetryReleaseResult
	decodeResponse(flow.t, response, &retried)
	flow.jobID, flow.jobStatus, flow.jobStage, flow.jobBuild, flow.jobFinished = retried.Job.Id, release.JobPending, "pending", nil, nil
	return flowReleaseResult{retried.Release.Id, retried.Job.Id}
}

func (flow *releaseFlow) assertPublishedPointerPreserved(articleID, revisionID int64) {
	flow.expectAdmin()
	expectReleaseQuery(flow.mock, flowSelectArticle).WithArgs(int64(articleID)).WillReturnRows(sqlmock.NewRows(strings.Split(flowArticleCols, ", ")).AddRow(
		int64(articleID), "oldarticle42", int64(18), int64(revisionID), "active", flow.now, flow.now,
	))
	flow.mock.ExpectBegin()
	expectReleaseQuery(flow.mock, flowSelectEditingAt).WithArgs(int64(18), int64(articleID)).WillReturnRows(flowRevisionRows().AddRow(
		int64(18), int64(articleID), int64(2), "editing", "draft", "Old live draft", "", nil, "Old live body", flow.articleHash, int64(1), flow.now, flow.now,
	))
	expectReleaseQuery(flow.mock, flowSelectDraftTags).WithArgs(int64(18)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}))
	expectReleaseQuery(flow.mock, flowSelectDraftMedia).WithArgs(int64(18)).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}))
	flow.mock.ExpectCommit()
	response := flow.adminJSON(http.MethodGet, "/api/admin/v1/articles/"+strconv.FormatInt(int64(articleID), 10), nil)
	require.Equal(flow.t, http.StatusOK, response.StatusCode)
	var detail httpapi.ArticleDetail
	decodeResponse(flow.t, response, &detail)
	require.NotNil(flow.t, detail.PublishedRevisionId)
	require.Equal(flow.t, int64(revisionID), *detail.PublishedRevisionId)
}

func (flow *releaseFlow) adminJSON(method, path string, body any) *http.Response {
	return doJSON(flow.t, flow.client, method, flow.base+path, adminOrigin, flow.cookie, body)
}

func (flow *releaseFlow) expectAdmin() {
	expectReleaseQuery(flow.mock, selectAdminPrefix+" WHERE id = ?").WithArgs(int64(41)).WillReturnRows(adminRows(flow.passwordHash))
}

func (flow *releaseFlow) expectBuilderLoad() {
	expectReleaseQuery(flow.mock, "SELECT id, name, base_url, username, token_ciphertext, job_name, enabled FROM builder_config WHERE singleton_key = 1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "base_url", "username", "token_ciphertext", "job_name", "enabled"}).
			AddRow(int64(9), "Production", "https://jenkins.example.test", "ci", flow.encryptedToken, "blog/deploy", true))
}

func (flow *releaseFlow) expectCreate() {
	flow.mock.ExpectBegin()
	expectReleaseQuery(flow.mock, flowSiteStateForUpdate).WithArgs(1).WillReturnRows(sqlmock.NewRows([]string{"current_release_id", "active_publish_job_id"}).AddRow(flow.oldCurrentReleaseID, nil))
	expectReleaseQuery(flow.mock, flowReleaseSelect).WithArgs(flow.oldCurrentReleaseID).WillReturnRows(flow.releaseRows(flow.oldCurrentReleaseID, release.ReleaseSuccess, flow.now))
	expectReleaseQuery(flow.mock, flowReleaseArticles).WithArgs(flow.oldCurrentReleaseID).WillReturnRows(flow.oldReleaseArticleRows())
	expectReleaseQuery(flow.mock, flowSnapshotArticle).WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"slug", "draft_revision_id"}).AddRow("article_slug", int64(71)))
	expectReleaseQuery(flow.mock, flowSnapshotDraft).WithArgs(int64(71), int64(41)).WillReturnRows(sqlmock.NewRows([]string{"id", "revision_no", "title", "summary", "cover_media_id", "content_md", "content_hash", "lock_version"}).AddRow(int64(71), int64(1), "Immutable", "Snapshot", nil, "Body", flow.articleHash, int64(1)))
	expectReleaseQuery(flow.mock, flowSnapshotTags).WithArgs(int64(71)).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}))
	expectReleaseQuery(flow.mock, flowSnapshotMedia).WithArgs(int64(71)).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}))
	expectReleaseExec(flow.mock, flowFreezeSnapshot).WithArgs(flow.now, int64(71), int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseExec(flow.mock, flowInsertSnapshot).WithArgs(int64(101), int64(41), int64(2), "Immutable", "Snapshot", nil, "Body", flow.articleHash, flow.now, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseExec(flow.mock, flowReplaceDraft).WithArgs(int64(101), flow.now, int64(41), int64(71)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseExec(flow.mock, flowInsertRelease).WithArgs(int64(101), flow.siteJSON, flow.checksum).WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseExec(flow.mock, flowInsertReleaseArt).WithArgs(int64(101), int64(101), int64(41), int64(71), "article_slug", "Immutable", "Snapshot", "Body", "sha256:"+flow.articleHash, flow.now, "[]").WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseExec(flow.mock, flowInsertReleaseArt).WithArgs(int64(202), int64(101), int64(42), int64(17), "oldarticle42", "Old published", "Old snapshot", "Old body", "sha256:"+flow.articleHash, flow.oldArticlePublishedAt, "[]").WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseExec(flow.mock, flowInsertPublishJob).WithArgs(int64(101), int64(101), int64(9), "Production", "https://jenkins.example.test", "ci", "blog/deploy").WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseExec(flow.mock, flowSetActive).WithArgs(int64(101), 1).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.expectAggregateRows(flow.releaseID, release.ReleaseQueued, nil, []flowJob{{id: 101, status: release.JobPending, stage: "pending"}})
	flow.mock.ExpectCommit()
}

type flowJob struct {
	id              int64
	status          release.JobStatus
	stage           string
	build, finished any
}

func (flow *releaseFlow) expectAggregateRows(id int64, status release.ReleaseStatus, completed any, jobs []flowJob) {
	expectReleaseQuery(flow.mock, flowReleaseSelect).WithArgs(id).WillReturnRows(flow.releaseRows(id, status, completed))
	rows := sqlmock.NewRows([]string{"id", "release_id", "builder_id", "builder_name", "builder_base_url", "builder_username", "builder_job_name", "status", "stage", "build_number", "error_summary", "created_at", "finished_at"})
	for _, job := range jobs {
		rows.AddRow(job.id, id, int64(9), "Production", "https://jenkins.example.test", "ci", "blog/deploy", job.status, job.stage, job.build, "", flow.now, job.finished)
	}
	expectReleaseQuery(flow.mock, flowJobsSelect).WithArgs(id).WillReturnRows(rows)
}

func (flow *releaseFlow) expectBundleRead(releaseID int64) {
	flow.mock.ExpectBegin()
	flow.expectAggregateRows(releaseID, flow.releaseStatus, flow.releaseDone, []flowJob{{id: flow.jobID, status: flow.jobStatus, stage: flow.jobStage, build: flow.jobBuild, finished: flow.jobFinished}})
	expectReleaseQuery(flow.mock, flowReleaseArticles).WithArgs(releaseID).WillReturnRows(flow.newReleaseArticleRows())
	flow.mock.ExpectCommit()
}

func (flow *releaseFlow) expectCallback(status release.JobStatus, stage string, build int64) {
	flow.mock.ExpectBegin()
	expectReleaseQuery(flow.mock, flowSiteStateForUpdate).WithArgs(1).WillReturnRows(sqlmock.NewRows([]string{"current_release_id", "active_publish_job_id"}).AddRow(flow.oldCurrentReleaseID, flow.jobID))
	expectReleaseQuery(flow.mock, flowJobForUpdate).WithArgs(flow.jobID, flow.releaseID).WillReturnRows(flow.jobRows(flow.jobID, flow.jobStatus, flow.jobStage, flow.jobBuild, flow.jobFinished))
	finished := any(nil)
	if status == release.JobFailed {
		finished = flow.now
	}
	expectReleaseExec(flow.mock, flowUpdateJob).WithArgs(status, stage, build, "", finished, flow.jobID, flow.jobStatus).WillReturnResult(sqlmock.NewResult(0, 1))
	if status == release.JobFailed {
		expectReleaseExec(flow.mock, flowUpdateReleaseFinal).WithArgs(release.ReleaseFailed, flow.now, flow.releaseID).WillReturnResult(sqlmock.NewResult(0, 1))
		// No current-release or article-pointer update is expected: failed
		// callbacks may only clear the active lock and preserve the old release.
		expectReleaseExec(flow.mock, flowFailActive).WithArgs(1, flow.jobID).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	flow.mock.ExpectCommit()
}

func (flow *releaseFlow) expectRetry(releaseID int64) {
	newJobID := flow.jobID + flow.cfg.IDGen.Step
	flow.mock.ExpectBegin()
	expectReleaseQuery(flow.mock, flowSiteStateForUpdate).WithArgs(1).WillReturnRows(sqlmock.NewRows([]string{"current_release_id", "active_publish_job_id"}).AddRow(flow.oldCurrentReleaseID, nil))
	flow.expectAggregateRows(releaseID, release.ReleaseFailed, flow.now, []flowJob{{id: flow.jobID, status: release.JobFailed, stage: "deploy", build: int64(18), finished: flow.now}})
	expectReleaseExec(flow.mock, flowInsertPublishJob).WithArgs(newJobID, releaseID, int64(9), "Production", "https://jenkins.example.test", "ci", "blog/deploy").WillReturnResult(sqlmock.NewResult(0, 1))
	expectReleaseExec(flow.mock, flowSetActive).WithArgs(newJobID, 1).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.expectAggregateRows(releaseID, release.ReleaseFailed, flow.now, []flowJob{{id: newJobID, status: release.JobPending, stage: "pending"}, {id: flow.jobID, status: release.JobFailed, stage: "deploy", build: int64(18), finished: flow.now}})
	flow.mock.ExpectCommit()
}

func (flow *releaseFlow) releaseRows(id int64, status release.ReleaseStatus, completed any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "site_snapshot_json", "checksum", "status", "created_at", "completed_at"}).AddRow(id, flow.siteJSON, flow.checksum, status, flow.now, completed)
}

func (flow *releaseFlow) oldReleaseArticleRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"article_id", "revision_id", "slug", "title", "summary", "content_md", "content_hash", "published_at", "tags_snapshot_json"}).AddRow(
		int64(42), int64(17), "oldarticle42", "Old published", "Old snapshot", "Old body", "sha256:"+flow.articleHash, flow.oldArticlePublishedAt, "[]",
	)
}

func (flow *releaseFlow) newReleaseArticleRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"article_id", "revision_id", "slug", "title", "summary", "content_md", "content_hash", "published_at", "tags_snapshot_json"}).
		AddRow(int64(41), int64(71), "article_slug", "Immutable", "Snapshot", "Body", "sha256:"+flow.articleHash, flow.now, "[]").
		AddRow(int64(42), int64(17), "oldarticle42", "Old published", "Old snapshot", "Old body", "sha256:"+flow.articleHash, flow.oldArticlePublishedAt, "[]")
}

func (flow *releaseFlow) jobRows(id int64, status release.JobStatus, stage string, build, finished any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "release_id", "builder_id", "builder_name", "builder_base_url", "builder_username", "builder_job_name", "status", "stage", "build_number", "error_summary", "created_at", "finished_at"}).
		AddRow(id, flow.releaseID, int64(9), "Production", "https://jenkins.example.test", "ci", "blog/deploy", status, stage, build, "", flow.now, finished)
}

type flowJenkinsTransport struct{ flow *releaseFlow }

func (transport flowJenkinsTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || request.URL.String() != "https://jenkins.example.test/job/blog/job/deploy/buildWithParameters" {
		return nil, io.ErrUnexpectedEOF
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	if values.Get("RELEASE_ID") != strconv.FormatInt(transport.flow.releaseID, 10) || values.Get("PUBLISH_JOB_ID") == "" {
		return nil, io.ErrUnexpectedEOF
	}
	releaseID, releaseErr := strconv.ParseInt(values.Get("RELEASE_ID"), 10, 64)
	jobID, jobErr := strconv.ParseInt(values.Get("PUBLISH_JOB_ID"), 10, 64)
	if releaseErr != nil || jobErr != nil {
		return nil, io.ErrUnexpectedEOF
	}
	transport.flow.jenkinsCalls = append(transport.flow.jenkinsCalls, flowJenkinsCall{ReleaseID: releaseID, JobID: jobID})
	return &http.Response{StatusCode: http.StatusCreated, Header: http.Header{"X-Jenkins-Queue-Id": []string{"18"}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
}

func expectReleaseQuery(mock sqlmock.Sqlmock, statement string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery("^" + regexp.QuoteMeta(statement) + "$")
}
func expectReleaseExec(mock sqlmock.Sqlmock, statement string) *sqlmock.ExpectedExec {
	return mock.ExpectExec("^" + regexp.QuoteMeta(statement) + "$")
}

func seedFlowAdminSession(t *testing.T, client *redis.Client, now time.Time, cookieName string, cookie **http.Cookie) {
	t.Helper()
	token := "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
	digest := sha256.Sum256([]byte(token))
	encoded, err := json.Marshal(auth.Session{AdminID: 41, Username: adminUsername, ExpiresAt: now.Add(time.Hour)})
	require.NoError(t, err)
	require.NoError(t, client.Set(context.Background(), "qiuxs-blog:session:"+hex.EncodeToString(digest[:]), encoded, time.Hour).Err())
	*cookie = &http.Cookie{Name: cookieName, Value: token, Path: "/api/admin/v1", Secure: true, HttpOnly: true}
}
