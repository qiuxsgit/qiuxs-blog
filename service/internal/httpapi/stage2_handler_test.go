package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/article"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/stretchr/testify/require"
)

var stage2TestTime = time.Date(2026, time.August, 14, 8, 9, 10, 0, time.UTC)

type stage2ArticleFake struct {
	calls        int
	listState    article.State
	createResult article.Detail
	getResult    article.Detail
	listResult   []article.Summary
	err          error
}

func (f *stage2ArticleFake) Create(context.Context) (article.Detail, error) {
	f.calls++
	return f.createResult, f.err
}
func (f *stage2ArticleFake) Get(context.Context, int64) (article.Detail, error) {
	f.calls++
	return f.getResult, f.err
}
func (f *stage2ArticleFake) List(_ context.Context, state article.State) ([]article.Summary, error) {
	f.calls++
	f.listState = state
	return f.listResult, f.err
}
func (f *stage2ArticleFake) Trash(context.Context, int64) error   { f.calls++; return f.err }
func (f *stage2ArticleFake) Untrash(context.Context, int64) error { f.calls++; return f.err }

type stage2RevisionFake struct {
	calls          int
	saveContent    revision.Content
	saveLock       int64
	draft          revision.Draft
	version        revision.Version
	versions       []revision.Version
	err            error
	freezableCalls int
}

func (f *stage2RevisionFake) GetDraft(context.Context, int64) (revision.Draft, error) {
	f.calls++
	return f.draft, f.err
}
func (f *stage2RevisionFake) GetDraftAt(context.Context, int64, int64) (revision.Draft, error) {
	f.calls++
	return f.draft, f.err
}
func (f *stage2RevisionFake) SaveDraft(_ context.Context, _ int64, lock int64, content revision.Content) (revision.Draft, error) {
	f.calls++
	f.saveLock = lock
	f.saveContent = content
	return f.draft, f.err
}
func (f *stage2RevisionFake) Preview(context.Context, int64) (revision.Draft, error) {
	f.calls++
	return f.draft, f.err
}
func (f *stage2RevisionFake) CreateVersion(context.Context, int64, int64) (revision.Version, revision.Draft, error) {
	f.calls++
	return f.version, f.draft, f.err
}
func (f *stage2RevisionFake) ListVersions(context.Context, int64) ([]revision.Version, error) {
	f.calls++
	return f.versions, f.err
}
func (f *stage2RevisionFake) RestoreVersion(context.Context, int64, int64, int64) (revision.Draft, error) {
	f.calls++
	return f.draft, f.err
}
func (f *stage2RevisionFake) ValidateFreezable(revision.Draft) error {
	f.freezableCalls++
	return f.err
}

type stage2TagFake struct {
	calls  int
	result tag.Tag
	list   []tag.Tag
	err    error
}

func (f *stage2TagFake) Create(context.Context, string) (tag.Tag, error) {
	f.calls++
	return f.result, f.err
}
func (f *stage2TagFake) List(context.Context) ([]tag.Tag, error) {
	f.calls++
	return f.list, f.err
}
func (f *stage2TagFake) Rename(context.Context, int64, string) (tag.Tag, error) {
	f.calls++
	return f.result, f.err
}
func (f *stage2TagFake) Snapshots(context.Context, []int64) ([]tag.Snapshot, error) {
	f.calls++
	return nil, f.err
}

type stage2MediaFake struct {
	calls  int
	policy media.UploadPolicy
	result media.Media
	err    error
}

func (f *stage2MediaFake) IssueUploadPolicy(context.Context) (media.UploadPolicy, error) {
	f.calls++
	return f.policy, f.err
}
func (f *stage2MediaFake) Register(context.Context, int64, string) (media.Media, error) {
	f.calls++
	return f.result, f.err
}
func (f *stage2MediaFake) ResolveReferences(context.Context, *int64, []string) (*media.Media, []media.Reference, error) {
	f.calls++
	return nil, nil, f.err
}
func (f *stage2MediaFake) RequireActive(context.Context, int64) error { f.calls++; return f.err }
func (f *stage2MediaFake) FindActiveByPublicKey(context.Context, string) (media.Media, error) {
	f.calls++
	return f.result, f.err
}

type stage2SiteFake struct {
	calls  int
	result settings.Site
	err    error
}

func (f *stage2SiteFake) GetSite(context.Context) (settings.Site, error) {
	f.calls++
	return f.result, f.err
}
func (f *stage2SiteFake) PutSite(context.Context, settings.Site, int64) (settings.Site, error) {
	f.calls++
	return f.result, f.err
}

type stage2HotlinkFake struct {
	calls  int
	result settings.HotlinkPolicy
	err    error
}

func (f *stage2HotlinkFake) Get(context.Context) (settings.HotlinkPolicy, error) {
	f.calls++
	return f.result, f.err
}
func (f *stage2HotlinkFake) Put(context.Context, bool, []settings.HotlinkEntry) (settings.HotlinkPolicy, error) {
	f.calls++
	return f.result, f.err
}
func (f *stage2HotlinkFake) Current(context.Context) (settings.HotlinkPolicy, error) {
	f.calls++
	return f.result, f.err
}
func (f *stage2HotlinkFake) AllowsReferer(settings.HotlinkPolicy, string) bool { return true }
func (f *stage2HotlinkFake) AllowsCurrentReferer(context.Context, string) (bool, error) {
	f.calls++
	return true, f.err
}

type stage2System struct {
	router    *gin.Engine
	articles  *stage2ArticleFake
	revisions *stage2RevisionFake
	tags      *stage2TagFake
	media     *stage2MediaFake
	site      *stage2SiteFake
	hotlink   *stage2HotlinkFake
	log       *stage2LogCapture
}

type stage2LogCapture struct {
	adminID   int64
	articleID int64
}

func newStage2System(t *testing.T, authenticated bool) stage2System {
	t.Helper()
	draft := stage2Draft()
	detail := article.Detail{Article: stage2Article(), Draft: draft}
	articles := &stage2ArticleFake{createResult: detail, getResult: detail, listResult: []article.Summary{{Article: stage2Article(), DraftTitle: "Draft title", DraftUpdatedAt: stage2TestTime}}}
	revisions := &stage2RevisionFake{draft: draft, version: revision.Version{Draft: stage2VersionDraft()}, versions: []revision.Version{{Draft: stage2VersionDraft()}}}
	tags := &stage2TagFake{result: stage2Tag(), list: []tag.Tag{stage2Tag()}}
	mediaService := &stage2MediaFake{
		policy: media.UploadPolicy{UploadURL: "https://gfs.example/v1/upload", AppID: "app", Policy: "policy", Signature: "signature", Timestamp: "1", Expire: "60", Nonce: "nonce", FileField: "file"},
		result: media.Media{ID: 31, PublicKey: "m_aaaaaaaaaaaaaaaaaaaaaa", GFSFileID: 41, OriginalName: "photo.png", MIMEType: "image/png", FileSize: 8192, Width: 640, Height: 480, State: "active", CreatedAt: stage2TestTime, UpdatedAt: stage2TestTime},
	}
	site := &stage2SiteFake{result: settings.Site{ID: 51, LockVersion: 2, SiteName: "qiuxs", AuthorName: "qiuxs", SocialLinks: []settings.SocialLink{{Label: "GitHub", URL: "https://github.com/qiuxs"}}, FilingName: "长安休息室", FilingNumber: "浙ICP备17057726号-1", UpdatedAt: stage2TestTime}}
	hotlink := &stage2HotlinkFake{result: settings.HotlinkPolicy{AllowEmptyReferer: true, Entries: []settings.HotlinkEntry{{ID: 61, Hostname: "qiuxs.com", Enabled: true}}}}
	authHandler := NewAuthHandler(auth.Service{}, config.SessionConfig{CookieName: "qx_blog_session", CookieSecure: true, TTL: time.Hour})
	handler, err := NewAdminHandler(authHandler, articles, revisions, tags, mediaService, site, hotlink)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	logCapture := &stage2LogCapture{}
	router.Use(RequestID(), func(c *gin.Context) {
		if authenticated {
			c.Set(sessionStateKey, sessionState{admin: auth.Admin{ID: 7, Username: "admin", State: "active"}})
		}
		c.Next()
		logCapture.adminID, _ = AdminIDFromLogContext(c)
		logCapture.articleID, _ = ArticleIDFromLogContext(c)
	})
	RegisterAdminHandlers(router, handler)
	return stage2System{router: router, articles: articles, revisions: revisions, tags: tags, media: mediaService, site: site, hotlink: hotlink, log: logCapture}
}

func TestAdminHandlerServesEveryStage2Operation(t *testing.T) {
	system := newStage2System(t, true)
	requests := []struct {
		method, path, contentType, body string
		status                          int
	}{
		{"GET", "/api/admin/v1/articles?state=trashed", "", "", 200},
		{"POST", "/api/admin/v1/articles", "", "", 201},
		{"GET", "/api/admin/v1/articles/11", "", "", 200},
		{"PUT", "/api/admin/v1/articles/11/draft", "application/json; charset=UTF-8", `{"lockVersion":1,"title":"Title","summary":"Summary","coverMediaId":31,"contentMd":"body","tagIds":[22,21]}`, 200},
		{"GET", "/api/admin/v1/articles/11/preview", "", "", 200},
		{"GET", "/api/admin/v1/articles/11/versions", "", "", 200},
		{"POST", "/api/admin/v1/articles/11/versions", "application/json", `{"lockVersion":1}`, 201},
		{"POST", "/api/admin/v1/articles/11/versions/13/restore", "application/json", `{"lockVersion":1}`, 200},
		{"POST", "/api/admin/v1/articles/11/trash", "", "", 204},
		{"POST", "/api/admin/v1/articles/11/untrash", "", "", 204},
		{"GET", "/api/admin/v1/tags", "", "", 200},
		{"POST", "/api/admin/v1/tags", "application/json", `{"name":"Go"}`, 201},
		{"PATCH", "/api/admin/v1/tags/21", "application/json", `{"name":"Golang"}`, 200},
		{"POST", "/api/admin/v1/media/upload-policy", "", "", 200},
		{"POST", "/api/admin/v1/media", "application/json", `{"gfsFileId":41,"originalName":"photo.png"}`, 201},
		{"GET", "/api/admin/v1/settings/site", "", "", 200},
		{"PUT", "/api/admin/v1/settings/site", "application/json", siteRequestJSON(), 200},
		{"GET", "/api/admin/v1/settings/hotlink", "", "", 200},
		{"PUT", "/api/admin/v1/settings/hotlink", "application/json", `{"allowEmptyReferer":false,"entries":[{"hostname":"example.com","enabled":true}]}`, 200},
	}
	for _, request := range requests {
		t.Run(request.method+" "+request.path, func(t *testing.T) {
			response := performHandlerRequest(system.router, request.method, request.path, request.contentType, request.body, nil)
			require.Equal(t, request.status, response.Code, response.Body.String())
			if request.status != http.StatusNoContent {
				require.Contains(t, response.Header().Get("Content-Type"), "application/json")
			}
		})
	}
	hotlinkResponse := performHandlerRequest(system.router, http.MethodGet, "/api/admin/v1/settings/hotlink", "", "", nil)
	require.JSONEq(t, `{"allowEmptyReferer":true,"entries":[{"hostname":"qiuxs.com","enabled":true}]}`, hotlinkResponse.Body.String())
	require.Equal(t, article.StateTrashed, system.articles.listState)
	require.Equal(t, int64(1), system.revisions.saveLock)
	require.Equal(t, []int64{22, 21}, system.revisions.saveContent.TagIDs)
}

func TestRegisterAdminHandlersDoesNotExposeReleaseContractBeforeTask7(t *testing.T) {
	system := newStage2System(t, true)
	requests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/v1/builder"},
		{http.MethodPut, "/api/admin/v1/builder"},
		{http.MethodPost, "/api/admin/v1/builder/test"},
		{http.MethodGet, "/api/admin/v1/releases"},
		{http.MethodPost, "/api/admin/v1/releases"},
		{http.MethodGet, "/api/admin/v1/releases/1"},
		{http.MethodPost, "/api/admin/v1/releases/1/retry"},
		{http.MethodGet, "/api/internal/v1/releases/1/bundle"},
		{http.MethodPost, "/api/internal/v1/jenkins/callback"},
	}
	for _, request := range requests {
		t.Run(request.method+" "+request.path, func(t *testing.T) {
			response := performHandlerRequest(system.router, request.method, request.path, "application/json", `{}`, nil)
			require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		})
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/admin/v1/builder", nil)
	context.Set(requestIDKey, "handler-request-42")
	(&stage3ContractAdapter{}).GetBuilderConfig(context)
	requireProblemResponse(t, recorder, http.StatusServiceUnavailable, "dependency_unavailable")
}

func TestAdminHandlerPreviewUsesArticleDetailForImmutableSlugAndDraft(t *testing.T) {
	system := newStage2System(t, true)
	response := performHandlerRequest(system.router, http.MethodGet, "/api/admin/v1/articles/11/preview", "", "", nil)
	require.Equal(t, http.StatusOK, response.Code)
	var preview PreviewView
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
	require.Equal(t, stage2Article().Slug, preview.Slug)
	require.Equal(t, stage2Draft().ID, preview.Draft.Id)
	require.Equal(t, 1, system.articles.calls)
	require.Zero(t, system.revisions.calls, "preview draft must come from the same article detail load")
}

func TestAdminHandlerAuthenticatesBeforeEveryStage2DomainCall(t *testing.T) {
	system := newStage2System(t, false)
	requests := []struct{ method, path, body string }{
		{"GET", "/api/admin/v1/articles", ""}, {"POST", "/api/admin/v1/articles", ""}, {"GET", "/api/admin/v1/articles/11", ""},
		{"PUT", "/api/admin/v1/articles/11/draft", `{}`}, {"GET", "/api/admin/v1/articles/11/preview", ""},
		{"GET", "/api/admin/v1/articles/11/versions", ""}, {"POST", "/api/admin/v1/articles/11/versions", `{}`},
		{"POST", "/api/admin/v1/articles/11/versions/13/restore", `{}`}, {"POST", "/api/admin/v1/articles/11/trash", ""},
		{"POST", "/api/admin/v1/articles/11/untrash", ""}, {"GET", "/api/admin/v1/tags", ""}, {"POST", "/api/admin/v1/tags", `{}`},
		{"PATCH", "/api/admin/v1/tags/21", `{}`}, {"POST", "/api/admin/v1/media/upload-policy", ""}, {"POST", "/api/admin/v1/media", `{}`},
		{"GET", "/api/admin/v1/settings/site", ""}, {"PUT", "/api/admin/v1/settings/site", `{}`},
		{"GET", "/api/admin/v1/settings/hotlink", ""}, {"PUT", "/api/admin/v1/settings/hotlink", `{}`},
	}
	for _, request := range requests {
		contentType := ""
		if request.body != "" {
			contentType = "application/json"
		}
		response := performHandlerRequest(system.router, request.method, request.path, contentType, request.body, nil)
		require.Equal(t, http.StatusUnauthorized, response.Code, request.method+" "+request.path+": "+response.Body.String())
	}
	require.Zero(t, system.articles.calls)
	require.Zero(t, system.revisions.calls)
	require.Zero(t, system.tags.calls)
	require.Zero(t, system.media.calls)
	require.Zero(t, system.site.calls)
	require.Zero(t, system.hotlink.calls)
}

func TestAdminHandlerRejectsMalformedBoundedJSONAndInvalidParameters(t *testing.T) {
	tests := []struct {
		name, method, path, contentType, body string
	}{
		{"unknown", "POST", "/api/admin/v1/tags", "application/json", `{"name":"Go","extra":true}`},
		{"duplicate", "POST", "/api/admin/v1/tags", "application/json", `{"name":"Go","name":"Rust"}`},
		{"missing", "POST", "/api/admin/v1/tags", "application/json", `{}`},
		{"nested duplicate", "PUT", "/api/admin/v1/settings/hotlink", "application/json", `{"allowEmptyReferer":false,"entries":[{"hostname":"example.com","hostname":"other.com","enabled":true}]}`},
		{"trailing", "POST", "/api/admin/v1/tags", "application/json", `{"name":"Go"}{}`},
		{"wrong type", "POST", "/api/admin/v1/tags", "text/plain", `{"name":"Go"}`},
		{"unsupported charset", "POST", "/api/admin/v1/tags", "application/json;charset=latin1", `{"name":"Go"}`},
		{"oversized small", "POST", "/api/admin/v1/tags", "application/json", `{"name":"` + strings.Repeat("x", 64*1024) + `"}`},
		{"oversized markdown", "PUT", "/api/admin/v1/articles/11/draft", "application/json", `{"lockVersion":1,"title":"t","summary":"","coverMediaId":null,"contentMd":"` + strings.Repeat("x", 2*1024*1024) + `","tagIds":[]}`},
		{"zero article", "GET", "/api/admin/v1/articles/0", "", ""},
		{"negative tag", "PATCH", "/api/admin/v1/tags/-1", "application/json", `{"name":"Go"}`},
		{"invalid state", "GET", "/api/admin/v1/articles?state=published", "", ""},
		{"unknown query", "GET", "/api/admin/v1/articles?search=secret", "", ""},
		{"repeated state", "GET", "/api/admin/v1/articles?state=active&state=trashed", "", ""},
		{"malformed query escape", "GET", "/api/admin/v1/articles?state=%ZZ", "", ""},
		{"empty query delimiter", "GET", "/api/admin/v1/articles?", "", ""},
		{"query where absent", "GET", "/api/admin/v1/tags?state=active", "", ""},
		{"non numeric path", "GET", "/api/admin/v1/articles/not-a-number", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			system := newStage2System(t, true)
			response := performHandlerRequest(system.router, test.method, test.path, test.contentType, test.body, nil)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			requireProblemResponse(t, response, http.StatusBadRequest, "invalid_request")
			require.Zero(t, system.articles.calls)
			require.Zero(t, system.revisions.calls)
			require.Zero(t, system.tags.calls)
		})
	}
}

func TestAdminHandlerRejectsInvalidUTF8RawJSONWithoutDomainCalls(t *testing.T) {
	tests := []struct {
		name, method, path string
		body               []byte
	}{
		{"object key", http.MethodPost, "/api/admin/v1/tags", append(append([]byte(`{"na`), 0xff), []byte(`me":"Go"}`)...)},
		{"top-level string", http.MethodPost, "/api/admin/v1/tags", append(append([]byte(`{"name":"G`), 0xff), []byte(`o"}`)...)},
		{"nested string", http.MethodPut, "/api/admin/v1/settings/hotlink", append(append([]byte(`{"allowEmptyReferer":false,"entries":[{"hostname":"exam`), 0xff), []byte(`ple.com","enabled":true}]}`)...)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			system := newStage2System(t, true)
			response := performRawHandlerRequest(system.router, test.method, test.path, "application/json", test.body)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			requireProblemResponse(t, response, http.StatusBadRequest, "invalid_request")
			require.NotContains(t, response.Body.String(), string([]byte{0xff}))
			require.Zero(t, system.tags.calls)
			require.Zero(t, system.hotlink.calls)
		})
	}
}

func TestAdminHandlerRejectsUnexpectedQueryOnEveryStage2Endpoint(t *testing.T) {
	requests := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/admin/v1/articles"},
		{http.MethodPost, "/api/admin/v1/articles"},
		{http.MethodGet, "/api/admin/v1/articles/11"},
		{http.MethodPut, "/api/admin/v1/articles/11/draft"},
		{http.MethodGet, "/api/admin/v1/articles/11/preview"},
		{http.MethodGet, "/api/admin/v1/articles/11/versions"},
		{http.MethodPost, "/api/admin/v1/articles/11/versions"},
		{http.MethodPost, "/api/admin/v1/articles/11/versions/13/restore"},
		{http.MethodPost, "/api/admin/v1/articles/11/trash"},
		{http.MethodPost, "/api/admin/v1/articles/11/untrash"},
		{http.MethodGet, "/api/admin/v1/tags"},
		{http.MethodPost, "/api/admin/v1/tags"},
		{http.MethodPatch, "/api/admin/v1/tags/21"},
		{http.MethodPost, "/api/admin/v1/media/upload-policy"},
		{http.MethodPost, "/api/admin/v1/media"},
		{http.MethodGet, "/api/admin/v1/settings/site"},
		{http.MethodPut, "/api/admin/v1/settings/site"},
		{http.MethodGet, "/api/admin/v1/settings/hotlink"},
		{http.MethodPut, "/api/admin/v1/settings/hotlink"},
	}
	for _, request := range requests {
		t.Run(request.method+" "+request.path, func(t *testing.T) {
			system := newStage2System(t, true)
			response := performHandlerRequest(system.router, request.method, request.path+"?unexpected=value", "", "", nil)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			requireProblemResponse(t, response, http.StatusBadRequest, "invalid_request")
			require.Zero(t, system.articles.calls)
			require.Zero(t, system.revisions.calls)
			require.Zero(t, system.tags.calls)
			require.Zero(t, system.media.calls)
			require.Zero(t, system.site.calls)
			require.Zero(t, system.hotlink.calls)
		})
	}
}

func TestPutHotlinkSettingsMapsConflictToDocumentedProblem(t *testing.T) {
	system := newStage2System(t, true)
	system.hotlink.err = settings.ErrConflict
	response := performHandlerRequest(system.router, http.MethodPut, "/api/admin/v1/settings/hotlink", "application/json", `{"allowEmptyReferer":false,"entries":[]}`, nil)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	requireProblemResponse(t, response, http.StatusConflict, "settings_conflict")
}

func TestAdminHandlerAcceptsExactJSONBodyLimits(t *testing.T) {
	tests := []struct {
		name, method, path, prefix, suffix string
		limit                              int
		status                             int
	}{
		{"small JSON", http.MethodPost, "/api/admin/v1/tags", `{"name":"`, `"}`, maxAdminJSONBodyBytes, http.StatusCreated},
		{"markdown JSON", http.MethodPut, "/api/admin/v1/articles/11/draft", `{"lockVersion":1,"title":"t","summary":"","coverMediaId":null,"contentMd":"`, `","tagIds":[]}`, maxAdminMarkdownBodyBytes, http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Less(t, len(test.prefix)+len(test.suffix), test.limit)
			body := test.prefix + strings.Repeat("x", test.limit-len(test.prefix)-len(test.suffix)) + test.suffix
			require.Len(t, body, test.limit)
			system := newStage2System(t, true)
			response := performHandlerRequest(system.router, test.method, test.path, "application/json", body, nil)
			require.Equal(t, test.status, response.Code, response.Body.String())
		})
	}
}

func TestAdminHandlerAuthenticatesBeforeGeneratedBindingErrors(t *testing.T) {
	system := newStage2System(t, false)
	for _, path := range []string{
		"/api/admin/v1/articles/not-a-number",
		"/api/admin/v1/articles?state=published",
		"/api/admin/v1/tags/not-a-number",
	} {
		response := performHandlerRequest(system.router, http.MethodGet, path, "", "", nil)
		if strings.Contains(path, "/tags/") {
			response = performHandlerRequest(system.router, http.MethodPatch, path, "application/json", `{}`, nil)
		}
		require.Equal(t, http.StatusUnauthorized, response.Code, path+": "+response.Body.String())
		requireProblemResponse(t, response, http.StatusUnauthorized, "unauthenticated")
	}
	require.Zero(t, system.articles.calls)
	require.Zero(t, system.tags.calls)
}

func TestAdminHandlerGeneratedBindingPrefersSessionDependencyFailure(t *testing.T) {
	system := newStage2System(t, false)
	system.router = gin.New()
	system.router.Use(RequestID(), func(c *gin.Context) {
		c.Set(sessionStateKey, sessionState{err: errors.Join(auth.ErrDependencyUnavailable, errors.New("redis token secret"))})
		c.Next()
	})
	handler, err := NewAdminHandler(NewAuthHandler(auth.Service{}, config.SessionConfig{CookieName: "qx_blog_session", TTL: time.Hour}), system.articles, system.revisions, system.tags, system.media, system.site, system.hotlink)
	require.NoError(t, err)
	RegisterAdminHandlers(system.router, handler)

	response := performHandlerRequest(system.router, http.MethodGet, "/api/admin/v1/articles/not-a-number", "", "", nil)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	requireProblemResponse(t, response, http.StatusServiceUnavailable, "dependency_unavailable")
	require.NotContains(t, response.Body.String(), "secret")
	require.Zero(t, system.articles.calls)
}

func TestRegisterAdminHandlersAuthenticatesBeforeGeneratedBinding(t *testing.T) {
	system := newStage2System(t, false)
	handler, err := NewAdminHandler(NewAuthHandler(auth.Service{}, config.SessionConfig{CookieName: "qx_blog_session", TTL: time.Hour}), system.articles, system.revisions, system.tags, system.media, system.site, system.hotlink)
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID())
	bindingErrors := 0
	registerAdminHandlersWithErrorHandler(router, handler, func(c *gin.Context, _ error, _ int) {
		bindingErrors++
		WriteProblem(c, ErrInvalidRequest)
	})

	response := performHandlerRequest(router, http.MethodGet, "/api/admin/v1/articles/not-a-number", "", "", nil)
	require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
	require.Zero(t, bindingErrors, "binding must not inspect anonymous path/query values")
}

func TestAdminHandlerMapsStage2ErrorsWithoutLeakingCauses(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
		key  string
	}{
		{"revision conflict", revision.ErrConflict, 409, "revision_conflict"},
		{"settings conflict", settings.ErrConflict, 409, "settings_conflict"},
		{"article conflict", article.ErrStateConflict, 409, "article_state_conflict"},
		{"published trash", article.ErrMustBeUnpublished, 409, "article_must_be_unpublished"},
		{"tag conflict", tag.ErrNameConflict, 409, "tag_conflict"},
		{"invalid content", revision.ErrInvalidContent, 422, "invalid_content"},
		{"invalid media", media.ErrInvalidMetadata, 422, "invalid_media"},
		{"article missing", article.ErrNotFound, 404, "not_found"},
		{"revision missing", revision.ErrNotFound, 404, "not_found"},
		{"tag missing", tag.ErrNotFound, 404, "not_found"},
		{"settings invalid", settings.ErrInvalid, 422, "invalid_settings"},
		{"operational", errors.New("db password secret-value"), 503, "dependency_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			system := newStage2System(t, true)
			system.tags.err = test.err
			response := performHandlerRequest(system.router, "GET", "/api/admin/v1/tags", "", "", nil)
			require.Equal(t, test.code, response.Code, response.Body.String())
			requireProblemResponse(t, response, test.code, test.key)
			require.NotContains(t, response.Body.String(), "secret-value")
		})
	}
}

func TestNewAdminHandlerRejectsNilAndTypedNilDependencies(t *testing.T) {
	system := newStage2System(t, true)
	validAuth := NewAuthHandler(auth.Service{}, config.SessionConfig{CookieName: "qx_blog_session", TTL: time.Hour})
	var typedNil *stage2ArticleFake
	_, err := NewAdminHandler(validAuth, typedNil, system.revisions, system.tags, system.media, system.site, system.hotlink)
	require.EqualError(t, err, "article service is required")
	_, err = NewAdminHandler(nil, system.articles, system.revisions, system.tags, system.media, system.site, system.hotlink)
	require.EqualError(t, err, "auth handler is required")
	invalidAuth := NewAuthHandler(auth.Service{}, config.SessionConfig{CookieName: "invalid;cookie", TTL: time.Hour})
	_, err = NewAdminHandler(invalidAuth, system.articles, system.revisions, system.tags, system.media, system.site, system.hotlink)
	require.EqualError(t, err, "auth handler is required")

	tests := []struct {
		name string
		call func() error
		want string
	}{
		{"revision", func() error {
			_, err := NewAdminHandler(validAuth, system.articles, nil, system.tags, system.media, system.site, system.hotlink)
			return err
		}, "revision service is required"},
		{"tag", func() error {
			_, err := NewAdminHandler(validAuth, system.articles, system.revisions, nil, system.media, system.site, system.hotlink)
			return err
		}, "tag service is required"},
		{"media", func() error {
			_, err := NewAdminHandler(validAuth, system.articles, system.revisions, system.tags, nil, system.site, system.hotlink)
			return err
		}, "media service is required"},
		{"site", func() error {
			_, err := NewAdminHandler(validAuth, system.articles, system.revisions, system.tags, system.media, nil, system.hotlink)
			return err
		}, "site settings service is required"},
		{"hotlink", func() error {
			_, err := NewAdminHandler(validAuth, system.articles, system.revisions, system.tags, system.media, system.site, nil)
			return err
		}, "hotlink settings service is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { require.EqualError(t, test.call(), test.want) })
	}
}

func TestAdminHandlerSetsOnlyNumericSafeLogContext(t *testing.T) {
	system := newStage2System(t, true)
	response := performHandlerRequest(system.router, "GET", "/api/admin/v1/articles/11", "", "", nil)
	require.Equal(t, http.StatusOK, response.Code)
	// Access-log middleware consumes these helpers in Task 12; public keys and
	// request strings deliberately have no corresponding log-context helper.
	require.Equal(t, int64(7), system.log.adminID)
	require.Equal(t, int64(11), system.log.articleID)
}

func stage2Article() article.Article {
	published := int64(17)
	return article.Article{ID: 11, Slug: "a_slug", DraftRevisionID: 12, PublishedRevisionID: &published, State: article.StateActive, CreatedAt: stage2TestTime, UpdatedAt: stage2TestTime}
}

func stage2Draft() revision.Draft {
	cover := int64(31)
	return revision.Draft{ID: 12, ArticleID: 11, RevisionNo: 1, LockVersion: 1, Status: revision.StatusEditing, Reason: revision.ReasonDraft, Title: "Draft title", Summary: "Summary", CoverMediaID: &cover, ContentMD: "body", ContentHash: "abc", Tags: []tag.Snapshot{{TagID: 21, Name: "Go", Slug: "t_go", Position: 0}}, Media: []media.Reference{{MediaID: 31, PublicKey: "m_aaaaaaaaaaaaaaaaaaaaaa", Purpose: "cover", Position: 0}}, CreatedAt: stage2TestTime, UpdatedAt: stage2TestTime}
}

func stage2VersionDraft() revision.Draft {
	item := stage2Draft()
	item.ID = 13
	item.Status = revision.StatusFrozen
	item.Reason = revision.ReasonManualVersion
	return item
}

func stage2Tag() tag.Tag {
	return tag.Tag{ID: 21, Name: "Go", Slug: "t_go", CreatedAt: stage2TestTime, UpdatedAt: stage2TestTime}
}

func siteRequestJSON() string {
	return `{"lockVersion":2,"siteName":"qiuxs","authorName":"qiuxs","authorBio":"","homeStatus":"","aboutMd":"about","socialLinks":[{"label":"GitHub","url":"https://github.com/qiuxs"}],"seoDefaultTitle":"","seoDefaultDescription":"","seoDefaultImageMediaId":null,"filingName":"长安休息室","filingNumber":"浙ICP备17057726号-1"}`
}

func performRawHandlerRequest(router http.Handler, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("X-Request-ID", "handler-request-42")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
