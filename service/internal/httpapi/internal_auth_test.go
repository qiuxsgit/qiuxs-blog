package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/builder"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRequireBundleTokenRequiresOneExactBearerAndNeverUsesCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token := []byte(strings.Repeat("b", 32))
	tests := []struct {
		name          string
		authorization []string
		cookie        string
		wantStatus    int
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized},
		{name: "cookie only", cookie: "qx_blog_session=" + string(token), wantStatus: http.StatusUnauthorized},
		{name: "wrong", authorization: []string{"Bearer " + strings.Repeat("x", 32)}, wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", authorization: []string{"bearer " + string(token)}, wantStatus: http.StatusUnauthorized},
		{name: "missing space", authorization: []string{"Bearer" + string(token)}, wantStatus: http.StatusUnauthorized},
		{name: "multiple", authorization: []string{"Bearer " + string(token), "Bearer " + string(token)}, wantStatus: http.StatusUnauthorized},
		{name: "exact", authorization: []string{"Bearer " + string(token)}, wantStatus: http.StatusNoContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(RequestID())
			router.GET("/bundle", RequireBundleToken(token), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodGet, "/bundle", nil)
			request.Header.Set("X-Request-ID", "handler-request-42")
			for _, value := range test.authorization {
				request.Header.Add("Authorization", value)
			}
			if test.cookie != "" {
				request.Header.Set("Cookie", test.cookie)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, test.wantStatus, response.Code, response.Body.String())
			if test.wantStatus == http.StatusUnauthorized {
				requireProblemResponse(t, response, http.StatusUnauthorized, "internal_unauthorized")
				require.NotContains(t, response.Body.String(), string(token))
			}
		})
	}
}

func TestVerifyJenkinsCallbackReadsOneStrictBoundedJSONBodyAndStoresClaim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := builder.CallbackPayload{ReleaseID: 7, PublishJobID: 12, BuildNumber: 41, Stage: "build", Status: "building", Timestamp: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC), Nonce: "nonce_1234567890"}
	verifier := &callbackVerifierStub{payload: payload, duplicate: true}
	router := gin.New()
	router.Use(RequestID())
	router.POST("/callback", VerifyJenkinsCallback(verifier), func(c *gin.Context) {
		got, duplicate, ok := JenkinsCallbackFrom(c)
		require.True(t, ok)
		require.Equal(t, payload, got)
		require.True(t, duplicate)
		c.Status(http.StatusNoContent)
	})
	raw := []byte(`{"releaseId":7}`)
	request := httptest.NewRequest(http.MethodPost, "/callback", &oneReadBody{Reader: bytes.NewReader(raw)})
	request.Header.Set("X-Request-ID", "handler-request-42")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Jenkins-Signature", "sha256="+strings.Repeat("a", 64))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	require.Equal(t, 1, verifier.calls)
	require.Equal(t, raw, verifier.raw)
	require.Equal(t, "sha256="+strings.Repeat("a", 64), verifier.signature)
}

func TestVerifyJenkinsCallbackRejectsIngressBeforeVerifier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		contentTypes []string
		signatures   []string
		body         []byte
		query        string
		status       int
		code         string
	}{
		{name: "missing content type", signatures: []string{"sig"}, body: []byte(`{}`), status: 400, code: "invalid_request"},
		{name: "content type parameters", contentTypes: []string{"application/json; charset=utf-8"}, signatures: []string{"sig"}, body: []byte(`{}`), status: 400, code: "invalid_request"},
		{name: "multiple content types", contentTypes: []string{"application/json", "application/json"}, signatures: []string{"sig"}, body: []byte(`{}`), status: 400, code: "invalid_request"},
		{name: "missing signature", contentTypes: []string{"application/json"}, body: []byte(`{}`), status: 401, code: "internal_unauthorized"},
		{name: "multiple signatures", contentTypes: []string{"application/json"}, signatures: []string{"one", "two"}, body: []byte(`{}`), status: 401, code: "internal_unauthorized"},
		{name: "empty body", contentTypes: []string{"application/json"}, signatures: []string{"sig"}, status: 400, code: "invalid_request"},
		{name: "oversize body", contentTypes: []string{"application/json"}, signatures: []string{"sig"}, body: bytes.Repeat([]byte{'x'}, maxInternalCallbackBodyBytes+1), status: 400, code: "invalid_request"},
		{name: "unexpected query", contentTypes: []string{"application/json"}, signatures: []string{"sig"}, body: []byte(`{}`), query: "?x=1", status: 400, code: "invalid_request"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &callbackVerifierStub{}
			router := gin.New()
			router.Use(RequestID())
			router.POST("/callback", VerifyJenkinsCallback(verifier), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodPost, "/callback"+test.query, bytes.NewReader(test.body))
			request.Header.Set("X-Request-ID", "handler-request-42")
			for _, value := range test.contentTypes {
				request.Header.Add("Content-Type", value)
			}
			for _, value := range test.signatures {
				request.Header.Add("X-Jenkins-Signature", value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, test.status, response.Code, response.Body.String())
			requireProblemResponse(t, response, test.status, test.code)
			require.Zero(t, verifier.calls)
		})
	}
}

func TestVerifyJenkinsCallbackMapsVerifierErrorsWithoutSecrets(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "invalid", err: builder.ErrInvalidCallback, want: 400, code: "invalid_request"},
		{name: "unauthorized", err: builder.ErrCallbackUnauthorized, want: 401, code: "internal_unauthorized"},
		{name: "replay", err: builder.ErrCallbackReplay, want: 409, code: "callback_conflict"},
		{name: "dependency", err: redis.ErrClosed, want: 503, code: "dependency_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &callbackVerifierStub{err: errors.Join(test.err, errors.New("callback-signature-secret"))}
			router := gin.New()
			router.Use(RequestID())
			router.POST("/callback", VerifyJenkinsCallback(verifier), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(`{}`))
			request.Header.Set("X-Request-ID", "handler-request-42")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Jenkins-Signature", "signature-secret")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			requireProblemResponse(t, response, test.want, test.code)
			require.NotContains(t, response.Body.String(), "secret")
		})
	}
}

type callbackVerifierStub struct {
	payload   builder.CallbackPayload
	duplicate bool
	err       error
	calls     int
	raw       []byte
	signature string
}

func (v *callbackVerifierStub) VerifyAndClaim(_ context.Context, raw []byte, signature string) (builder.CallbackPayload, bool, error) {
	v.calls++
	v.raw = append([]byte(nil), raw...)
	v.signature = signature
	return v.payload, v.duplicate, v.err
}

type oneReadBody struct {
	*bytes.Reader
	reads int
}

func (b *oneReadBody) Read(target []byte) (int, error) {
	b.reads++
	if b.reads > 2 {
		return 0, errors.New("body read more than once")
	}
	return b.Reader.Read(target)
}

func (*oneReadBody) Close() error { return nil }
