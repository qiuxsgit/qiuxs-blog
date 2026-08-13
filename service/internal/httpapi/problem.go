package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
)

const problemContentType = "application/problem+json"

var (
	ErrInvalidRequest  = errors.New("invalid request")
	ErrOriginForbidden = errors.New("origin forbidden")
	ErrNotFound        = errors.New("not found")
)

// WriteProblem renders a stable, non-sensitive RFC 9457-style response.
func WriteProblem(c *gin.Context, err error) {
	status, code, title := problemMapping(err)

	var rateLimitErr auth.RateLimitError
	if errors.As(err, &rateLimitErr) {
		seconds := retryAfterSeconds(rateLimitErr.RetryAfter)
		c.Header("Retry-After", strconv.FormatInt(seconds, 10))
	}

	c.Header("Content-Type", problemContentType)
	c.JSON(status, Problem{
		Type:      "https://qiuxs.com/problems/" + code,
		Title:     title,
		Status:    status,
		Code:      code,
		RequestId: RequestIDFrom(c),
	})
}

func problemMapping(err error) (int, string, string) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return http.StatusBadRequest, "invalid_request", "Invalid request"
	case errors.Is(err, auth.ErrInvalidCredentials):
		return http.StatusUnauthorized, "invalid_credentials", "Invalid credentials"
	case errors.Is(err, auth.ErrUnauthenticated):
		return http.StatusUnauthorized, "unauthenticated", "Unauthenticated"
	case errors.Is(err, ErrOriginForbidden):
		return http.StatusForbidden, "origin_forbidden", "Origin forbidden"
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, "not_found", "Not found"
	case errors.Is(err, auth.ErrRateLimited):
		return http.StatusTooManyRequests, "login_rate_limited", "Login rate limited"
	case errors.Is(err, auth.ErrDependencyUnavailable):
		return http.StatusServiceUnavailable, "dependency_unavailable", "Dependency unavailable"
	default:
		return http.StatusInternalServerError, "internal_error", "Internal error"
	}
}

func retryAfterSeconds(retryAfter time.Duration) int64 {
	if retryAfter <= 0 {
		return 0
	}
	seconds := int64(retryAfter / time.Second)
	if retryAfter%time.Second != 0 {
		seconds++
	}
	return seconds
}
