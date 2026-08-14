package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/builder"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/release"
	"github.com/stretchr/testify/require"
)

func TestReleaseHandlerAdminViewsContainNoSecretsAndUseCurrentAdmin(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	configRepository := &releaseBuilderRepository{stored: builder.StoredConfig{ConfigView: builder.ConfigView{
		ID: 9, Name: "Production", BaseURL: "https://jenkins.example.com", Username: "builder", JobName: "blog/deploy", Enabled: true, TokenConfigured: true,
	}, EncryptedToken: "encrypted-token-secret"}}
	reader := &releaseReaderStub{aggregate: testReleaseAggregate(now)}
	operations := &releaseOperationsStub{created: reader.aggregate.Release, createdJob: reader.aggregate.Jobs[0]}
	handler := newTestReleaseHandler(t, configRepository, reader, operations)
	router := releaseAdminTestRouter(handler, 41)

	getBuilder := serveReleaseRequest(router, http.MethodGet, "/api/admin/v1/builder", "", "")
	require.Equal(t, http.StatusOK, getBuilder.Code, getBuilder.Body.String())
	require.NotContains(t, getBuilder.Body.String(), "encrypted-token-secret")
	require.NotContains(t, getBuilder.Body.String(), "token-secret")
	var configView BuilderConfigView
	require.NoError(t, json.Unmarshal(getBuilder.Body.Bytes(), &configView))
	require.Equal(t, int64(9), configView.Id)
	require.True(t, configView.TokenConfigured)

	create := serveReleaseRequest(router, http.MethodPost, "/api/admin/v1/releases", "application/json", `{"mode":"publish_article","articleId":11}`)
	require.Equal(t, http.StatusAccepted, create.Code, create.Body.String())
	require.Equal(t, release.CreateCommand{Mode: release.PublishArticle, ArticleID: 11, RequestedBy: 41}, operations.publishCommand)
	var created CreateReleaseResult
	require.NoError(t, json.Unmarshal(create.Body.Bytes(), &created))
	require.Equal(t, int64(7), created.Release.Id)
	require.Equal(t, created.Job, created.Release.LatestJob)
	require.Equal(t, []PublishJobView{created.Job}, created.Release.Jobs)

	list := serveReleaseRequest(router, http.MethodGet, "/api/admin/v1/releases?limit=2&offset=3", "", "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.Equal(t, release.ListQuery{Limit: 2, Offset: 3}, reader.listQuery)
	get := serveReleaseRequest(router, http.MethodGet, "/api/admin/v1/releases/7", "", "")
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	retry := serveReleaseRequest(router, http.MethodPost, "/api/admin/v1/releases/7/retry", "", "")
	require.Equal(t, http.StatusAccepted, retry.Code, retry.Body.String())
	require.Equal(t, int64(7), operations.retryID)
}

func TestReleaseHandlerBuilderSaveAndTestNeverReturnToken(t *testing.T) {
	configRepository := &releaseBuilderRepository{stored: testStoredBuilder()}
	tester := &builderTesterStub{}
	handler := newTestReleaseHandlerWithTester(t, configRepository, &releaseReaderStub{aggregate: testReleaseAggregate(testReleaseTime())}, &releaseOperationsStub{}, tester)
	router := releaseAdminTestRouter(handler, 41)

	saved := serveReleaseRequest(router, http.MethodPut, "/api/admin/v1/builder", "application/json", `{"name":"Production","baseUrl":"https://jenkins.example.com","username":"builder","token":"write-token-secret","jobName":"blog/deploy","enabled":true}`)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	require.Equal(t, "write-token-secret", configRepository.saved.Token)
	require.NotContains(t, saved.Body.String(), "write-token-secret")

	tested := serveReleaseRequest(router, http.MethodPost, "/api/admin/v1/builder/test", "", "")
	require.Equal(t, http.StatusNoContent, tested.Code, tested.Body.String())
	require.Empty(t, tested.Body.Bytes())
	require.Equal(t, 1, tester.calls)
	require.Equal(t, configRepository.stored.ID, tester.config.ID)
}

func TestPutBuilderConfigOmitsTokenForExistingConfiguration(t *testing.T) {
	configs := &releaseBuilderRepository{stored: testStoredBuilder()}
	handler := newTestReleaseHandler(t, configs, &releaseReaderStub{aggregate: testReleaseAggregate(testReleaseTime())}, &releaseOperationsStub{})
	router := releaseAdminTestRouter(handler, 41)

	response := serveReleaseRequest(router, http.MethodPut, "/api/admin/v1/builder", "application/json", `{"name":"Production","baseUrl":"https://jenkins.example.com","username":"builder","jobName":"blog/deploy","enabled":true}`)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, configs.saveCalls)
	require.Empty(t, configs.saved.Token)
	require.Contains(t, response.Body.String(), `"tokenConfigured":true`)
}

func TestPutBuilderConfigRejectsExplicitNullToken(t *testing.T) {
	configs := &releaseBuilderRepository{stored: testStoredBuilder()}
	handler := newTestReleaseHandler(t, configs, &releaseReaderStub{aggregate: testReleaseAggregate(testReleaseTime())}, &releaseOperationsStub{})
	router := releaseAdminTestRouter(handler, 41)

	response := serveReleaseRequest(router, http.MethodPut, "/api/admin/v1/builder", "application/json", `{"name":"Production","baseUrl":"https://jenkins.example.com","username":"builder","token":null,"jobName":"blog/deploy","enabled":true}`)

	requireProblemResponse(t, response, http.StatusBadRequest, "invalid_request")
	require.Zero(t, configs.saveCalls)
}

func TestPutBuilderConfigFirstSaveWithoutTokenMapsInvalidConfig(t *testing.T) {
	configs := &releaseBuilderRepository{saveErr: builder.ErrInvalidConfig}
	handler := newTestReleaseHandler(t, configs, &releaseReaderStub{aggregate: testReleaseAggregate(testReleaseTime())}, &releaseOperationsStub{})
	router := releaseAdminTestRouter(handler, 41)

	response := serveReleaseRequest(router, http.MethodPut, "/api/admin/v1/builder", "application/json", `{"name":"Production","baseUrl":"https://jenkins.example.com","username":"builder","jobName":"blog/deploy","enabled":true}`)

	requireProblemResponse(t, response, http.StatusUnprocessableEntity, "invalid_builder")
	require.Equal(t, 1, configs.saveCalls)
	require.Empty(t, configs.saved.Token)
}

func TestTestBuilderConfigMissingConfigurationIsPrecondition(t *testing.T) {
	configs := &releaseBuilderRepository{loadErr: errors.Join(builder.ErrNotFound, errors.New("builder-secret"))}
	tester := &builderTesterStub{}
	handler := newTestReleaseHandlerWithTester(t, configs, &releaseReaderStub{aggregate: testReleaseAggregate(testReleaseTime())}, &releaseOperationsStub{}, tester)
	router := releaseAdminTestRouter(handler, 41)

	response := serveReleaseRequest(router, http.MethodPost, "/api/admin/v1/builder/test", "", "")

	requireProblemResponse(t, response, http.StatusPreconditionFailed, "precondition_failed")
	require.Zero(t, tester.calls)
	require.Contains(t, response.Body.String(), `"requestId":"handler-request-42"`)
	require.NotContains(t, response.Body.String(), "builder-secret")
}

func TestReleaseHandlerBundleNegotiatesGzipWithStableIdentityETag(t *testing.T) {
	body := []byte(`{"schemaVersion":1,"releaseId":7}`)
	etag := "sha256:" + strings.Repeat("a", 64)
	releaseService := &releaseBundleStub{body: body, etag: etag}
	handler := newTestReleaseHandlerWithBundle(t, releaseService)

	for _, test := range []struct {
		name, acceptEncoding string
		wantGzip             bool
	}{
		{name: "identity"},
		{name: "gzip", acceptEncoding: "br, gzip", wantGzip: true},
		{name: "case and quality", acceptEncoding: "GZip; q=0.5", wantGzip: true},
		{name: "gzip disabled", acceptEncoding: "gzip;q=0"},
		{name: "wildcard", acceptEncoding: "*;q=0.8", wantGzip: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(RequestID())
			router.GET("/api/internal/v1/releases/:releaseId/bundle", RequireBundleToken([]byte(strings.Repeat("b", 32))), func(c *gin.Context) {
				handler.GetReleaseBundle(c, 7)
			})
			request := httptest.NewRequest(http.MethodGet, "/api/internal/v1/releases/7/bundle", nil)
			request.Header.Set("X-Request-ID", "handler-request-42")
			request.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 32))
			request.Header.Set("Accept-Encoding", test.acceptEncoding)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, `"`+etag+`"`, response.Header().Get("ETag"))
			require.Equal(t, "Accept-Encoding", response.Header().Get("Vary"))
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			require.Equal(t, strconv.Itoa(response.Body.Len()), response.Header().Get("Content-Length"))
			decoded := response.Body.Bytes()
			if test.wantGzip {
				require.Equal(t, "gzip", response.Header().Get("Content-Encoding"))
				reader, err := gzip.NewReader(bytes.NewReader(decoded))
				require.NoError(t, err)
				decoded, err = io.ReadAll(reader)
				require.NoError(t, err)
				require.NoError(t, reader.Close())
			} else {
				require.Empty(t, response.Header().Get("Content-Encoding"))
			}
			require.Equal(t, body, decoded)
		})
	}
}

func TestReleaseHandlerCallbackAppliesAuthenticatedClaimExactlyOnce(t *testing.T) {
	operations := &releaseOperationsStub{}
	handler := newTestReleaseHandler(t, &releaseBuilderRepository{stored: testStoredBuilder()}, &releaseReaderStub{aggregate: testReleaseAggregate(testReleaseTime())}, operations)
	payload := builder.CallbackPayload{ReleaseID: 7, PublishJobID: 12, BuildNumber: 41, Stage: "build", Status: release.JobBuilding, Timestamp: testReleaseTime(), Nonce: "nonce_1234567890"}
	for _, duplicate := range []bool{false, true} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set(jenkinsCallbackContextKey, jenkinsCallbackClaim{payload: payload, duplicate: duplicate})
			c.Next()
		})
		router.POST("/callback", handler.AcceptJenkinsCallback)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/callback", nil))
		require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
		require.Empty(t, response.Body.Bytes())
		require.Equal(t, payload.Event(), operations.callbackEvent)
		require.Equal(t, duplicate, operations.callbackDuplicate)
	}
	require.Equal(t, 2, operations.callbackCalls)
}

func TestRegisterInternalReleaseHandlersKeepsBundleAndCallbackOutsideAdminMiddleware(t *testing.T) {
	system := newStage2System(t, true)
	verifier := &callbackVerifierStub{payload: builder.CallbackPayload{
		ReleaseID: 7, PublishJobID: 12, BuildNumber: 41, Stage: "build", Status: release.JobBuilding,
		Timestamp: testReleaseTime(), Nonce: "nonce_1234567890",
	}}
	router := gin.New()
	router.Use(RequestID())
	RegisterInternalReleaseHandlers(router, system.handler, []byte(strings.Repeat("b", 32)), verifier)

	cookieOnly := httptest.NewRequest(http.MethodGet, "/api/internal/v1/releases/not-a-number/bundle", nil)
	cookieOnly.Header.Set("X-Request-ID", "handler-request-42")
	cookieOnly.AddCookie(&http.Cookie{Name: "qx_blog_session", Value: "admin-cookie-secret"})
	cookieResponse := httptest.NewRecorder()
	router.ServeHTTP(cookieResponse, cookieOnly)
	requireProblemResponse(t, cookieResponse, http.StatusUnauthorized, "internal_unauthorized")

	bundle := httptest.NewRequest(http.MethodGet, "/api/internal/v1/releases/7/bundle", nil)
	bundle.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 32))
	bundle.Header.Set("X-Request-ID", "handler-request-42")
	bundleResponse := httptest.NewRecorder()
	router.ServeHTTP(bundleResponse, bundle)
	require.Equal(t, http.StatusOK, bundleResponse.Code, bundleResponse.Body.String())

	callback := httptest.NewRequest(http.MethodPost, "/api/internal/v1/jenkins/callback", strings.NewReader(`{}`))
	callback.Header.Set("Content-Type", "application/json")
	callback.Header.Set("X-Jenkins-Signature", "sha256="+strings.Repeat("a", 64))
	callback.Header.Set("X-Request-ID", "handler-request-42")
	callbackResponse := httptest.NewRecorder()
	router.ServeHTTP(callbackResponse, callback)
	require.Equal(t, http.StatusNoContent, callbackResponse.Code, callbackResponse.Body.String())
	require.Equal(t, 1, verifier.calls)
}

func TestReleaseHandlerMapsDomainFailuresToDocumentedProblems(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "builder invalid", err: builder.ErrInvalidConfig, want: 422, code: "invalid_builder"},
		{name: "builder conflict", err: builder.ErrConflict, want: 409, code: "builder_conflict"},
		{name: "builder missing", err: builder.ErrNotFound, want: 404, code: "not_found"},
		{name: "release busy", err: release.ErrBusy, want: 409, code: "release_conflict"},
		{name: "release reconciliation", err: release.ErrReconciliationRequired, want: 412, code: "precondition_failed"},
		{name: "release missing", err: release.ErrNotFound, want: 404, code: "not_found"},
		{name: "dependency", err: release.ErrDependencyUnavailable, want: 503, code: "dependency_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Set(requestIDKey, "handler-request-42")
			writeReleaseProblem(context, errors.Join(test.err, errors.New("release-token-secret")))
			requireProblemResponse(t, recorder, test.want, test.code)
		})
	}
}

func newTestReleaseHandler(t *testing.T, configs *releaseBuilderRepository, reader *releaseReaderStub, operations *releaseOperationsStub) *ReleaseHandler {
	t.Helper()
	return newTestReleaseHandlerWithTester(t, configs, reader, operations, &builderTesterStub{})
}

func newTestReleaseHandlerWithTester(t *testing.T, configs *releaseBuilderRepository, reader *releaseReaderStub, operations *releaseOperationsStub, tester *builderTesterStub) *ReleaseHandler {
	t.Helper()
	return newTestReleaseHandlerDependencies(t, configs, tester, reader, &releaseBundleStub{body: []byte(`{}`), etag: "sha256:" + strings.Repeat("a", 64)}, operations)
}

func newTestReleaseHandlerWithBundle(t *testing.T, bundle *releaseBundleStub) *ReleaseHandler {
	t.Helper()
	return newTestReleaseHandlerDependencies(t, &releaseBuilderRepository{stored: testStoredBuilder()}, &builderTesterStub{}, &releaseReaderStub{aggregate: testReleaseAggregate(testReleaseTime())}, bundle, &releaseOperationsStub{})
}

func newTestReleaseHandlerDependencies(t *testing.T, configs *releaseBuilderRepository, tester *builderTesterStub, reader *releaseReaderStub, bundle *releaseBundleStub, operations *releaseOperationsStub) *ReleaseHandler {
	t.Helper()
	box := &platform.SecretBox{}
	handler, err := NewReleaseHandler(configs, tester, box, reader, bundle, operations)
	require.NoError(t, err)
	return handler
}

func releaseAdminTestRouter(handler *ReleaseHandler, adminID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID(), func(c *gin.Context) {
		c.Set(sessionStateKey, sessionState{admin: auth.Admin{ID: adminID, Username: "admin", State: "active"}})
		c.Next()
	})
	router.GET("/api/admin/v1/builder", handler.GetBuilderConfig)
	router.PUT("/api/admin/v1/builder", handler.PutBuilderConfig)
	router.POST("/api/admin/v1/builder/test", handler.TestBuilderConfig)
	router.GET("/api/admin/v1/releases", func(c *gin.Context) {
		handler.ListReleases(c, ListReleasesParams{Limit: intPointer(2), Offset: intPointer(3)})
	})
	router.POST("/api/admin/v1/releases", handler.CreateRelease)
	router.GET("/api/admin/v1/releases/:releaseId", func(c *gin.Context) { handler.GetRelease(c, 7) })
	router.POST("/api/admin/v1/releases/:releaseId/retry", func(c *gin.Context) { handler.RetryRelease(c, 7) })
	return router
}

func serveReleaseRequest(router http.Handler, method, target, contentType, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("X-Request-ID", "handler-request-42")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	router.ServeHTTP(response, request)
	return response
}

func testReleaseTime() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC) }

func testStoredBuilder() builder.StoredConfig {
	return builder.StoredConfig{ConfigView: builder.ConfigView{ID: 9, Name: "Production", BaseURL: "https://jenkins.example.com", Username: "builder", JobName: "blog/deploy", Enabled: true, TokenConfigured: true}, EncryptedToken: "ciphertext"}
}

func testReleaseAggregate(now time.Time) release.Aggregate {
	job := release.PublishJob{ID: 12, ReleaseID: 7, BuilderID: 9, Status: release.JobPending, Stage: "pending", CreatedAt: now}
	return release.Aggregate{Release: release.Release{ID: 7, Status: release.ReleaseQueued, Checksum: "sha256:" + strings.Repeat("a", 64), CreatedAt: now}, Jobs: []release.PublishJob{job}}
}

type releaseBuilderRepository struct {
	stored    builder.StoredConfig
	saved     builder.ConfigInput
	saveCalls int
	loadErr   error
	saveErr   error
}

func (r *releaseBuilderRepository) Load(context.Context) (builder.StoredConfig, error) {
	return r.stored, r.loadErr
}
func (r *releaseBuilderRepository) Save(_ context.Context, input builder.ConfigInput) (builder.ConfigView, error) {
	r.saveCalls++
	r.saved = input
	return r.stored.ConfigView, r.saveErr
}

type builderTesterStub struct {
	config builder.StoredConfig
	calls  int
	err    error
}

func (t *builderTesterStub) Test(_ context.Context, config builder.StoredConfig, _ *platform.SecretBox) error {
	t.calls++
	t.config = config
	return t.err
}

type releaseReaderStub struct {
	aggregate release.Aggregate
	items     []release.Aggregate
	listQuery release.ListQuery
	err       error
}

func (r *releaseReaderStub) FindRelease(context.Context, int64) (release.Aggregate, error) {
	return r.aggregate, r.err
}
func (r *releaseReaderStub) ListReleases(_ context.Context, query release.ListQuery) ([]release.Aggregate, error) {
	r.listQuery = query
	if r.items == nil {
		return []release.Aggregate{r.aggregate}, r.err
	}
	return r.items, r.err
}

type releaseBundleStub struct {
	body []byte
	etag string
	err  error
}

func (s *releaseBundleStub) Bundle(context.Context, int64) ([]byte, string, error) {
	return append([]byte(nil), s.body...), s.etag, s.err
}

type releaseOperationsStub struct {
	created           release.Release
	createdJob        release.PublishJob
	publishCommand    release.CreateCommand
	retryID           int64
	callbackEvent     release.CallbackEvent
	callbackDuplicate bool
	callbackCalls     int
	err               error
}

func (o *releaseOperationsStub) Publish(_ context.Context, command release.CreateCommand) (release.Release, release.PublishJob, error) {
	o.publishCommand = command
	return o.created, o.createdJob, o.err
}
func (o *releaseOperationsStub) Retry(_ context.Context, id int64) (release.Aggregate, release.PublishJob, error) {
	o.retryID = id
	aggregate := testReleaseAggregate(testReleaseTime())
	return aggregate, aggregate.Jobs[0], o.err
}
func (o *releaseOperationsStub) Callback(_ context.Context, event release.CallbackEvent, duplicate bool) (release.PublishJob, bool, error) {
	o.callbackCalls++
	o.callbackEvent = event
	o.callbackDuplicate = duplicate
	return release.PublishJob{}, duplicate, o.err
}

func intPointer(value int) *int { return &value }
