package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/httpapi"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/release"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBuildRegistersPublicAndAdminRoutes(t *testing.T) {
	router := buildTestRouter(t)
	want := []string{
		http.MethodGet + " /health/live",
		http.MethodGet + " /health/ready",
		http.MethodPost + " /api/admin/v1/session",
		http.MethodDelete + " /api/admin/v1/session",
		http.MethodGet + " /api/admin/v1/me",
		http.MethodGet + " /api/admin/v1/articles",
		http.MethodPost + " /api/admin/v1/articles",
		http.MethodGet + " /api/admin/v1/articles/:articleId",
		http.MethodPut + " /api/admin/v1/articles/:articleId/draft",
		http.MethodGet + " /api/admin/v1/articles/:articleId/preview",
		http.MethodGet + " /api/admin/v1/articles/:articleId/versions",
		http.MethodPost + " /api/admin/v1/articles/:articleId/versions",
		http.MethodPost + " /api/admin/v1/articles/:articleId/versions/:revisionId/restore",
		http.MethodPost + " /api/admin/v1/articles/:articleId/trash",
		http.MethodPost + " /api/admin/v1/articles/:articleId/untrash",
		http.MethodGet + " /api/admin/v1/tags",
		http.MethodPost + " /api/admin/v1/tags",
		http.MethodPatch + " /api/admin/v1/tags/:tagId",
		http.MethodPost + " /api/admin/v1/media/upload-policy",
		http.MethodPost + " /api/admin/v1/media",
		http.MethodGet + " /api/admin/v1/settings/site",
		http.MethodPut + " /api/admin/v1/settings/site",
		http.MethodGet + " /api/admin/v1/settings/hotlink",
		http.MethodPut + " /api/admin/v1/settings/hotlink",
		http.MethodGet + " /api/admin/v1/builder",
		http.MethodPut + " /api/admin/v1/builder",
		http.MethodPost + " /api/admin/v1/builder/test",
		http.MethodGet + " /api/admin/v1/releases",
		http.MethodPost + " /api/admin/v1/releases",
		http.MethodGet + " /api/admin/v1/releases/:releaseId",
		http.MethodPost + " /api/admin/v1/releases/:releaseId/retry",
		http.MethodGet + " /api/internal/v1/releases/:releaseId/bundle",
		http.MethodPost + " /api/internal/v1/jenkins/callback",
		http.MethodGet + " /img/proxy/:publicKey",
	}
	got := make([]string, 0, len(router.Routes()))
	for _, route := range router.Routes() {
		got = append(got, route.Method+" "+route.Path)
	}
	sort.Strings(want)
	sort.Strings(got)
	require.Equal(t, want, got)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Header.Set("X-Request-ID", "route-test")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusNotFound, response.Code)
	require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
	var problem httpapi.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, httpapi.Problem{
		Type:      "https://qiuxs.com/problems/not_found",
		Title:     "Not found",
		Status:    http.StatusNotFound,
		Code:      "not_found",
		RequestId: "route-test",
	}, problem)
}

func TestBuildEnforcesAdminMiddlewareWithoutApplyingItToPublicMedia(t *testing.T) {
	deps, mock, miniRedis := testDependenciesWithResources(t, io.Discard)
	router, err := Build(testConfig(), deps)
	require.NoError(t, err)

	anonymous := httptest.NewRecorder()
	anonymousRequest := httptest.NewRequest(http.MethodGet, "/api/admin/v1/tags", nil)
	anonymousRequest.Header.Set("X-Request-ID", "anonymous-admin")
	router.ServeHTTP(anonymous, anonymousRequest)
	require.Equal(t, http.StatusUnauthorized, anonymous.Code, anonymous.Body.String())
	requireProblemCode(t, anonymous, "unauthenticated")

	token := testAdminSessionToken()
	seedAdminSession(t, deps.Redis, token, deps.Now())
	for _, origin := range []string{"", "https://wrong-origin.example"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/admin/v1/tags", strings.NewReader(`{"name":"Go"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", origin)
		request.AddCookie(&http.Cookie{Name: testConfig().Session.CookieName, Value: token})
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		requireProblemCode(t, response, "origin_forbidden")
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, allow_empty_referer FROM hotlink_settings WHERE singleton_key = 1")).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	redisCommands := miniRedis.CommandCount()
	public := httptest.NewRecorder()
	publicRequest := httptest.NewRequest(http.MethodGet, "/img/proxy/stable-key-secret", nil)
	publicRequest.Header.Set("Origin", "https://wrong-origin.example")
	publicRequest.AddCookie(&http.Cookie{Name: testConfig().Session.CookieName, Value: token})
	router.ServeHTTP(public, publicRequest)
	require.Equal(t, http.StatusNotFound, public.Code, public.Body.String())
	requireProblemCode(t, public, "not_found")
	require.Equal(t, redisCommands, miniRedis.CommandCount(), "public media must not load an Admin session")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildUsesOneSharedStage2DependencyGraph(t *testing.T) {
	deps := testDependencies(t, io.Discard)
	var observed buildObservation
	deps.observeBuild = func(value buildObservation) { observed = value }

	_, err := Build(testConfig(), deps)
	require.NoError(t, err)
	require.NoError(t, observed.validateShared())
}

func TestPrepareDoesNotReadArtifactUntilExplicitStartupReconcile(t *testing.T) {
	deps := testDependencies(t, io.Discard)
	reads := 0
	deps.ReleaseJSONReader = func() (io.ReadCloser, error) {
		reads++
		return nil, fs.ErrNotExist
	}

	application, err := Prepare(testConfig(), deps)
	require.NoError(t, err)
	require.NotNil(t, application.Handler())
	require.Zero(t, reads, "composition must not read release.json")

	reconciled, err := application.Reconcile(context.Background())
	require.NoError(t, err)
	require.False(t, reconciled)
	require.Equal(t, 1, reads)
}

func TestBuildObservationDetectsDistinctActualConstructorArguments(t *testing.T) {
	tests := []struct {
		name, want string
		role       buildRole
		replace    func(any) any
	}{
		{
			name: "repository ID generator", want: "ID generator", role: buildArticleRepositoryIDs,
			replace: func(any) any { return &idgen.Generator{} },
		},
		{
			name: "service random keys", want: "random key generator", role: buildTagServiceKeys,
			replace: func(any) any {
				keys, err := randomkey.New(bytes.NewReader(make([]byte, 128)))
				require.NoError(t, err)
				return keys
			},
		},
		{
			name: "service clock", want: "clock", role: buildRevisionServiceClock,
			replace: func(any) any { return &buildClock{now: makeBuildClock(time.Unix(2, 0).UTC())} },
		},
		{
			name: "proxy hotlink cache", want: "hotlink service", role: buildMediaProxyHotlink,
			replace: func(value any) any {
				return &distinctHotlinkService{HotlinkService: value.(settings.HotlinkService)}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := testDependencies(t, io.Discard)
			var clockReplacement any
			if test.role == buildRevisionServiceClock {
				deps.Now = makeBuildClock(time.Unix(1, 0).UTC())
				clockReplacement = test.replace(nil)
				replacementClock := clockReplacement.(*buildClock).now
				require.Equal(t, reflect.ValueOf(deps.Now).Pointer(), reflect.ValueOf(replacementClock).Pointer(), "regression requires identical function code with different captured state")
				require.NotEqual(t, deps.Now(), replacementClock())
			}
			deps.mutateBuildArgument = func(role buildRole, value any) any {
				if role == test.role {
					if clockReplacement != nil {
						return clockReplacement
					}
					return test.replace(value)
				}
				return value
			}

			_, observed, err := buildComponents(testConfig(), deps)
			require.NoError(t, err)
			require.ErrorContains(t, observed.validateShared(), test.want)
		})
	}
}

type distinctHotlinkService struct{ settings.HotlinkService }

func makeBuildClock(at time.Time) func() time.Time {
	return (&capturedBuildClock{at: at}).Now
}

type capturedBuildClock struct{ at time.Time }

func (c *capturedBuildClock) Now() time.Time { return c.at }

func TestBuildRejectsInvalidDependenciesAndAuthConfig(t *testing.T) {
	typedNilRandom := (*bytes.Reader)(nil)
	tests := []struct {
		name   string
		mutate func(*config.Config, *Dependencies)
	}{
		{name: "database", mutate: func(_ *config.Config, deps *Dependencies) { deps.DB = nil }},
		{name: "zero database", mutate: func(_ *config.Config, deps *Dependencies) { deps.DB = new(sql.DB) }},
		{name: "redis", mutate: func(_ *config.Config, deps *Dependencies) { deps.Redis = nil }},
		{name: "zero redis", mutate: func(_ *config.Config, deps *Dependencies) { deps.Redis = new(redis.Client) }},
		{name: "logger", mutate: func(_ *config.Config, deps *Dependencies) { deps.Logger = nil }},
		{name: "zero logger", mutate: func(_ *config.Config, deps *Dependencies) { deps.Logger = new(slog.Logger) }},
		{name: "random", mutate: func(_ *config.Config, deps *Dependencies) { deps.Random = nil }},
		{name: "typed nil random", mutate: func(_ *config.Config, deps *Dependencies) { deps.Random = typedNilRandom }},
		{name: "clock", mutate: func(_ *config.Config, deps *Dependencies) { deps.Now = nil }},
		{name: "HTTP client", mutate: func(_ *config.Config, deps *Dependencies) { deps.HTTPClient = nil }},
		{name: "HTTP client timeout", mutate: func(_ *config.Config, deps *Dependencies) { deps.HTTPClient.Timeout = time.Second }},
		{name: "Jenkins HTTP client", mutate: func(_ *config.Config, deps *Dependencies) { deps.JenkinsHTTPClient = nil }},
		{name: "release JSON reader", mutate: func(_ *config.Config, deps *Dependencies) { deps.ReleaseJSONReader = nil }},
		{name: "cookie name", mutate: func(cfg *config.Config, _ *Dependencies) { cfg.Session.CookieName = "bad cookie" }},
		{name: "session TTL", mutate: func(cfg *config.Config, _ *Dependencies) { cfg.Session.TTL = 0 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig()
			deps := testDependencies(t, io.Discard)
			test.mutate(&cfg, &deps)
			router, err := Build(cfg, deps)
			require.Error(t, err)
			require.Nil(t, router)
		})
	}
}

func TestBuildRejectsInvalidDirectConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{name: "environment", mutate: func(cfg *config.Config) { cfg.Environment = "test" }},
		{name: "HTTP address", mutate: func(cfg *config.Config) { cfg.HTTP.Addr = " " }},
		{name: "admin origin", mutate: func(cfg *config.Config) { cfg.HTTP.AdminOrigin = "https://user-secret@admin.example.com" }},
		{name: "noncanonical admin origin", mutate: func(cfg *config.Config) { cfg.HTTP.AdminOrigin += "/" }},
		{name: "production HTTP origin", mutate: func(cfg *config.Config) {
			cfg.Environment = "production"
			cfg.Session.CookieSecure = true
		}},
		{name: "production insecure cookie", mutate: func(cfg *config.Config) {
			cfg.Environment = "production"
			cfg.HTTP.AdminOrigin = "https://admin.example.com"
			cfg.Session.CookieSecure = false
		}},
		{name: "MySQL DSN", mutate: func(cfg *config.Config) { cfg.MySQL.DSN = " " }},
		{name: "Redis address", mutate: func(cfg *config.Config) { cfg.Redis.Addr = " " }},
		{name: "Redis database", mutate: func(cfg *config.Config) { cfg.Redis.DB = -1 }},
		{name: "ID generator", mutate: func(cfg *config.Config) { cfg.IDGen.Offset = 0 }},
		{name: "cookie name", mutate: func(cfg *config.Config) { cfg.Session.CookieName = "bad cookie" }},
		{name: "short session TTL", mutate: func(cfg *config.Config) { cfg.Session.TTL = 15*time.Minute - time.Nanosecond }},
		{name: "long session TTL", mutate: func(cfg *config.Config) { cfg.Session.TTL = 168*time.Hour + time.Nanosecond }},
		{name: "GFS base URL", mutate: func(cfg *config.Config) { cfg.GFS.BaseURL = "://gfs-url-secret" }},
		{name: "noncanonical GFS base URL", mutate: func(cfg *config.Config) { cfg.GFS.BaseURL += "/" }},
		{name: "GFS app ID", mutate: func(cfg *config.Config) { cfg.GFS.AppID = " " }},
		{name: "GFS app secret", mutate: func(cfg *config.Config) { cfg.GFS.AppSecret = " " }},
		{name: "GFS public read secret", mutate: func(cfg *config.Config) { cfg.GFS.PublicReadSecret = " " }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig()
			test.mutate(&cfg)

			router, err := Build(cfg, testDependencies(t, io.Discard))

			require.Error(t, err)
			require.Nil(t, router)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestBuildRecoversWithSanitizedProblemAndStructuredAccessLog(t *testing.T) {
	var logs bytes.Buffer
	deps := testDependencies(t, &logs)
	router, err := Build(testConfig(), deps)
	require.NoError(t, err)
	router.POST("/panic/:secret", func(*gin.Context) {
		panic("panic-password-secret")
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/panic/panic-path-secret?token=query-token-secret", strings.NewReader(`{"password":"body-password-secret"}`))
	request.Header.Set("X-Request-ID", "panic-test")
	request.Header.Set("Authorization", "Bearer authorization-secret")
	request.Header.Set("Cookie", "admin_session=cookie-secret")
	require.NotPanics(t, func() { router.ServeHTTP(response, request) })
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))

	var problem httpapi.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, "internal_error", problem.Code)
	require.Equal(t, "panic-test", problem.RequestId)

	logText := logs.String()
	for _, secret := range []string{
		"panic-password-secret",
		"panic-path-secret",
		"query-token-secret",
		"body-password-secret",
		"authorization-secret",
		"cookie-secret",
	} {
		require.NotContains(t, logText, secret)
	}

	entries := decodeLogEntries(t, logText)
	require.Len(t, entries, 2)
	require.Equal(t, "panic recovered", entries[0]["msg"])
	require.Equal(t, "/panic/:secret", entries[0]["path"])
	require.Equal(t, "panic-test", entries[0]["request_id"])
	require.Equal(t, "http request", entries[1]["msg"])
	require.Equal(t, "/panic/:secret", entries[1]["path"])
	require.Equal(t, "panic-test", entries[1]["request_id"])
	require.Equal(t, float64(http.StatusInternalServerError), entries[1]["status"])
	require.Contains(t, entries[1], "duration_ms")
}

func TestBuildAccessLogUsesRouteTemplatesNumericIDsAndRedactsSensitiveRequestData(t *testing.T) {
	var logs bytes.Buffer
	deps, mock, _ := testDependenciesWithResources(t, &logs)
	router, err := Build(testConfig(), deps)
	require.NoError(t, err)
	token := testAdminSessionToken()
	seedAdminSession(t, deps.Redis, token, deps.Now())

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, username, password_hash, state FROM admins WHERE id = ?")).
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "state"}).
			AddRow(int64(41), "admin.user", "database-password-hash-secret", "active"))
	article := httptest.NewRecorder()
	articleRequest := httptest.NewRequest(http.MethodPut, "/api/admin/v1/articles/11/draft", strings.NewReader(
		`{"lockVersion":1,"title":"title","summary":"","coverMediaId":null,"contentMd":"![x](/img/proxy/m_body-media-key-secret)","tagIds":[],"originalName":"filename-secret.png","password":"body-password-secret"}`,
	))
	articleRequest.Header.Set("Content-Type", "application/json")
	articleRequest.Header.Set("Origin", testConfig().HTTP.AdminOrigin)
	articleRequest.Header.Set("Referer", "https://qiuxs.com/preview?signature=signed-target-secret")
	articleRequest.Header.Set("Authorization", "Bearer authorization-secret")
	articleRequest.AddCookie(&http.Cookie{Name: testConfig().Session.CookieName, Value: token})
	router.ServeHTTP(article, articleRequest)
	require.Equal(t, http.StatusBadRequest, article.Code, article.Body.String())

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, allow_empty_referer FROM hotlink_settings WHERE singleton_key = 1")).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	public := httptest.NewRecorder()
	publicRequest := httptest.NewRequest(http.MethodGet, "/img/proxy/stable-key-secret?token=query-secret", nil)
	publicRequest.Header.Set("Referer", "https://qiuxs.com/preview?signature=signed-target-secret")
	router.ServeHTTP(public, publicRequest)
	require.Equal(t, http.StatusNotFound, public.Code, public.Body.String())
	unknown := httptest.NewRecorder()
	unknownRequest := httptest.NewRequest(http.MethodGet, "/missing/unknown-media-key-secret?token=unknown-query-secret", nil)
	router.ServeHTTP(unknown, unknownRequest)
	require.Equal(t, http.StatusNotFound, unknown.Code, unknown.Body.String())

	entries := decodeLogEntries(t, logs.String())
	require.Len(t, entries, 3)
	require.Equal(t, "/api/admin/v1/articles/:articleId/draft", entries[0]["path"])
	require.Equal(t, float64(41), entries[0]["admin_id"])
	require.Equal(t, float64(11), entries[0]["article_id"])
	require.Equal(t, "/img/proxy/:publicKey", entries[1]["path"])
	require.Equal(t, "<unmatched>", entries[2]["path"])
	for _, secret := range []string{
		token,
		"database-password-hash-secret",
		"m_body-media-key-secret",
		"filename-secret.png",
		"body-password-secret",
		"authorization-secret",
		"stable-key-secret",
		"query-secret",
		"signed-target-secret",
		"unknown-media-key-secret",
		"unknown-query-secret",
	} {
		require.NotContains(t, logs.String(), secret)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildTrustsNoForwardedClientIP(t *testing.T) {
	router := buildTestRouter(t)
	router.GET("/client-ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
	request.RemoteAddr = "127.0.0.1:54321"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "127.0.0.1", response.Body.String())
}

func buildTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	router, err := Build(testConfig(), testDependencies(t, io.Discard))
	require.NoError(t, err)
	return router
}

func testDependencies(t *testing.T, logOutput io.Writer) Dependencies {
	t.Helper()
	deps, _, _ := testDependenciesWithResources(t, logOutput)
	return deps
}

func testDependenciesWithResources(t *testing.T, logOutput io.Writer) (Dependencies, sqlmock.Sqlmock, *miniredis.Miniredis) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	miniRedis := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })

	return Dependencies{
		DB:     db,
		Redis:  redisClient,
		Logger: slog.New(slog.NewJSONHandler(logOutput, nil)),
		Random: bytes.NewReader(make([]byte, 256)),
		Now:    func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) },
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		JenkinsHTTPClient: &http.Client{Timeout: 5 * time.Second},
		ReleaseJSONReader: func() (io.ReadCloser, error) {
			return nil, fs.ErrNotExist
		},
	}, mock, miniRedis
}

var _ release.ArtifactReader = func() (io.ReadCloser, error) { return nil, fs.ErrNotExist }

func testConfig() config.Config {
	return config.Config{
		Environment: "development",
		HTTP: config.HTTPConfig{
			Addr:        ":8080",
			AdminOrigin: "http://admin.example.com",
		},
		MySQL: config.MySQLConfig{DSN: "blog:password@tcp(mysql:3306)/blog"},
		Redis: config.RedisConfig{Addr: "redis:6379"},
		IDGen: config.IDGenConfig{Offset: 1, Step: 1},
		Session: config.SessionConfig{
			CookieName:   "admin_session",
			CookieSecure: true,
			TTL:          time.Hour,
		},
		GFS: config.GFSConfig{
			BaseURL:          "http://gfs.example.com",
			AppID:            "blog-app",
			AppSecret:        "test-app-secret",
			PublicReadSecret: "test-public-read-secret",
		},
		Release: config.ReleaseConfig{
			BundleToken:            []byte(strings.Repeat("b", 32)),
			CallbackHMACKey:        []byte(strings.Repeat("h", 32)),
			BuilderMasterKey:       []byte(strings.Repeat("k", 32)),
			CurrentReleaseJSONPath: "/srv/blog/current/release.json",
		},
	}
}

func requireProblemCode(t *testing.T, response *httptest.ResponseRecorder, code string) {
	t.Helper()
	var problem httpapi.Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, code, problem.Code)
}

func testAdminSessionToken() string {
	return "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
}

func seedAdminSession(t *testing.T, client *redis.Client, token string, now time.Time) {
	t.Helper()
	digest := sha256.Sum256([]byte(token))
	key := "qiuxs-blog:session:" + hex.EncodeToString(digest[:])
	encoded, err := json.Marshal(auth.Session{AdminID: 41, Username: "admin.user", ExpiresAt: now.Add(time.Hour)})
	require.NoError(t, err)
	require.NoError(t, client.Set(context.Background(), key, encoded, time.Hour).Err())
}

func decodeLogEntries(t *testing.T, raw string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	entries := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		entries = append(entries, entry)
	}
	return entries
}
