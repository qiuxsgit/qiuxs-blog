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
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/stretchr/testify/require"
)

type handlerRepositoryFake struct {
	admin       auth.Admin
	findErr     error
	updateErr   error
	findCalls   int
	updateCalls int
}

func (r *handlerRepositoryFake) Count(context.Context) (int, error) { return 0, nil }
func (r *handlerRepositoryFake) Create(context.Context, string, string) (auth.Admin, error) {
	return auth.Admin{}, errors.New("not implemented")
}
func (r *handlerRepositoryFake) FindByUsername(context.Context, string) (auth.Admin, error) {
	r.findCalls++
	return r.admin, r.findErr
}
func (r *handlerRepositoryFake) FindByID(context.Context, int64) (auth.Admin, error) {
	return r.admin, r.findErr
}
func (r *handlerRepositoryFake) UpdateLastLogin(context.Context, int64, time.Time) error {
	r.updateCalls++
	return r.updateErr
}

type handlerSessionStoreFake struct {
	sessions  map[string]auth.Session
	setErr    error
	getErr    error
	deleteErr error
}

func (s *handlerSessionStoreFake) Set(_ context.Context, digest string, session auth.Session, _ time.Duration) error {
	if s.setErr != nil {
		return s.setErr
	}
	if s.sessions == nil {
		s.sessions = make(map[string]auth.Session)
	}
	s.sessions[digest] = session
	return nil
}
func (s *handlerSessionStoreFake) Get(_ context.Context, digest string) (auth.Session, error) {
	if s.getErr != nil {
		return auth.Session{}, s.getErr
	}
	session, ok := s.sessions[digest]
	if !ok {
		return auth.Session{}, auth.ErrSessionNotFound
	}
	return session, nil
}
func (s *handlerSessionStoreFake) Delete(_ context.Context, digest string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.sessions, digest)
	return nil
}

type handlerLimiterFake struct {
	decision auth.LimitDecision
	allowErr error
	ip       string
}

func (l *handlerLimiterFake) Allow(_ context.Context, _, ip string) (auth.LimitDecision, error) {
	l.ip = ip
	return l.decision, l.allowErr
}
func (l *handlerLimiterFake) RecordFailure(context.Context, string, string) error { return nil }
func (l *handlerLimiterFake) ResetUsername(context.Context, string) error         { return nil }

type handlerTestSystem struct {
	router  *gin.Engine
	service auth.Service
	repo    *handlerRepositoryFake
	store   *handlerSessionStoreFake
	limiter *handlerLimiterFake
	now     time.Time
	config  config.SessionConfig
}

func newHandlerTestSystem(t *testing.T) handlerTestSystem {
	t.Helper()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	hasher := auth.DefaultPasswordHasher()
	passwordHash, err := hasher.Hash("correct-password")
	require.NoError(t, err)

	repo := &handlerRepositoryFake{admin: auth.Admin{ID: 42, Username: "admin.user", PasswordHash: passwordHash, State: "active"}}
	store := &handlerSessionStoreFake{sessions: make(map[string]auth.Session)}
	limiter := &handlerLimiterFake{decision: auth.LimitDecision{Allowed: true}}
	sessionConfig := config.SessionConfig{CookieName: "qx_blog_session", CookieSecure: true, TTL: 90 * time.Minute}
	sessions := auth.NewSessionManager(store, sessionConfig.TTL, strings.NewReader(string(bytesFromZeroTo31ForHandler())), func() time.Time { return now })
	service := auth.NewService(repo, hasher, sessions, limiter, func() time.Time { return now })
	handler := NewAuthHandler(service, sessionConfig)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID(), LoadAdminSession(service, sessionConfig.CookieName))
	RegisterAuthHandlers(router, handler)
	return handlerTestSystem{router: router, service: service, repo: repo, store: store, limiter: limiter, now: now, config: sessionConfig}
}

func TestRegisterAuthHandlersUsesExactGeneratedAuthRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterAuthHandlers(router, NewAuthHandler(auth.Service{}, config.SessionConfig{CookieName: "qx_blog_session", TTL: time.Hour}))

	routes := router.Routes()
	actual := make([]string, len(routes))
	for index, route := range routes {
		actual[index] = route.Method + " " + route.Path
	}
	require.ElementsMatch(t, []string{
		http.MethodPost + " /api/admin/v1/session",
		http.MethodDelete + " /api/admin/v1/session",
		http.MethodGet + " /api/admin/v1/me",
	}, actual)
}

func TestAuthHandlerLoginRejectsUnknownFieldsTrailingJSONAndWrongContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "unknown field", contentType: "application/json", body: `{"username":"admin.user","password":"correct-password","extra":true}`},
		{name: "trailing value", contentType: "application/json", body: `{"username":"admin.user","password":"correct-password"}{}`},
		{name: "wrong content type", contentType: "text/plain", body: `{"username":"admin.user","password":"correct-password"}`},
		{name: "missing content type", body: `{"username":"admin.user","password":"correct-password"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			system := newHandlerTestSystem(t)
			recorder := performHandlerRequest(system.router, http.MethodPost, "/api/admin/v1/session", tt.contentType, tt.body, nil)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Zero(t, system.repo.findCalls)
			requireProblemResponse(t, recorder, http.StatusBadRequest, "invalid_request")
		})
	}
}

func TestAuthHandlerLoginRejectsInvalidUTF8RawJSONBeforeService(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{"object key", append(append([]byte(`{"user`), 0xff), []byte(`name":"admin.user","password":"x"}`)...)},
		{"username", append(append([]byte(`{"username":"adm`), 0xff), []byte(`in","password":"x"}`)...)},
		{"password", append(append([]byte(`{"username":"admin.user","password":"x`), 0xff), []byte(`"}`)...)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			system := newHandlerTestSystem(t)
			response := performRawHandlerRequest(system.router, http.MethodPost, "/api/admin/v1/session", "application/json", test.body)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			requireProblemResponse(t, response, http.StatusBadRequest, "invalid_request")
			require.Zero(t, system.repo.findCalls)
			require.Empty(t, system.limiter.ip)
			require.Empty(t, system.store.sessions)
		})
	}
}

func TestAuthHandlerLoginRequiresExactUniqueStringProperties(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "capitalized username", body: `{"Username":"admin.user","password":"correct-password"}`},
		{name: "mixed-case username", body: `{"userName":"admin.user","password":"correct-password"}`},
		{name: "capitalized password", body: `{"username":"admin.user","Password":"correct-password"}`},
		{name: "duplicate username", body: `{"username":"admin.user","username":"admin.user","password":"correct-password"}`},
		{name: "duplicate password", body: `{"username":"admin.user","password":"correct-password","password":"correct-password"}`},
		{name: "unknown property", body: `{"username":"admin.user","password":"correct-password","unknown":"value"}`},
		{name: "numeric username", body: `{"username":42,"password":"correct-password"}`},
		{name: "null password", body: `{"username":"admin.user","password":null}`},
		{name: "missing username", body: `{"password":"correct-password"}`},
		{name: "missing password", body: `{"username":"admin.user"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			system := newHandlerTestSystem(t)

			recorder := performHandlerRequest(system.router, http.MethodPost, "/api/admin/v1/session", "application/json", tt.body, nil)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Zero(t, system.repo.findCalls)
			require.Empty(t, system.store.sessions)
			requireProblemResponse(t, recorder, http.StatusBadRequest, "invalid_request")
		})
	}
}

func TestAuthHandlerLoginAcceptsExactly16KiBAndRejectsLargerBody(t *testing.T) {
	const base = `{"username":"admin.user","password":"correct-password"}`
	atLimit := base + strings.Repeat(" ", 16*1024-len(base))
	require.Len(t, atLimit, 16*1024)

	successSystem := newHandlerTestSystem(t)
	success := performHandlerRequest(successSystem.router, http.MethodPost, "/api/admin/v1/session", "application/json", atLimit, nil)
	require.Equal(t, http.StatusOK, success.Code)

	overLimitSystem := newHandlerTestSystem(t)
	overLimit := performHandlerRequest(overLimitSystem.router, http.MethodPost, "/api/admin/v1/session", "application/json", atLimit+" ", nil)
	require.Equal(t, http.StatusBadRequest, overLimit.Code)
	require.Zero(t, overLimitSystem.repo.findCalls)
	requireProblemResponse(t, overLimit, http.StatusBadRequest, "invalid_request")
}

func TestAuthHandlerLoginAllowsJSONCharsetAndSetsHardenedHostOnlyCookie(t *testing.T) {
	system := newHandlerTestSystem(t)
	recorder := performHandlerRequest(
		system.router,
		http.MethodPost,
		"/api/admin/v1/session",
		"application/json; charset=utf-8",
		`{"username":"admin.user","password":"correct-password"}`,
		nil,
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	require.JSONEq(t, `{"id":42,"username":"admin.user"}`, recorder.Body.String())
	var responseObject map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &responseObject))
	require.Len(t, responseObject, 2)
	require.Equal(t, "192.0.2.1", system.limiter.ip)

	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]
	require.Equal(t, system.config.CookieName, cookie.Name)
	require.NotEmpty(t, cookie.Value)
	require.Equal(t, "/api/admin/v1", cookie.Path)
	require.Empty(t, cookie.Domain)
	require.True(t, cookie.HttpOnly)
	require.True(t, cookie.Secure)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.Equal(t, 5400, cookie.MaxAge)
	require.Equal(t, system.now.Add(system.config.TTL), cookie.Expires)
	require.NotContains(t, recorder.Header().Get("Set-Cookie"), "Domain=")
}

func TestAuthHandlerLoginReturnsSchemaValidRateLimitProblemAndCeilingRetryAfter(t *testing.T) {
	system := newHandlerTestSystem(t)
	system.limiter.decision = auth.LimitDecision{Allowed: false, RetryAfter: 1501 * time.Millisecond}

	recorder := performHandlerRequest(
		system.router,
		http.MethodPost,
		"/api/admin/v1/session",
		"application/json",
		`{"username":"admin.user","password":"correct-password"}`,
		nil,
	)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "2", recorder.Header().Get("Retry-After"))
	require.Zero(t, system.repo.findCalls)
	requireProblemResponse(t, recorder, http.StatusTooManyRequests, "login_rate_limited")
}

func TestAuthHandlerRejectsInvalidCookieConfigurationBeforeCreatingSession(t *testing.T) {
	system := newHandlerTestSystem(t)
	invalidConfig := system.config
	invalidConfig.CookieName = "invalid;cookie"
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID(), LoadAdminSession(system.service, system.config.CookieName))
	RegisterAuthHandlers(router, NewAuthHandler(system.service, invalidConfig))

	recorder := performHandlerRequest(
		router,
		http.MethodPost,
		"/api/admin/v1/session",
		"application/json",
		`{"username":"admin.user","password":"correct-password"}`,
		nil,
	)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Zero(t, system.repo.findCalls)
	require.Zero(t, system.repo.updateCalls)
	require.Empty(t, system.store.sessions)
	require.Empty(t, recorder.Header().Values("Set-Cookie"))
	requireProblemResponse(t, recorder, http.StatusInternalServerError, "internal_error")
}

func TestAuthHandlerLogoutClearsCookieAndReturns204WhenSessionAbsent(t *testing.T) {
	system := newHandlerTestSystem(t)

	recorder := performHandlerRequest(system.router, http.MethodDelete, "/api/admin/v1/session", "", "", nil)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Empty(t, recorder.Body.String())
	require.Contains(t, recorder.Header().Get("Set-Cookie"), "Max-Age=0")
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]
	require.Equal(t, system.config.CookieName, cookie.Name)
	require.Empty(t, cookie.Value)
	require.Equal(t, "/api/admin/v1", cookie.Path)
	require.Empty(t, cookie.Domain)
	require.True(t, cookie.HttpOnly)
	require.True(t, cookie.Secure)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.Less(t, cookie.Expires, system.now)
}

func TestAuthHandlerGetCurrentAdminReturnsContextAdminAndRequiresAuthentication(t *testing.T) {
	system := newHandlerTestSystem(t)
	system.router = gin.New()
	system.router.Use(RequestID(), func(c *gin.Context) {
		c.Set(sessionStateKey, sessionState{admin: auth.Admin{ID: 42, Username: "context-name", PasswordHash: "must-not-leak", State: "active"}})
		c.Next()
	})
	RegisterAuthHandlers(system.router, NewAuthHandler(auth.Service{}, system.config))

	recorder := performHandlerRequest(system.router, http.MethodGet, "/api/admin/v1/me", "", "", nil)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"id":42,"username":"context-name"}`, recorder.Body.String())

	anonymous := newHandlerTestSystem(t)
	unauthorized := performHandlerRequest(anonymous.router, http.MethodGet, "/api/admin/v1/me", "", "", nil)
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code)
	requireProblemResponse(t, unauthorized, http.StatusUnauthorized, "unauthenticated")
}

func performHandlerRequest(router http.Handler, method, path, contentType, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("X-Request-ID", "handler-request-42")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func requireProblemResponse(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
	var raw any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &raw))
	spec, err := GetSpec()
	require.NoError(t, err)
	require.NoError(t, spec.Components.Schemas["Problem"].Value.VisitJSON(raw))

	encoded, err := json.Marshal(raw)
	require.NoError(t, err)
	var problem Problem
	require.NoError(t, json.NewDecoder(bytes.NewReader(encoded)).Decode(&problem))
	require.Equal(t, status, problem.Status)
	require.Equal(t, code, problem.Code)
	require.Equal(t, "https://qiuxs.com/problems/"+code, problem.Type)
	require.Equal(t, "handler-request-42", problem.RequestId)
	require.NotContains(t, recorder.Body.String(), "secret")
}

func bytesFromZeroTo31ForHandler() []byte {
	bytes := make([]byte, 32)
	for index := range bytes {
		bytes[index] = byte(index)
	}
	return bytes
}
