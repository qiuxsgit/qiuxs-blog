package httpapi

import (
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
	"github.com/stretchr/testify/require"
)

const testSessionToken = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

type middlewareRepositoryFake struct {
	admin   auth.Admin
	findErr error
	findID  int64
}

func (r *middlewareRepositoryFake) Count(context.Context) (int, error) { return 0, nil }
func (r *middlewareRepositoryFake) Create(context.Context, string, string) (auth.Admin, error) {
	return auth.Admin{}, errors.New("not implemented")
}
func (r *middlewareRepositoryFake) FindByUsername(context.Context, string) (auth.Admin, error) {
	return auth.Admin{}, errors.New("not implemented")
}
func (r *middlewareRepositoryFake) FindByID(_ context.Context, id int64) (auth.Admin, error) {
	r.findID = id
	return r.admin, r.findErr
}
func (r *middlewareRepositoryFake) UpdateLastLogin(context.Context, int64, time.Time) error {
	return errors.New("not implemented")
}

type middlewareSessionStoreFake struct {
	session auth.Session
	getErr  error
}

func (s *middlewareSessionStoreFake) Set(context.Context, string, auth.Session, time.Duration) error {
	return errors.New("not implemented")
}
func (s *middlewareSessionStoreFake) Get(context.Context, string) (auth.Session, error) {
	return s.session, s.getErr
}
func (s *middlewareSessionStoreFake) Delete(context.Context, string) error { return nil }

type middlewareLimiterFake struct{}

func (middlewareLimiterFake) Allow(context.Context, string, string) (auth.LimitDecision, error) {
	return auth.LimitDecision{Allowed: true}, nil
}
func (middlewareLimiterFake) RecordFailure(context.Context, string, string) error { return nil }
func (middlewareLimiterFake) ResetUsername(context.Context, string) error         { return nil }

func middlewareService(now time.Time, repo auth.Repository, store auth.SessionStore) auth.Service {
	sessions := auth.NewSessionManager(store, time.Hour, strings.NewReader(strings.Repeat("x", 32)), func() time.Time { return now })
	return auth.NewService(repo, auth.DefaultPasswordHasher(), sessions, middlewareLimiterFake{}, func() time.Time { return now })
}

func TestLoadAdminSessionAttachesRefetchedActiveAdmin(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	repo := &middlewareRepositoryFake{admin: auth.Admin{ID: 42, Username: "current-name", PasswordHash: "must-not-leak", State: "active"}}
	store := &middlewareSessionStoreFake{session: auth.Session{AdminID: 42, Username: "stale-name", ExpiresAt: now.Add(time.Hour)}}
	service := middlewareService(now, repo, store)
	recorder := serveSessionMiddleware(service, testSessionToken, false, func(c *gin.Context) {
		admin, ok := AdminFrom(c)
		require.True(t, ok)
		require.Empty(t, admin.PasswordHash)
		c.JSON(http.StatusOK, AdminView{Id: admin.ID, Username: admin.Username})
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(42), repo.findID)
	require.JSONEq(t, `{"id":42,"username":"current-name"}`, recorder.Body.String())
}

func TestLoadAdminSessionLeavesMissingMalformedAndExpiredCookiesAnonymous(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		token   string
		session auth.Session
		getErr  error
	}{
		{name: "missing"},
		{name: "malformed", token: "malformed"},
		{name: "expired", token: testSessionToken, session: auth.Session{AdminID: 42, Username: "admin.user", ExpiresAt: now}},
		{name: "absent", token: testSessionToken, getErr: auth.ErrSessionNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &middlewareRepositoryFake{}
			store := &middlewareSessionStoreFake{session: tt.session, getErr: tt.getErr}
			service := middlewareService(now, repo, store)

			recorder := serveSessionMiddleware(service, tt.token, false, func(c *gin.Context) {
				_, authenticated := AdminFrom(c)
				require.False(t, authenticated)
				require.NoError(t, SessionLoadErrorFrom(c))
				c.Status(http.StatusNoContent)
			})

			require.Equal(t, http.StatusNoContent, recorder.Code)
			require.Zero(t, repo.findID)
		})
	}
}

func TestRequireAdminReturnsProblemForAnonymousSession(t *testing.T) {
	now := time.Now()
	service := middlewareService(now, &middlewareRepositoryFake{}, &middlewareSessionStoreFake{getErr: auth.ErrSessionNotFound})

	recorder := serveSessionMiddleware(service, testSessionToken, true, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
	var problem Problem
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &problem))
	require.Equal(t, "unauthenticated", problem.Code)
	require.Equal(t, "https://qiuxs.com/problems/unauthenticated", problem.Type)
	require.Equal(t, "session-request-42", problem.RequestId)
}

func TestRequireAdminSurfacesOperationalSessionFailure(t *testing.T) {
	now := time.Now()
	service := middlewareService(now, &middlewareRepositoryFake{}, &middlewareSessionStoreFake{getErr: errors.New("redis password secret")})

	recorder := serveSessionMiddleware(service, testSessionToken, true, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
	require.NotContains(t, recorder.Body.String(), "secret")
	var problem Problem
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &problem))
	require.Equal(t, "dependency_unavailable", problem.Code)
	require.Equal(t, "https://qiuxs.com/problems/dependency_unavailable", problem.Type)
}

func serveSessionMiddleware(service auth.Service, token string, required bool, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID(), LoadAdminSession(service, "qx_blog_session"))
	if required {
		router.Use(RequireAdmin())
	}
	router.GET("/", handler)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "session-request-42")
	if token != "" {
		request.AddCookie(&http.Cookie{Name: "qx_blog_session", Value: token})
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
