package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/stretchr/testify/require"
)

const testProxyPublicKey = "m_aaaaaaaaaaaaaaaaaaaaaa"

func TestMediaProxyRouteReturnsEmptyNoStoreRedirectAndForwardsRawReferer(t *testing.T) {
	target := "https://gfs.example.com/read/signed-policy?signature=required-signature"
	service := &mediaProxyServiceFake{target: target}
	router := newMediaProxyRouter(t, service)
	request := httptest.NewRequest(http.MethodGet, "/img/proxy/"+testProxyPublicKey, nil)
	request.Header.Set("X-Request-ID", "handler-request-42")
	request.Header.Set("Referer", "https://qiuxs.com:8443/raw/path?q=1#fragment")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, target, recorder.Header().Get("Location"))
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "handler-request-42", recorder.Header().Get("X-Request-ID"))
	require.Empty(t, recorder.Body.Bytes(), "redirect response must not contain Gin/net/http's default anchor body")
	require.Equal(t, []mediaProxyCall{{publicKey: testProxyPublicKey, referer: "https://qiuxs.com:8443/raw/path?q=1#fragment"}}, service.calls)
}

func TestMediaProxyRouteMapsSanitizedProblemsWithNoStoreAndRequestID(t *testing.T) {
	secretTarget := "https://gfs-secret.example/read/policy?signature=signature-secret"
	for _, test := range []struct {
		name   string
		key    string
		err    error
		status int
		code   string
	}{
		{name: "forbidden", key: testProxyPublicKey, err: fmt.Errorf("forbidden-secret: %w", media.ErrHotlinkForbidden), status: http.StatusForbidden, code: "hotlink_forbidden"},
		{name: "missing or inactive", key: testProxyPublicKey, err: fmt.Errorf("missing-secret: %w", media.ErrNotFound), status: http.StatusNotFound, code: "not_found"},
		{name: "malformed public key", key: "malformed-secret", err: media.ErrNotFound, status: http.StatusNotFound, code: "not_found"},
		{name: "dependency", key: testProxyPublicKey, err: fmt.Errorf("%s: %w", secretTarget, media.ErrDependencyUnavailable), status: http.StatusServiceUnavailable, code: "dependency_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &mediaProxyServiceFake{err: test.err}
			router := newMediaProxyRouter(t, service)
			request := httptest.NewRequest(http.MethodGet, "/img/proxy/"+test.key, nil)
			request.Header.Set("X-Request-ID", "handler-request-42")
			request.Header.Set("Referer", "https://referer-secret.example/image.png")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.Empty(t, recorder.Header().Get("Location"))
			require.Equal(t, "handler-request-42", recorder.Header().Get("X-Request-ID"))
			requireProblemResponse(t, recorder, test.status, test.code)
			require.NotContains(t, recorder.Body.String(), secretTarget)
			require.NotContains(t, recorder.Body.String(), "referer-secret")
			require.Equal(t, []mediaProxyCall{{publicKey: test.key, referer: "https://referer-secret.example/image.png"}}, service.calls)
		})
	}
}

func TestMediaProxyRouteGivesDependencyFailurePrecedenceOverWrappedDomainCause(t *testing.T) {
	for _, test := range []struct {
		name       string
		authorizer media.HotlinkAuthorizer
		finder     *proxyFinderFake
		signer     *proxySignerFake
	}{
		{name: "authorizer wraps forbidden", authorizer: &proxyAuthorizerFake{err: fmt.Errorf("policy-secret: %w", media.ErrHotlinkForbidden)}, finder: &proxyFinderFake{}, signer: &proxySignerFake{}},
		{name: "signer returns not found", authorizer: &proxyAuthorizerFake{allowed: true}, finder: &proxyFinderFake{item: media.Media{PublicKey: testProxyPublicKey, GFSFileID: 91, State: "active"}}, signer: &proxySignerFake{err: fmt.Errorf("signer-secret: %w", media.ErrNotFound)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			proxy, err := media.NewProxyService(test.authorizer, test.finder, test.signer, time.Now)
			require.NoError(t, err)
			router := newMediaProxyRouter(t, proxy)
			request := httptest.NewRequest(http.MethodGet, "/img/proxy/"+testProxyPublicKey, nil)
			request.Header.Set("X-Request-ID", "handler-request-42")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			requireProblemResponse(t, recorder, http.StatusServiceUnavailable, "dependency_unavailable")
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		})
	}
}

func TestMediaProxyHandlerRejectsNilServicesAndNilReceiverWithoutPanic(t *testing.T) {
	var typedNilService *mediaProxyServiceFake
	for _, test := range []struct {
		name    string
		service media.ProxyService
	}{
		{name: "nil service"},
		{name: "typed nil service", service: typedNilService},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, err := NewMediaProxyHandler(test.service)
			require.Nil(t, handler)
			require.Error(t, err)
		})
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID())
	var nilHandler *MediaProxyHandler
	router.GET("/img/proxy/:publicKey", nilHandler.Get)
	request := httptest.NewRequest(http.MethodGet, "/img/proxy/"+testProxyPublicKey, nil)
	request.Header.Set("X-Request-ID", "handler-request-42")
	recorder := httptest.NewRecorder()

	require.NotPanics(t, func() { router.ServeHTTP(recorder, request) })
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	requireProblemResponse(t, recorder, http.StatusServiceUnavailable, "dependency_unavailable")
}

type mediaProxyCall struct {
	publicKey string
	referer   string
}

type mediaProxyServiceFake struct {
	target string
	err    error
	calls  []mediaProxyCall
}

type proxyAuthorizerFake struct {
	allowed bool
	err     error
}

func (a *proxyAuthorizerFake) AllowsCurrentReferer(context.Context, string) (bool, error) {
	return a.allowed, a.err
}

type proxyFinderFake struct {
	item media.Media
	err  error
}

func (f *proxyFinderFake) FindActiveByPublicKey(context.Context, string) (media.Media, error) {
	return f.item, f.err
}

type proxySignerFake struct {
	target string
	err    error
}

func (s *proxySignerFake) ReadURL(media.Media, time.Time) (string, error) {
	return s.target, s.err
}

func (s *mediaProxyServiceFake) Redirect(_ context.Context, publicKey, referer string) (string, error) {
	s.calls = append(s.calls, mediaProxyCall{publicKey: publicKey, referer: referer})
	return s.target, s.err
}

func newMediaProxyRouter(t *testing.T, service media.ProxyService) http.Handler {
	t.Helper()
	handler, err := NewMediaProxyHandler(service)
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID())
	RegisterMediaProxy(router, handler)
	return router
}
