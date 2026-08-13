package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLiveAlwaysReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, CheckFunc(func(context.Context) error { return errors.New("database is down") }), CheckFunc(func(context.Context) error { return errors.New("redis is down") }))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"status":"ok"}`, recorder.Body.String())
}

func TestReadyReturnsOKWhenAllDependenciesPass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, CheckFunc(func(context.Context) error { return nil }), CheckFunc(func(context.Context) error { return nil }))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"status":"ok"}`, recorder.Body.String())
}

func TestReadyHidesDependencyFailureDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routerFailure := errors.New("mysql credentials leaked")
	RegisterRoutes(router, CheckFunc(func(context.Context) error { return routerFailure }), CheckFunc(func(context.Context) error { return nil }))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.JSONEq(t, `{"status":"unavailable"}`, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), routerFailure.Error())
}
