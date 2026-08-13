package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/httpapi"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBuildRegistersPublicAndAdminRoutes(t *testing.T) {
	router := buildTestRouter(t)
	requireRoute(t, router, http.MethodGet, "/health/live")
	requireRoute(t, router, http.MethodGet, "/health/ready")
	requireRoute(t, router, http.MethodPost, "/api/admin/v1/session")
	requireRoute(t, router, http.MethodDelete, "/api/admin/v1/session")
	requireRoute(t, router, http.MethodGet, "/api/admin/v1/me")

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
	router.POST("/panic", func(*gin.Context) {
		panic("panic-password-secret")
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/panic?token=query-token-secret", strings.NewReader(`{"password":"body-password-secret"}`))
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
	require.Equal(t, "panic-test", entries[0]["request_id"])
	require.Equal(t, "http request", entries[1]["msg"])
	require.Equal(t, "panic-test", entries[1]["request_id"])
	require.Equal(t, float64(http.StatusInternalServerError), entries[1]["status"])
	require.Contains(t, entries[1], "duration_ms")
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
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	mock.ExpectClose()
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	miniRedis := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })

	return Dependencies{
		DB:     db,
		Redis:  redisClient,
		Logger: slog.New(slog.NewJSONHandler(logOutput, nil)),
		Random: bytes.NewReader(make([]byte, 256)),
		Now:    func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) },
	}
}

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
	}
}

func requireRoute(t *testing.T, router *gin.Engine, method, path string) {
	t.Helper()
	for _, route := range router.Routes() {
		if route.Method == method && route.Path == path {
			return
		}
	}
	t.Fatalf("route %s %s was not registered", method, path)
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
