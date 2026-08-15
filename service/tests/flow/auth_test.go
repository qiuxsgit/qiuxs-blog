package flow_test

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/app"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/bootstrap"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/httpapi"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

const (
	adminUsername     = "qiuxs"
	adminPassword     = "correct-horse-battery-staple"
	adminOrigin       = "https://admin.example.com"
	selectAdminPrefix = `SELECT id, username, password_hash, state FROM admins`
)

func TestFirstAdminThroughLogoutFlow(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	miniRedis := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })

	cfg := flowConfig(miniRedis.Addr())
	now := time.Date(2026, 8, 13, 12, 34, 56, 0, time.UTC)
	ids, err := idgen.New(idgen.NewRedisCounter(redisClient), db, cfg.IDGen.Offset, cfg.IDGen.Step, cfg.IDGen.Heal)
	require.NoError(t, err)
	repository := auth.NewMySQLRepository(db, ids)

	var storedPasswordHash string
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM admins`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`INSERT INTO admins \(id, singleton_key, username, password_hash, state\) VALUES \(\?, \?, \?, \?, \?\)`).
		WithArgs(int64(1), 1, adminUsername, captureString{target: &storedPasswordHash}, "active").
		WillReturnResult(sqlmock.NewResult(0, 1))

	admin, err := bootstrap.CreateFirstAdmin(context.Background(), repository, auth.DefaultPasswordHasher(), adminUsername, adminPassword)
	require.NoError(t, err)
	require.Equal(t, int64(1), admin.ID)
	require.Equal(t, adminUsername, admin.Username)
	require.NotEmpty(t, storedPasswordHash)
	adminSequence, err := miniRedis.Get("idseq:admins")
	require.NoError(t, err)
	require.Equal(t, "1", adminSequence)

	router, err := app.Build(cfg, app.Dependencies{
		DB:     db,
		Redis:  redisClient,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 128)),
		Now:    func() time.Time { return now },
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		JenkinsHTTPClient: &http.Client{Timeout: 5 * time.Second},
		ReleaseJSONReader: func() (io.ReadCloser, error) { return nil, fs.ErrNotExist },
	})
	require.NoError(t, err)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()

	response := doJSON(t, client, http.MethodPost, server.URL+"/api/admin/v1/session", "", nil, map[string]string{
		"username": adminUsername,
		"password": adminPassword,
	})
	requireProblem(t, response, http.StatusForbidden, "origin_forbidden")

	mock.ExpectQuery(selectAdminPrefix + ` WHERE username = \?`).
		WithArgs(adminUsername).
		WillReturnRows(adminRows(storedPasswordHash))
	response = doJSON(t, client, http.MethodPost, server.URL+"/api/admin/v1/session", adminOrigin, nil, map[string]string{
		"username": adminUsername,
		"password": "wrong-password",
	})
	requireProblem(t, response, http.StatusUnauthorized, "invalid_credentials")
	assertLimiterIncremented(t, miniRedis)

	mock.ExpectQuery(selectAdminPrefix + ` WHERE username = \?`).
		WithArgs(adminUsername).
		WillReturnRows(adminRows(storedPasswordHash))
	mock.ExpectExec(`UPDATE admins SET last_login_at = \? WHERE id = \?`).
		WithArgs(now.UTC(), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	response = doJSON(t, client, http.MethodPost, server.URL+"/api/admin/v1/session", adminOrigin, nil, map[string]string{
		"username": adminUsername,
		"password": adminPassword,
	})
	require.Equal(t, http.StatusOK, response.StatusCode)
	var loginView httpapi.AdminView
	decodeResponse(t, response, &loginView)
	require.Equal(t, httpapi.AdminView{Id: 1, Username: adminUsername}, loginView)

	cookies := response.Cookies()
	require.Len(t, cookies, 1)
	sessionCookie := cookies[0]
	require.Equal(t, cfg.Session.CookieName, sessionCookie.Name)
	require.NotEmpty(t, sessionCookie.Value)
	require.Empty(t, sessionCookie.Domain)
	require.True(t, sessionCookie.Secure)
	require.True(t, sessionCookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, sessionCookie.SameSite)
	require.Equal(t, "/api/admin/v1", sessionCookie.Path)

	mock.ExpectQuery(selectAdminPrefix + ` WHERE id = \?`).
		WithArgs(int64(1)).
		WillReturnRows(adminRows(storedPasswordHash))
	response = doJSON(t, client, http.MethodGet, server.URL+"/api/admin/v1/me", "", sessionCookie, nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var currentAdmin httpapi.AdminView
	decodeResponse(t, response, &currentAdmin)
	require.Equal(t, loginView, currentAdmin)

	mock.ExpectQuery(selectAdminPrefix + ` WHERE id = \?`).
		WithArgs(int64(1)).
		WillReturnRows(adminRows(storedPasswordHash))
	response = doJSON(t, client, http.MethodDelete, server.URL+"/api/admin/v1/session", adminOrigin, sessionCookie, nil)
	require.Equal(t, http.StatusNoContent, response.StatusCode)
	require.NoError(t, response.Body.Close())

	response = doJSON(t, client, http.MethodGet, server.URL+"/api/admin/v1/me", "", sessionCookie, nil)
	requireProblem(t, response, http.StatusUnauthorized, "unauthenticated")

	mock.ExpectPing()
	response = doJSON(t, client, http.MethodGet, server.URL+"/health/ready", "", nil, nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var readiness map[string]string
	decodeResponse(t, response, &readiness)
	require.Equal(t, map[string]string{"status": "ok"}, readiness)

	require.NoError(t, mock.ExpectationsWereMet())
}

type captureString struct {
	target *string
}

func (capture captureString) Match(value driver.Value) bool {
	text, ok := value.(string)
	if !ok || !strings.HasPrefix(text, "$argon2id$") {
		return false
	}
	*capture.target = text
	return true
}

func flowConfig(redisAddr string) config.Config {
	return config.Config{
		Environment: "production",
		HTTP: config.HTTPConfig{
			Addr:        ":8080",
			AdminOrigin: adminOrigin,
		},
		MySQL: config.MySQLConfig{Host: "sqlmock", Port: 3306, User: "test", Database: "test", Args: "parseTime=true&loc=UTC&charset=utf8mb4"},
		Redis: config.RedisConfig{Addr: redisAddr},
		IDGen: config.IDGenConfig{Offset: 1, Step: 1, Heal: false},
		Session: config.SessionConfig{
			CookieName:   "qx_blog_session",
			CookieSecure: true,
			TTL:          time.Hour,
		},
		GFS: config.GFSConfig{
			BaseURL:          "https://gfs.example.com",
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

func adminRows(passwordHash string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "username", "password_hash", "state"}).
		AddRow(int64(1), adminUsername, passwordHash, "active")
}

func doJSON(t *testing.T, client *http.Client, method, url, origin string, cookie *http.Cookie, body any) *http.Response {
	t.Helper()
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, requestBody)
	require.NoError(t, err)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response, err := client.Do(request)
	require.NoError(t, err)
	return response
}

func requireProblem(t *testing.T, response *http.Response, status int, code string) {
	t.Helper()
	require.Equal(t, status, response.StatusCode)
	require.Equal(t, "application/problem+json", response.Header.Get("Content-Type"))
	var problem httpapi.Problem
	decodeResponse(t, response, &problem)
	require.Equal(t, status, problem.Status)
	require.Equal(t, code, problem.Code)
	require.NotEmpty(t, problem.RequestId)
}

func decodeResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.NoError(t, json.NewDecoder(response.Body).Decode(target))
}

func withoutRedirects(client *http.Client) *http.Client {
	cloned := *client
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &cloned
}

func assertLimiterIncremented(t *testing.T, redisServer *miniredis.Miniredis) {
	t.Helper()
	var limiterKeys []string
	for _, key := range redisServer.Keys() {
		if strings.HasPrefix(key, "qiuxs-blog:login:") {
			limiterKeys = append(limiterKeys, key)
		}
	}
	require.Len(t, limiterKeys, 2)
	for _, key := range limiterKeys {
		count, err := redisServer.Get(key)
		require.NoError(t, err)
		require.Equal(t, "1", count)
	}
}
