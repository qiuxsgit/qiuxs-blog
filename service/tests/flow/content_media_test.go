package flow_test

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/app"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/httpapi"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

const (
	flowMediaColumns = "id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at"
	flowArticleCols  = "id, slug, draft_revision_id, published_revision_id, state, created_at, updated_at"
	flowRevisionCols = "id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at"

	flowFindMediaByGFS       = "SELECT " + flowMediaColumns + " FROM media WHERE gfs_file_id = ?"
	flowFindActiveMediaByID  = "SELECT " + flowMediaColumns + " FROM media WHERE id = ? AND state = 'active'"
	flowFindActiveMediaByKey = "SELECT " + flowMediaColumns + " FROM media WHERE public_key = ? AND state = 'active'"
	flowFindActiveMediaKeys  = "SELECT " + flowMediaColumns + " FROM media WHERE public_key IN (?) AND state = 'active'"
	flowInsertMedia          = "INSERT INTO media (id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)"

	flowInsertTag  = "INSERT INTO tags (id, name, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?)"
	flowFindTagIDs = "SELECT id, name, slug, created_at, updated_at FROM tags WHERE id IN (?) ORDER BY id ASC"
	flowRenameTag  = "UPDATE tags SET name = ?, updated_at = ? WHERE id = ?"
	flowFindTagID  = "SELECT id, name, slug, created_at, updated_at FROM tags WHERE id = ?"

	flowInsertArticle        = "INSERT INTO articles (id, slug, draft_revision_id, published_revision_id, state, created_at, updated_at) VALUES (?, ?, NULL, NULL, 'active', ?, ?)"
	flowInsertInitialDraft   = "INSERT INTO article_revisions (id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at) VALUES (?, ?, 1, 'editing', 'draft', '', '', NULL, '', ?, 1, ?, ?)"
	flowSetInitialDraft      = "UPDATE articles SET draft_revision_id = ?, updated_at = ? WHERE id = ? AND draft_revision_id IS NULL"
	flowSelectArticle        = "SELECT " + flowArticleCols + " FROM articles WHERE id = ?"
	flowTrashArticle         = "UPDATE articles SET state = 'trashed', updated_at = ? WHERE id = ? AND state = 'active' AND published_revision_id IS NULL"
	flowUntrashArticle       = "UPDATE articles SET state = 'active', updated_at = ? WHERE id = ? AND state = 'trashed'"
	flowSelectEditingDraft   = "SELECT " + flowRevisionCols + " FROM article_revisions WHERE article_id = ? AND status = 'editing'"
	flowSelectEditingAt      = "SELECT " + flowRevisionCols + " FROM article_revisions WHERE id = ? AND article_id = ? AND status = 'editing'"
	flowSelectDraftTags      = "SELECT tag_id, tag_name, tag_slug, position FROM article_revision_tags WHERE revision_id = ? ORDER BY position ASC"
	flowSelectDraftMedia     = "SELECT arm.media_id, m.public_key, arm.purpose, arm.position FROM article_revision_media arm JOIN media m ON m.id = arm.media_id WHERE arm.revision_id = ? ORDER BY arm.position ASC"
	flowUpdateDraft          = "UPDATE article_revisions SET title = ?, summary = ?, cover_media_id = ?, content_md = ?, content_hash = ?, lock_version = lock_version + 1, updated_at = ? WHERE article_id = ? AND status = 'editing' AND lock_version = ?"
	flowSelectSavedIdentity  = "SELECT id, lock_version, revision_no, created_at FROM article_revisions WHERE article_id = ? AND status = 'editing'"
	flowDeleteDraftTags      = "DELETE FROM article_revision_tags WHERE revision_id = ?"
	flowInsertDraftTag       = "INSERT INTO article_revision_tags (id, revision_id, tag_id, tag_name, tag_slug, position, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
	flowDeleteDraftMedia     = "DELETE FROM article_revision_media WHERE revision_id = ?"
	flowInsertDraftMedia     = "INSERT INTO article_revision_media (id, revision_id, media_id, purpose, position, created_at) VALUES (?, ?, ?, ?, ?, ?)"
	flowTouchArticle         = "UPDATE articles SET updated_at = ? WHERE id = ? AND state = 'active'"
	flowSelectArticlePointer = "SELECT state, draft_revision_id FROM articles WHERE id = ? FOR UPDATE"
	flowSelectCurrent        = "SELECT " + flowRevisionCols + " FROM article_revisions WHERE id = ? AND article_id = ? AND status = 'editing' FOR UPDATE"
	flowFreezeVersion        = "UPDATE article_revisions SET status = 'frozen', reason = 'manual_version', updated_at = ? WHERE id = ? AND status = 'editing' AND lock_version = ?"
	flowInsertEditing        = "INSERT INTO article_revisions (id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at) VALUES (?, ?, ?, 'editing', 'draft', ?, ?, ?, ?, ?, 1, ?, ?)"
	flowReplaceDraftPointer  = "UPDATE articles SET draft_revision_id = ?, updated_at = ? WHERE id = ? AND draft_revision_id = ? AND state = 'active'"
	flowSelectFrozen         = "SELECT " + flowRevisionCols + " FROM article_revisions WHERE id = ? AND article_id = ? AND status = 'frozen'"
	flowListFrozen           = "SELECT " + flowRevisionCols + " FROM article_revisions WHERE article_id = ? AND status = 'frozen' ORDER BY revision_no DESC"

	flowSelectSite = "SELECT id, site_name, author_name, author_bio, home_status, about_md, social_links_json, seo_default_title, seo_default_description, seo_default_image_media_id, filing_name, filing_number, lock_version, updated_at FROM site_settings WHERE singleton_key = 1"
	flowInsertSite = "INSERT INTO site_settings (id, singleton_key, site_name, author_name, author_bio, home_status, about_md, social_links_json, seo_default_title, seo_default_description, seo_default_image_media_id, filing_name, filing_number, lock_version, created_at, updated_at) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)"
	flowUpdateSite = "UPDATE site_settings SET site_name=?, author_name=?, author_bio=?, home_status=?, about_md=?, social_links_json=?, seo_default_title=?, seo_default_description=?, seo_default_image_media_id=?, filing_name=?, filing_number=?, lock_version=lock_version+1, updated_at=? WHERE singleton_key=1 AND lock_version=?"

	flowSelectHotlink        = "SELECT id, allow_empty_referer FROM hotlink_settings WHERE singleton_key = 1"
	flowSelectHotlinkEntries = "SELECT id, hostname, enabled FROM referer_allowlist ORDER BY hostname ASC, id ASC"
	flowUpdateHotlink        = "UPDATE hotlink_settings SET allow_empty_referer=?, updated_at=? WHERE singleton_key=1"
	flowLockHotlink          = "SELECT id FROM hotlink_settings WHERE singleton_key=1 FOR UPDATE"
	flowInsertHotlink        = "INSERT INTO hotlink_settings (id, singleton_key, allow_empty_referer, created_at, updated_at) VALUES (?, 1, ?, ?, ?)"
	flowDeleteHotlinkEntries = "DELETE FROM referer_allowlist"
	flowInsertHotlinkEntry   = "INSERT INTO referer_allowlist (id, hostname, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?)"

	flowMediaKey       = "m_aaaaaaaaaaaaaaaaaaaaaa"
	flowTagSlug        = "t_aaaaaaaaaaaa"
	flowArticleSlug    = "aaaaaaaaaaaa"
	flowDraftTitle     = "A GFM Flow"
	flowDraftSummary   = "First snapshot"
	flowDraftBody      = "# A GFM Flow\n\n![hero](/img/proxy/" + flowMediaKey + ")\n\n~~verified~~\n"
	flowChangedTitle   = "A Changed Draft"
	flowChangedSummary = "Current snapshot"
	flowChangedBody    = "# A Changed Draft\n\n![hero](/img/proxy/" + flowMediaKey + ")\n\nCurrent text.\n"
	flowDraftHash      = "96557411ca40604fe3ed460eefaab4dd323754b0a7cc0bb149abcd3725887371"
	flowChangedHash    = "f0685a21ae30ac98694e81f73eba1ff0a47d032a3c183eb72d1fcc9dca709871"
	flowReadPolicy     = "eyJ1c2VySWQiOiIiLCJmaWxlSWQiOjkxLCJpbWFnZVdpZHRoIjowLCJpbWFnZUhlaWdodCI6MCwiaW50ZXJuYWxGbGFnIjowfQ=="
	flowReadTimestamp  = "1786694950"
	flowReadSignature  = "c3dd99c645497ce0508f3ee0645555a4"
)

func TestContentRevisionMediaAndHotlinkFlow(t *testing.T) {
	flow := newContentMediaFlow(t)

	// 1. Login through the real auth stack and manually retain the Secure cookie.
	flow.expectLogin()
	response := doJSON(t, flow.client, http.MethodPost, flow.baseURL+"/api/admin/v1/session", adminOrigin, nil, map[string]string{
		"username": adminUsername,
		"password": adminPassword,
	})
	require.Equal(t, http.StatusOK, response.StatusCode)
	var loggedIn httpapi.AdminView
	decodeResponse(t, response, &loggedIn)
	require.Equal(t, httpapi.AdminView{Id: 1, Username: adminUsername}, loggedIn)
	cookies := response.Cookies()
	require.Len(t, cookies, 1)
	flow.cookie = cookies[0]
	require.True(t, flow.cookie.Secure)
	require.True(t, flow.cookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, flow.cookie.SameSite)
	require.Equal(t, "/api/admin/v1", flow.cookie.Path)

	// 2. Upload signing is local and exposes only the fixed GFS policy contract.
	flow.expectAdmin()
	response = flow.adminJSON(http.MethodPost, "/api/admin/v1/media/upload-policy", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	policyBody := readResponseBody(t, response)
	var policy httpapi.MediaUploadPolicy
	require.NoError(t, json.Unmarshal(policyBody, &policy))
	require.Equal(t, flow.gfs.URL+"/v1/upload", policy.UploadUrl)
	require.Equal(t, "60", policy.Expire)
	require.Equal(t, "file", policy.FileField)
	require.Equal(t, strings.Repeat("a", 22), policy.Nonce)
	decodedPolicy, err := base64.StdEncoding.DecodeString(policy.Policy)
	require.NoError(t, err)
	var policyFields map[string]string
	require.NoError(t, json.Unmarshal(decodedPolicy, &policyFields))
	require.Equal(t, map[string]string{"savePath": "blog/{{year}}/{{month}}/{{uuid}}.{{fileExt}}"}, policyFields)
	require.NotContains(t, string(policyBody), flow.cfg.GFS.AppSecret)
	require.NotContains(t, string(policyBody), flow.cfg.GFS.PublicReadSecret)
	require.Zero(t, flow.gfsMetadataCalls.Load())

	// 3. Register file 91 from actual fake-GFS metadata.
	flow.expectAdmin()
	expectQuery(flow.mock, flowFindMediaByGFS).WithArgs(int64(91)).WillReturnError(sql.ErrNoRows)
	expectExec(flow.mock, flowInsertMedia).WithArgs(
		int64(1), flowMediaKey, int64(91), "hero.png", "image/png", int64(1024), 640, 480, flow.now, flow.now,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	response = flow.adminJSON(http.MethodPost, "/api/admin/v1/media", map[string]any{"gfsFileId": 91, "originalName": "hero.png"})
	require.Equal(t, http.StatusCreated, response.StatusCode)
	var registered httpapi.MediaView
	decodeResponse(t, response, &registered)
	require.Equal(t, int64(1), registered.Id)
	require.Equal(t, flowMediaKey, registered.PublicKey)
	require.Equal(t, int64(91), registered.GfsFileId)
	require.Equal(t, "/img/proxy/"+flowMediaKey, registered.Url)
	require.Equal(t, "image/png", string(registered.MimeType))
	require.Equal(t, int64(1), flow.sequence("media"))
	require.Equal(t, int64(1), flow.gfsMetadataCalls.Load())

	// 4. Create a stable tag.
	flow.expectAdmin()
	expectExec(flow.mock, flowInsertTag).WithArgs(int64(1), "Go", flowTagSlug, flow.now, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	response = flow.adminJSON(http.MethodPost, "/api/admin/v1/tags", map[string]string{"name": "Go"})
	require.Equal(t, http.StatusCreated, response.StatusCode)
	var createdTag httpapi.TagView
	decodeResponse(t, response, &createdTag)
	require.Equal(t, int64(1), createdTag.Id)
	require.Equal(t, "Go", createdTag.Name)
	require.Equal(t, flowTagSlug, createdTag.Slug)
	require.Equal(t, int64(1), flow.sequence("tags"))

	// 5. Article and initial revision sequences are independent.
	emptyHash := revision.ComputeHash(revision.PreparedContent{})
	flow.expectAdmin()
	flow.mock.ExpectBegin()
	expectExec(flow.mock, flowInsertArticle).WithArgs(int64(1), flowArticleSlug, flow.now, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowInsertInitialDraft).WithArgs(int64(1), int64(1), emptyHash, flow.now, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowSetInitialDraft).WithArgs(int64(1), flow.now, int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.mock.ExpectCommit()
	response = flow.adminJSON(http.MethodPost, "/api/admin/v1/articles", nil)
	require.Equal(t, http.StatusCreated, response.StatusCode)
	var createdArticle httpapi.ArticleDetail
	decodeResponse(t, response, &createdArticle)
	require.Equal(t, int64(1), createdArticle.Id)
	require.Equal(t, flowArticleSlug, createdArticle.Slug)
	require.Equal(t, "active", string(createdArticle.State))
	require.Equal(t, int64(1), createdArticle.Draft.Id)
	require.Equal(t, int64(1), createdArticle.Draft.RevisionNo)
	require.Equal(t, int64(1), createdArticle.Draft.LockVersion)
	require.Empty(t, createdArticle.Draft.Title)
	require.Equal(t, emptyHash, createdArticle.Draft.ContentHash)
	require.Equal(t, int64(1), flow.sequence("articles"))
	require.Equal(t, int64(1), flow.sequence("article_revisions"))

	// 6. Autosave resolves live media/tag data and snapshots it atomically.
	hashOne := contentFlowHash(flowDraftTitle, flowDraftSummary, flowDraftBody, "Go")
	require.Equal(t, flowDraftHash, hashOne)
	flow.expectAdmin()
	flow.expectResolvedContent("Go")
	flow.mock.ExpectBegin()
	flow.expectArticlePointer(1)
	expectQuery(flow.mock, flowSelectCurrent).WithArgs(int64(1), int64(1)).WillReturnRows(flowRevisionRows().AddRow(
		int64(1), int64(1), int64(1), "editing", "draft", "", "", nil, "", emptyHash, int64(1), flow.now, flow.now,
	))
	expectExec(flow.mock, flowUpdateDraft).WithArgs(flowDraftTitle, flowDraftSummary, int64(1), flowDraftBody, hashOne, flow.now, int64(1), int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectQuery(flow.mock, flowSelectSavedIdentity).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(1), int64(2), int64(1), flow.now))
	expectExec(flow.mock, flowDeleteDraftTags).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 0))
	expectExec(flow.mock, flowInsertDraftTag).WithArgs(int64(1), int64(1), int64(1), "Go", flowTagSlug, 0, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowDeleteDraftMedia).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 0))
	expectExec(flow.mock, flowInsertDraftMedia).WithArgs(int64(1), int64(1), int64(1), "cover", 0, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowInsertDraftMedia).WithArgs(int64(2), int64(1), int64(1), "content", 1, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowTouchArticle).WithArgs(flow.now, int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.mock.ExpectCommit()
	response = flow.adminJSON(http.MethodPut, "/api/admin/v1/articles/1/draft", saveDraftBody(1, flowDraftTitle, flowDraftSummary, flowDraftBody))
	require.Equal(t, http.StatusOK, response.StatusCode)
	var savedOne httpapi.DraftView
	decodeResponse(t, response, &savedOne)
	flow.requireDraft(savedOne, 1, 1, 2, flowDraftTitle, flowDraftSummary, flowDraftBody, hashOne, "Go")
	require.Equal(t, int64(1), flow.sequence("article_revision_tags"))
	require.Equal(t, int64(2), flow.sequence("article_revision_media"))

	// 7. A stale lock rolls back before association mutation.
	flow.expectAdmin()
	flow.expectResolvedContent("Go")
	flow.mock.ExpectBegin()
	flow.expectArticlePointer(1)
	flow.expectRevisionScalar(flowSelectCurrent, []driverArg{int64(1), int64(1)}, 1, 1, 2, "editing", "draft", flowDraftTitle, flowDraftSummary, flowDraftBody, hashOne)
	flow.mock.ExpectRollback()
	response = flow.adminJSON(http.MethodPut, "/api/admin/v1/articles/1/draft", saveDraftBody(1, flowDraftTitle, flowDraftSummary, flowDraftBody))
	requireProblem(t, response, http.StatusConflict, "revision_conflict")
	require.Equal(t, int64(1), flow.sequence("article_revision_tags"))
	require.Equal(t, int64(2), flow.sequence("article_revision_media"))

	// 8. Manual version freezes revision 1 and creates editing revision 2.
	flow.expectAdmin()
	flow.expectDraftRead(1, 1, 2, "editing", "draft", flowDraftTitle, flowDraftSummary, flowDraftBody, hashOne, "Go")
	flow.expectActiveMediaResolution()
	flow.mock.ExpectBegin()
	flow.expectArticlePointer(1)
	flow.expectRevisionScalar(flowSelectCurrent, []driverArg{int64(1), int64(1)}, 1, 1, 2, "editing", "draft", flowDraftTitle, flowDraftSummary, flowDraftBody, hashOne)
	flow.expectStoredAssociations(1, "Go")
	expectExec(flow.mock, flowFreezeVersion).WithArgs(flow.now, int64(1), int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowInsertEditing).WithArgs(int64(2), int64(1), int64(2), flowDraftTitle, flowDraftSummary, int64(1), flowDraftBody, hashOne, flow.now, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.expectAssociationCopies(2, 2, 3, 4, "Go")
	expectExec(flow.mock, flowReplaceDraftPointer).WithArgs(int64(2), flow.now, int64(1), int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.mock.ExpectCommit()
	response = flow.adminJSON(http.MethodPost, "/api/admin/v1/articles/1/versions", map[string]int64{"lockVersion": 2})
	require.Equal(t, http.StatusCreated, response.StatusCode)
	var versionResult httpapi.VersionResult
	decodeResponse(t, response, &versionResult)
	require.Equal(t, int64(1), versionResult.Version.Id)
	require.Equal(t, int64(1), versionResult.Version.RevisionNo)
	require.Equal(t, "frozen", string(versionResult.Version.Status))
	require.Equal(t, "manual_version", string(versionResult.Version.Reason))
	flow.requireDraft(versionResult.Draft, 2, 2, 1, flowDraftTitle, flowDraftSummary, flowDraftBody, hashOne, "Go")

	// 9. Rename is transactional and historic snapshots remain unchanged.
	flow.expectAdmin()
	flow.mock.ExpectBegin()
	expectExec(flow.mock, flowRenameTag).WithArgs("Golang", flow.now, int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectQuery(flow.mock, flowFindTagID).WithArgs(int64(1)).WillReturnRows(flowTagRows("Golang", flow.now))
	flow.mock.ExpectCommit()
	response = flow.adminJSON(http.MethodPatch, "/api/admin/v1/tags/1", map[string]string{"name": "Golang"})
	require.Equal(t, http.StatusOK, response.StatusCode)
	var renamed httpapi.TagView
	decodeResponse(t, response, &renamed)
	require.Equal(t, "Golang", renamed.Name)
	require.Equal(t, flowTagSlug, renamed.Slug)

	flow.expectAdmin()
	flow.expectVersionList([]flowStoredRevision{{id: 1, revisionNo: 1, lock: 2, title: flowDraftTitle, summary: flowDraftSummary, body: flowDraftBody, hash: hashOne, tagName: "Go"}})
	response = flow.adminJSON(http.MethodGet, "/api/admin/v1/articles/1/versions", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var history httpapi.RevisionList
	decodeResponse(t, response, &history)
	require.Len(t, history.Items, 1)
	require.Equal(t, "Go", history.Items[0].Tags[0].Name)

	// 10. Save current names, then restore the immutable historic snapshot.
	hashTwo := contentFlowHash(flowChangedTitle, flowChangedSummary, flowChangedBody, "Golang")
	require.Equal(t, flowChangedHash, hashTwo)
	flow.expectAdmin()
	flow.expectResolvedContent("Golang")
	flow.mock.ExpectBegin()
	flow.expectArticlePointer(2)
	flow.expectRevisionScalar(flowSelectCurrent, []driverArg{int64(2), int64(1)}, 2, 2, 1, "editing", "draft", flowDraftTitle, flowDraftSummary, flowDraftBody, hashOne)
	expectExec(flow.mock, flowUpdateDraft).WithArgs(flowChangedTitle, flowChangedSummary, int64(1), flowChangedBody, hashTwo, flow.now, int64(1), int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectQuery(flow.mock, flowSelectSavedIdentity).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id", "lock_version", "revision_no", "created_at"}).AddRow(int64(2), int64(2), int64(2), flow.now))
	expectExec(flow.mock, flowDeleteDraftTags).WithArgs(int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowInsertDraftTag).WithArgs(int64(3), int64(2), int64(1), "Golang", flowTagSlug, 0, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowDeleteDraftMedia).WithArgs(int64(2)).WillReturnResult(sqlmock.NewResult(0, 2))
	expectExec(flow.mock, flowInsertDraftMedia).WithArgs(int64(5), int64(2), int64(1), "cover", 0, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowInsertDraftMedia).WithArgs(int64(6), int64(2), int64(1), "content", 1, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowTouchArticle).WithArgs(flow.now, int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.mock.ExpectCommit()
	response = flow.adminJSON(http.MethodPut, "/api/admin/v1/articles/1/draft", saveDraftBody(1, flowChangedTitle, flowChangedSummary, flowChangedBody))
	require.Equal(t, http.StatusOK, response.StatusCode)
	var savedTwo httpapi.DraftView
	decodeResponse(t, response, &savedTwo)
	flow.requireDraft(savedTwo, 2, 2, 2, flowChangedTitle, flowChangedSummary, flowChangedBody, hashTwo, "Golang")

	flow.expectAdmin()
	flow.expectDraftRead(2, 2, 2, "editing", "draft", flowChangedTitle, flowChangedSummary, flowChangedBody, hashTwo, "Golang")
	flow.mock.ExpectBegin()
	flow.expectArticlePointer(2)
	flow.expectRevisionScalar(flowSelectCurrent, []driverArg{int64(2), int64(1)}, 2, 2, 2, "editing", "draft", flowChangedTitle, flowChangedSummary, flowChangedBody, hashTwo)
	flow.expectRevisionScalar(flowSelectFrozen, []driverArg{int64(1), int64(1)}, 1, 1, 2, "frozen", "manual_version", flowDraftTitle, flowDraftSummary, flowDraftBody, hashOne)
	flow.expectStoredAssociations(1, "Go")
	expectExec(flow.mock, flowFreezeVersion).WithArgs(flow.now, int64(2), int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowInsertEditing).WithArgs(int64(3), int64(1), int64(3), flowDraftTitle, flowDraftSummary, int64(1), flowDraftBody, hashOne, flow.now, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.expectAssociationCopies(3, 4, 7, 8, "Go")
	expectExec(flow.mock, flowReplaceDraftPointer).WithArgs(int64(3), flow.now, int64(1), int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.mock.ExpectCommit()
	response = flow.adminJSON(http.MethodPost, "/api/admin/v1/articles/1/versions/1/restore", map[string]int64{"lockVersion": 2})
	require.Equal(t, http.StatusOK, response.StatusCode)
	var restored httpapi.DraftView
	decodeResponse(t, response, &restored)
	flow.requireDraft(restored, 3, 3, 1, flowDraftTitle, flowDraftSummary, flowDraftBody, hashOne, "Go")

	flow.expectAdmin()
	flow.expectVersionList([]flowStoredRevision{
		{id: 2, revisionNo: 2, lock: 2, title: flowChangedTitle, summary: flowChangedSummary, body: flowChangedBody, hash: hashTwo, tagName: "Golang"},
		{id: 1, revisionNo: 1, lock: 2, title: flowDraftTitle, summary: flowDraftSummary, body: flowDraftBody, hash: hashOne, tagName: "Go"},
	})
	response = flow.adminJSON(http.MethodGet, "/api/admin/v1/articles/1/versions", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	decodeResponse(t, response, &history)
	require.Len(t, history.Items, 2)
	require.Equal(t, "Golang", history.Items[0].Tags[0].Name)
	require.Equal(t, flowChangedBody, history.Items[0].ContentMd)
	require.Equal(t, "Go", history.Items[1].Tags[0].Name)
	require.Equal(t, flowDraftBody, history.Items[1].ContentMd)

	// 11. Preview contains the immutable slug plus the restored complete draft.
	flow.expectAdmin()
	expectQuery(flow.mock, flowSelectArticle).WithArgs(int64(1)).WillReturnRows(flowArticleRows(1, "active", nil, flow.now))
	flow.expectDraftReadAt(3, 3, 1, "editing", "draft", flowDraftTitle, flowDraftSummary, flowDraftBody, hashOne, "Go")
	response = flow.adminJSON(http.MethodGet, "/api/admin/v1/articles/1/preview", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var preview struct {
		Slug  string            `json:"slug"`
		Draft httpapi.DraftView `json:"draft"`
	}
	decodeResponse(t, response, &preview)
	require.Equal(t, flowArticleSlug, preview.Slug)
	flow.requireDraft(preview.Draft, 3, 3, 1, flowDraftTitle, flowDraftSummary, flowDraftBody, hashOne, "Go")

	// 12. Unpublished lifecycle succeeds; published fixture rejects before update.
	flow.expectAdmin()
	expectQuery(flow.mock, flowSelectArticle).WithArgs(int64(1)).WillReturnRows(flowArticleRows(1, "active", nil, flow.now))
	expectExec(flow.mock, flowTrashArticle).WithArgs(flow.now, int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	response = flow.adminJSON(http.MethodPost, "/api/admin/v1/articles/1/trash", nil)
	require.Equal(t, http.StatusNoContent, response.StatusCode)
	require.NoError(t, response.Body.Close())

	flow.expectAdmin()
	expectQuery(flow.mock, flowSelectArticle).WithArgs(int64(1)).WillReturnRows(flowArticleRows(1, "trashed", nil, flow.now))
	expectExec(flow.mock, flowUntrashArticle).WithArgs(flow.now, int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	response = flow.adminJSON(http.MethodPost, "/api/admin/v1/articles/1/untrash", nil)
	require.Equal(t, http.StatusNoContent, response.StatusCode)
	require.NoError(t, response.Body.Close())

	flow.expectAdmin()
	expectQuery(flow.mock, flowSelectArticle).WithArgs(int64(1)).WillReturnRows(flowArticleRows(1, "active", int64(1), flow.now))
	response = flow.adminJSON(http.MethodPost, "/api/admin/v1/articles/1/trash", nil)
	requireProblem(t, response, http.StatusConflict, "article_must_be_unpublished")

	// 13. Virtual defaults allow empty Referer and signing stays process-local.
	flow.mock.ExpectBegin()
	expectQuery(flow.mock, flowSelectHotlink).WillReturnError(sql.ErrNoRows)
	flow.mock.ExpectRollback()
	expectQuery(flow.mock, flowFindActiveMediaByKey).WithArgs(flowMediaKey).WillReturnRows(flowMediaRows(flow.now))
	response = flow.publicMedia("")
	flow.requireMediaRedirect(response)
	require.Equal(t, int64(1), flow.gfsMetadataCalls.Load())

	// 14. Replacing settings invalidates the already-warm cache immediately.
	flow.expectAdmin()
	flow.mock.ExpectBegin()
	expectExec(flow.mock, flowUpdateHotlink).WithArgs(false, flow.now).WillReturnResult(sqlmock.NewResult(0, 0))
	expectQuery(flow.mock, flowLockHotlink).WillReturnError(sql.ErrNoRows)
	expectExec(flow.mock, flowInsertHotlink).WithArgs(int64(1), false, flow.now, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowDeleteHotlinkEntries).WillReturnResult(sqlmock.NewResult(0, 0))
	expectExec(flow.mock, flowInsertHotlinkEntry).WithArgs(int64(1), "blog-admin.qiuxs.com", true, flow.now, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	flow.mock.ExpectCommit()
	response = flow.adminJSON(http.MethodPut, "/api/admin/v1/settings/hotlink", map[string]any{
		"allowEmptyReferer": false,
		"entries":           []map[string]any{{"hostname": "blog-admin.qiuxs.com", "enabled": true}},
	})
	require.Equal(t, http.StatusOK, response.StatusCode)
	var hotlink httpapi.HotlinkSettingsView
	decodeResponse(t, response, &hotlink)
	require.False(t, hotlink.AllowEmptyReferer)
	require.Equal(t, []httpapi.HotlinkEntry{{Hostname: "blog-admin.qiuxs.com", Enabled: true}}, hotlink.Entries)

	flow.mock.ExpectBegin()
	expectQuery(flow.mock, flowSelectHotlink).WillReturnRows(sqlmock.NewRows([]string{"id", "allow_empty_referer"}).AddRow(int64(1), false))
	expectQuery(flow.mock, flowSelectHotlinkEntries).WillReturnRows(sqlmock.NewRows([]string{"id", "hostname", "enabled"}).AddRow(int64(1), "blog-admin.qiuxs.com", true))
	flow.mock.ExpectCommit()
	response = flow.publicMedia("")
	requireProblem(t, response, http.StatusForbidden, "hotlink_forbidden")
	require.Equal(t, "no-store", response.Header.Get("Cache-Control"))

	expectQuery(flow.mock, flowFindActiveMediaByKey).WithArgs(flowMediaKey).WillReturnRows(flowMediaRows(flow.now))
	response = flow.publicMedia("https://blog-admin.qiuxs.com/preview")
	flow.requireMediaRedirect(response)
	response = flow.publicMedia("https://sub.blog-admin.qiuxs.com/preview")
	requireProblem(t, response, http.StatusForbidden, "hotlink_forbidden")
	require.Equal(t, "no-store", response.Header.Get("Cache-Control"))

	// 15. Site settings expose the fixed filing link and enforce optimistic writes.
	flow.expectAdmin()
	expectQuery(flow.mock, flowSelectSite).WillReturnError(sql.ErrNoRows)
	response = flow.adminJSON(http.MethodGet, "/api/admin/v1/settings/site", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var virtualSite httpapi.SiteSettingsView
	decodeResponse(t, response, &virtualSite)
	require.Nil(t, virtualSite.Id)
	require.Equal(t, int64(0), virtualSite.LockVersion)
	require.Equal(t, "长安休息室", virtualSite.FilingName)
	require.Equal(t, "浙ICP备17057726号-1", virtualSite.FilingNumber)
	require.NotNil(t, virtualSite.FilingUrl)
	require.Equal(t, settings.FilingURL, *virtualSite.FilingUrl)

	createSite := siteWrite(0, "Stage 2")
	flow.expectAdmin()
	expectExec(flow.mock, flowInsertSite).WithArgs(
		int64(1), "qiuxs", "qiuxs", "Stage 2", "shipping", "# About\n", `[{"label":"GitHub","url":"https://github.com/qiuxsgit"}]`,
		"qiuxs blog", "Service content", nil, "长安休息室", "浙ICP备17057726号-1", flow.now, flow.now,
	).WillReturnResult(sqlmock.NewResult(0, 1))
	response = flow.adminJSON(http.MethodPut, "/api/admin/v1/settings/site", createSite)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var storedSite httpapi.SiteSettingsView
	decodeResponse(t, response, &storedSite)
	require.NotNil(t, storedSite.Id)
	require.Equal(t, int64(1), *storedSite.Id)
	require.Equal(t, int64(1), storedSite.LockVersion)
	require.Equal(t, settings.FilingURL, *storedSite.FilingUrl)

	updateSite := siteWrite(1, "Stage 2 updated")
	flow.expectAdmin()
	flow.mock.ExpectBegin()
	expectExec(flow.mock, flowUpdateSite).WithArgs(
		"qiuxs", "qiuxs", "Stage 2 updated", "shipping", "# About\n", `[{"label":"GitHub","url":"https://github.com/qiuxsgit"}]`,
		"qiuxs blog", "Service content", nil, "长安休息室", "浙ICP备17057726号-1", flow.now, int64(1),
	).WillReturnResult(sqlmock.NewResult(0, 1))
	expectQuery(flow.mock, flowSelectSite).WillReturnRows(flowSiteRows(2, "Stage 2 updated", flow.now))
	flow.mock.ExpectCommit()
	response = flow.adminJSON(http.MethodPut, "/api/admin/v1/settings/site", updateSite)
	require.Equal(t, http.StatusOK, response.StatusCode)
	decodeResponse(t, response, &storedSite)
	require.Equal(t, int64(2), storedSite.LockVersion)
	require.Equal(t, "Stage 2 updated", storedSite.AuthorBio)

	flow.expectAdmin()
	response = flow.adminJSON(http.MethodPut, "/api/admin/v1/settings/site", map[string]any{
		"lockVersion": 2, "siteName": "qiuxs", "authorName": "qiuxs", "authorBio": "bad", "homeStatus": "shipping",
		"aboutMd": "# About\n", "socialLinks": []any{}, "seoDefaultTitle": "qiuxs blog", "seoDefaultDescription": "Service content",
		"seoDefaultImageMediaId": nil, "filingName": "长安休息室", "filingNumber": "浙ICP备17057726号-1", "filingUrl": "https://evil.example/",
	})
	requireProblem(t, response, http.StatusBadRequest, "invalid_request")

	flow.expectAdmin()
	flow.mock.ExpectBegin()
	expectExec(flow.mock, flowUpdateSite).WithArgs(
		"qiuxs", "qiuxs", "stale", "shipping", "# About\n", `[{"label":"GitHub","url":"https://github.com/qiuxsgit"}]`,
		"qiuxs blog", "Service content", nil, "长安休息室", "浙ICP备17057726号-1", flow.now, int64(1),
	).WillReturnResult(sqlmock.NewResult(0, 0))
	flow.mock.ExpectRollback()
	response = flow.adminJSON(http.MethodPut, "/api/admin/v1/settings/site", siteWrite(1, "stale"))
	requireProblem(t, response, http.StatusConflict, "settings_conflict")

	require.Equal(t, int64(3), flow.sequence("article_revisions"))
	require.Equal(t, int64(4), flow.sequence("article_revision_tags"))
	require.Equal(t, int64(8), flow.sequence("article_revision_media"))
	require.Equal(t, int64(1), flow.sequence("hotlink_settings"))
	require.Equal(t, int64(1), flow.sequence("referer_allowlist"))
	require.Equal(t, int64(1), flow.sequence("site_settings"))
	require.Equal(t, int64(1), flow.gfsMetadataCalls.Load(), "read URL signing must not call GFS")
	require.Equal(t, int64(1), flow.gfsTotalCalls.Load(), "no startup probe or signing HTTP call may reach GFS")
	require.NotContains(t, flow.logs.String(), flow.cfg.GFS.AppSecret)
	require.NotContains(t, flow.logs.String(), flow.cfg.GFS.PublicReadSecret)
	require.NoError(t, flow.mock.ExpectationsWereMet())
}

type contentMediaFlow struct {
	t                *testing.T
	mock             sqlmock.Sqlmock
	miniRedis        *miniredis.Miniredis
	client           *http.Client
	baseURL          string
	cookie           *http.Cookie
	now              time.Time
	cfg              config.Config
	gfs              *httptest.Server
	gfsMetadataCalls atomic.Int64
	gfsTotalCalls    atomic.Int64
	logs             bytes.Buffer
	passwordHash     string
}

func newContentMediaFlow(t *testing.T) *contentMediaFlow {
	t.Helper()
	flow := &contentMediaFlow{t: t, now: time.Date(2026, 8, 14, 8, 9, 10, 0, time.UTC)}
	flow.gfs = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		flow.gfsTotalCalls.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/alioss/objects/91/metadata" {
			http.NotFound(writer, request)
			return
		}
		flow.gfsMetadataCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":0,"data":{"fileId":91,"fileName":"hero.png","fileSize":1024,"contentType":"image/png","imageMetadata":{"imageWidth":"640","imageHeight":"480"}}}`)
	}))
	t.Cleanup(flow.gfs.Close)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	flow.mock = mock
	t.Cleanup(func() { _ = db.Close() })
	flow.miniRedis = miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: flow.miniRedis.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })

	flow.cfg = flowConfig(flow.miniRedis.Addr())
	flow.cfg.Environment = "development"
	flow.cfg.GFS.BaseURL = flow.gfs.URL
	flow.cfg.Session.CookieSecure = true
	flow.passwordHash, err = auth.DefaultPasswordHasher().Hash(adminPassword)
	require.NoError(t, err)
	router, err := app.Build(flow.cfg, app.Dependencies{
		DB: db, Redis: redisClient, Logger: slog.New(slog.NewJSONHandler(&flow.logs, nil)),
		Random: bytes.NewReader(bytes.Repeat([]byte{0}, 4096)), Now: func() time.Time { return flow.now },
		HTTPClient:        &http.Client{Timeout: 5 * time.Second},
		JenkinsHTTPClient: &http.Client{Timeout: 5 * time.Second},
		ReleaseJSONReader: func() (io.ReadCloser, error) { return nil, fs.ErrNotExist },
	})
	require.NoError(t, err)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	flow.client = withoutRedirects(server.Client())
	flow.baseURL = server.URL
	return flow
}

func (flow *contentMediaFlow) expectLogin() {
	expectQuery(flow.mock, selectAdminPrefix+" WHERE username = ?").WithArgs(adminUsername).WillReturnRows(adminRows(flow.passwordHash))
	expectExec(flow.mock, "UPDATE admins SET last_login_at = ? WHERE id = ?").WithArgs(flow.now, int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
}

func (flow *contentMediaFlow) expectAdmin() {
	expectQuery(flow.mock, selectAdminPrefix+" WHERE id = ?").WithArgs(int64(1)).WillReturnRows(adminRows(flow.passwordHash))
}

func (flow *contentMediaFlow) adminJSON(method, path string, body any) *http.Response {
	return doJSON(flow.t, flow.client, method, flow.baseURL+path, unsafeOrigin(method), flow.cookie, body)
}

func (flow *contentMediaFlow) publicMedia(referer string) *http.Response {
	request, err := http.NewRequest(http.MethodGet, flow.baseURL+"/img/proxy/"+flowMediaKey, nil)
	require.NoError(flow.t, err)
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
	response, err := flow.client.Do(request)
	require.NoError(flow.t, err)
	return response
}

func (flow *contentMediaFlow) requireMediaRedirect(response *http.Response) {
	flow.t.Helper()
	require.Equal(flow.t, http.StatusFound, response.StatusCode)
	require.Equal(flow.t, "no-store", response.Header.Get("Cache-Control"))
	target := response.Header.Get("Location")
	parsed, err := url.Parse(target)
	require.NoError(flow.t, err)
	gfsBase, err := url.Parse(flow.gfs.URL)
	require.NoError(flow.t, err)
	require.Equal(flow.t, gfsBase.Scheme, parsed.Scheme)
	require.Equal(flow.t, gfsBase.Host, parsed.Host)
	require.Nil(flow.t, parsed.User)
	require.Empty(flow.t, parsed.Fragment)
	require.Equal(flow.t, "/read/"+url.PathEscape(flowReadPolicy), parsed.EscapedPath())
	encodedPolicy := strings.TrimPrefix(parsed.EscapedPath(), "/read/")
	encodedPolicy, err = url.PathUnescape(encodedPolicy)
	require.NoError(flow.t, err)
	decodedPolicy, err := base64.StdEncoding.DecodeString(encodedPolicy)
	require.NoError(flow.t, err)
	require.JSONEq(flow.t, `{"userId":"","fileId":91,"imageWidth":0,"imageHeight":0,"internalFlag":0}`, string(decodedPolicy))
	expectedQuery := url.Values{
		"expire":    {"60"},
		"signature": {flowReadSignature},
		"timestamp": {flowReadTimestamp},
	}
	require.Equal(flow.t, expectedQuery.Encode(), parsed.RawQuery)
	body := readResponseBody(flow.t, response)
	require.Empty(flow.t, body)
}

func (flow *contentMediaFlow) expectResolvedContent(tagName string) {
	expectQuery(flow.mock, flowFindTagIDs).WithArgs(int64(1)).WillReturnRows(flowTagRows(tagName, flow.now))
	flow.expectActiveMediaResolution()
}

func (flow *contentMediaFlow) expectActiveMediaResolution() {
	expectQuery(flow.mock, flowFindActiveMediaByID).WithArgs(int64(1)).WillReturnRows(flowMediaRows(flow.now))
	expectQuery(flow.mock, flowFindActiveMediaKeys).WithArgs(flowMediaKey).WillReturnRows(flowMediaRows(flow.now))
}

func (flow *contentMediaFlow) expectDraftRead(id, revisionNo, lock int64, status, reason, title, summary, body, hash, tagName string) {
	flow.mock.ExpectBegin()
	flow.expectRevisionScalar(flowSelectEditingDraft, []driverArg{int64(1)}, id, revisionNo, lock, status, reason, title, summary, body, hash)
	flow.expectStoredAssociations(id, tagName)
	flow.mock.ExpectCommit()
}

func (flow *contentMediaFlow) expectDraftReadAt(id, revisionNo, lock int64, status, reason, title, summary, body, hash, tagName string) {
	flow.mock.ExpectBegin()
	flow.expectRevisionScalar(flowSelectEditingAt, []driverArg{id, int64(1)}, id, revisionNo, lock, status, reason, title, summary, body, hash)
	flow.expectStoredAssociations(id, tagName)
	flow.mock.ExpectCommit()
}

type driverArg any

func (flow *contentMediaFlow) expectRevisionScalar(statement string, args []driverArg, id, revisionNo, lock int64, status, reason, title, summary, body, hash string) {
	expectation := expectQuery(flow.mock, statement)
	values := make([]driver.Value, len(args))
	for index := range args {
		values[index] = args[index]
	}
	expectation.WithArgs(values...).WillReturnRows(flowRevisionRows().AddRow(
		id, int64(1), revisionNo, status, reason, title, summary, int64(1), body, hash, lock, flow.now, flow.now,
	))
}

func (flow *contentMediaFlow) expectArticlePointer(revisionID int64) {
	expectQuery(flow.mock, flowSelectArticlePointer).WithArgs(int64(1)).WillReturnRows(
		sqlmock.NewRows([]string{"state", "draft_revision_id"}).AddRow("active", revisionID),
	)
}

func (flow *contentMediaFlow) expectStoredAssociations(revisionID int64, tagName string) {
	expectQuery(flow.mock, flowSelectDraftTags).WithArgs(revisionID).WillReturnRows(sqlmock.NewRows([]string{"tag_id", "tag_name", "tag_slug", "position"}).AddRow(int64(1), tagName, flowTagSlug, 0))
	expectQuery(flow.mock, flowSelectDraftMedia).WithArgs(revisionID).WillReturnRows(sqlmock.NewRows([]string{"media_id", "public_key", "purpose", "position"}).
		AddRow(int64(1), flowMediaKey, "cover", 0).
		AddRow(int64(1), flowMediaKey, "content", 1))
}

func (flow *contentMediaFlow) expectAssociationCopies(revisionID, tagAssociationID, coverAssociationID, contentAssociationID int64, tagName string) {
	expectExec(flow.mock, flowInsertDraftTag).WithArgs(tagAssociationID, revisionID, int64(1), tagName, flowTagSlug, 0, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowInsertDraftMedia).WithArgs(coverAssociationID, revisionID, int64(1), "cover", 0, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExec(flow.mock, flowInsertDraftMedia).WithArgs(contentAssociationID, revisionID, int64(1), "content", 1, flow.now).WillReturnResult(sqlmock.NewResult(0, 1))
}

type flowStoredRevision struct {
	id, revisionNo, lock                int64
	title, summary, body, hash, tagName string
}

func (flow *contentMediaFlow) expectVersionList(items []flowStoredRevision) {
	flow.mock.ExpectBegin()
	rows := flowRevisionRows()
	for _, item := range items {
		rows.AddRow(item.id, int64(1), item.revisionNo, "frozen", "manual_version", item.title, item.summary, int64(1), item.body, item.hash, item.lock, flow.now, flow.now)
	}
	expectQuery(flow.mock, flowListFrozen).WithArgs(int64(1)).WillReturnRows(rows)
	for _, item := range items {
		flow.expectStoredAssociations(item.id, item.tagName)
	}
	flow.mock.ExpectCommit()
}

func (flow *contentMediaFlow) requireDraft(got httpapi.DraftView, id, revisionNo, lock int64, title, summary, body, hash, tagName string) {
	flow.t.Helper()
	require.Equal(flow.t, id, got.Id)
	require.Equal(flow.t, int64(1), got.ArticleId)
	require.Equal(flow.t, revisionNo, got.RevisionNo)
	require.Equal(flow.t, lock, got.LockVersion)
	require.Equal(flow.t, "editing", string(got.Status))
	require.Equal(flow.t, "draft", string(got.Reason))
	require.Equal(flow.t, title, got.Title)
	require.Equal(flow.t, summary, got.Summary)
	require.Equal(flow.t, body, got.ContentMd)
	require.Equal(flow.t, hash, got.ContentHash)
	require.NotNil(flow.t, got.CoverMediaId)
	require.Equal(flow.t, int64(1), *got.CoverMediaId)
	require.Equal(flow.t, []httpapi.TagSnapshot{{TagId: 1, Name: tagName, Slug: flowTagSlug, Position: 0}}, got.Tags)
	require.Equal(flow.t, []httpapi.MediaReference{
		{MediaId: 1, PublicKey: flowMediaKey, Purpose: "cover", Position: 0},
		{MediaId: 1, PublicKey: flowMediaKey, Purpose: "content", Position: 1},
	}, got.Media)
}

func (flow *contentMediaFlow) sequence(table string) int64 {
	flow.t.Helper()
	value, err := flow.miniRedis.Get("idseq:" + table)
	require.NoError(flow.t, err)
	var decoded int64
	require.NoError(flow.t, json.Unmarshal([]byte(value), &decoded))
	return decoded
}

func contentFlowHash(title, summary, body, tagName string) string {
	cover := &media.Media{ID: 1, PublicKey: flowMediaKey}
	prepared := revision.PreparedContent{
		Title: title, Summary: summary, Cover: cover, ContentMD: body,
		Tags: []tag.Snapshot{{TagID: 1, Name: tagName, Slug: flowTagSlug, Position: 0}},
		Media: []media.Reference{
			{MediaID: 1, PublicKey: flowMediaKey, Purpose: "cover", Position: 0},
			{MediaID: 1, PublicKey: flowMediaKey, Purpose: "content", Position: 1},
		},
	}
	return revision.ComputeHash(prepared)
}

func saveDraftBody(lock int64, title, summary, body string) map[string]any {
	return map[string]any{"lockVersion": lock, "title": title, "summary": summary, "coverMediaId": 1, "contentMd": body, "tagIds": []int64{1}}
}

func siteWrite(lock int64, authorBio string) map[string]any {
	return map[string]any{
		"lockVersion": lock, "siteName": "qiuxs", "authorName": "qiuxs", "authorBio": authorBio, "homeStatus": "shipping",
		"aboutMd": "# About\n", "socialLinks": []map[string]string{{"label": "GitHub", "url": "https://github.com/qiuxsgit"}},
		"seoDefaultTitle": "qiuxs blog", "seoDefaultDescription": "Service content", "seoDefaultImageMediaId": nil,
		"filingName": "长安休息室", "filingNumber": "浙ICP备17057726号-1",
	}
}

func flowRevisionRows() *sqlmock.Rows {
	return sqlmock.NewRows(strings.Split(flowRevisionCols, ", "))
}

func flowMediaRows(at time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(strings.Split(flowMediaColumns, ", ")).AddRow(int64(1), flowMediaKey, int64(91), "hero.png", "image/png", int64(1024), 640, 480, "active", at, at)
}

func flowTagRows(name string, at time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "slug", "created_at", "updated_at"}).AddRow(int64(1), name, flowTagSlug, at, at)
}

func flowArticleRows(id int64, state string, published any, at time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(strings.Split(flowArticleCols, ", ")).AddRow(id, flowArticleSlug, int64(3), published, state, at, at)
}

func flowSiteRows(lock int64, authorBio string, at time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "site_name", "author_name", "author_bio", "home_status", "about_md", "social_links_json", "seo_default_title",
		"seo_default_description", "seo_default_image_media_id", "filing_name", "filing_number", "lock_version", "updated_at",
	}).AddRow(int64(1), "qiuxs", "qiuxs", authorBio, "shipping", "# About\n", `[{"label":"GitHub","url":"https://github.com/qiuxsgit"}]`, "qiuxs blog", "Service content", nil, "长安休息室", "浙ICP备17057726号-1", lock, at)
}

func expectQuery(mock sqlmock.Sqlmock, statement string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery("^" + regexp.QuoteMeta(statement) + "$")
}

func expectExec(mock sqlmock.Sqlmock, statement string) *sqlmock.ExpectedExec {
	return mock.ExpectExec("^" + regexp.QuoteMeta(statement) + "$")
}

func unsafeOrigin(method string) string {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return ""
	}
	return adminOrigin
}

func readResponseBody(t *testing.T, response *http.Response) []byte {
	t.Helper()
	defer func() { require.NoError(t, response.Body.Close()) }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return body
}
