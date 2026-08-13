package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRequestIDPreservesValidIncomingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID())
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"requestId": RequestIDFrom(c)})
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "request-42.alpha")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "request-42.alpha", recorder.Header().Get("X-Request-ID"))
	require.JSONEq(t, `{"requestId":"request-42.alpha"}`, recorder.Body.String())
}

func TestRequestIDCreatesUUIDForMissingOrInvalidHeader(t *testing.T) {
	for _, header := range []string{"", "invalid request id"} {
		t.Run(header, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(RequestID())
			router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("X-Request-ID", header)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			requestID := recorder.Header().Get("X-Request-ID")
			require.NoError(t, uuid.Validate(requestID))
		})
	}
}
