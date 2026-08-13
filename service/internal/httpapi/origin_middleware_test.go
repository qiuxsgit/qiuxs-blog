package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const testAdminOrigin = "https://blog-admin.qiuxs.com"

func TestOriginGuardAllowsSafeMethodsWithoutOrigin(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			recorder := serveOriginRequest(method, nil)

			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}

func TestOriginGuardRequiresExactConfiguredOriginForUnsafeMethods(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			recorder := serveOriginRequest(method, []string{testAdminOrigin})

			require.Equal(t, http.StatusNoContent, recorder.Code)
			require.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}

func TestOriginGuardRejectsMissingMalformedAndNonExactOrigins(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
	}{
		{name: "missing"},
		{name: "null", origins: []string{"null"}},
		{name: "malformed", origins: []string{"://not-an-origin"}},
		{name: "subdomain", origins: []string{"https://evil.blog-admin.qiuxs.com"}},
		{name: "different port", origins: []string{"https://blog-admin.qiuxs.com:444"}},
		{name: "http", origins: []string{"http://blog-admin.qiuxs.com"}},
		{name: "path", origins: []string{"https://blog-admin.qiuxs.com/path"}},
		{name: "multiple", origins: []string{testAdminOrigin, testAdminOrigin}},
		{name: "comma combined", origins: []string{testAdminOrigin + ", " + testAdminOrigin}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := serveOriginRequest(http.MethodPost, tt.origins)

			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
			require.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))

			var problem Problem
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &problem))
			require.Equal(t, Problem{
				Type:      "https://qiuxs.com/problems/origin_forbidden",
				Title:     "Origin forbidden",
				Status:    http.StatusForbidden,
				Code:      "origin_forbidden",
				RequestId: "origin-request-42",
			}, problem)
		})
	}
}

func serveOriginRequest(method string, origins []string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID(), OriginGuard(testAdminOrigin))
	router.Handle(method, "/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := httptest.NewRequest(method, "/", nil)
	request.Header.Set("X-Request-ID", "origin-request-42")
	for _, origin := range origins {
		request.Header.Add("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
