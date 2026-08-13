package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/article"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

const problemContentType = "application/problem+json"

var (
	ErrInvalidRequest        = errors.New("invalid request")
	ErrOriginForbidden       = errors.New("origin forbidden")
	ErrNotFound              = errors.New("not found")
	ErrDependencyUnavailable = errors.New("dependency unavailable")
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
	case errors.Is(err, media.ErrDependencyUnavailable), errors.Is(err, ErrDependencyUnavailable):
		return http.StatusServiceUnavailable, "dependency_unavailable", "Dependency unavailable"
	case errors.Is(err, revision.ErrConflict):
		return http.StatusConflict, "revision_conflict", "Revision conflict"
	case errors.Is(err, settings.ErrConflict):
		return http.StatusConflict, "settings_conflict", "Settings conflict"
	case errors.Is(err, article.ErrMustBeUnpublished):
		return http.StatusConflict, "article_must_be_unpublished", "Article must be unpublished"
	case errors.Is(err, article.ErrStateConflict), errors.Is(err, article.ErrSlugConflict), errors.Is(err, revision.ErrArticleInactive):
		return http.StatusConflict, "article_state_conflict", "Article state conflict"
	case errors.Is(err, tag.ErrNameConflict), errors.Is(err, tag.ErrSlugConflict):
		return http.StatusConflict, "tag_conflict", "Tag conflict"
	case errors.Is(err, media.ErrPublicKeyConflict), errors.Is(err, media.ErrGFSFileIDConflict):
		return http.StatusConflict, "media_conflict", "Media conflict"
	case errors.Is(err, revision.ErrInvalidContent), errors.Is(err, revision.ErrNotFrozen):
		return http.StatusUnprocessableEntity, "invalid_content", "Invalid content"
	case errors.Is(err, media.ErrInvalidMetadata):
		return http.StatusUnprocessableEntity, "invalid_media", "Invalid media"
	case errors.Is(err, settings.ErrInvalid):
		return http.StatusUnprocessableEntity, "invalid_settings", "Invalid settings"
	case errors.Is(err, tag.ErrInvalidName), errors.Is(err, tag.ErrInvalidSelection):
		return http.StatusBadRequest, "invalid_request", "Invalid request"
	case errors.Is(err, media.ErrHotlinkForbidden):
		return http.StatusForbidden, "hotlink_forbidden", "Hotlink forbidden"
	case errors.Is(err, ErrNotFound), errors.Is(err, media.ErrNotFound), errors.Is(err, article.ErrNotFound), errors.Is(err, revision.ErrNotFound), errors.Is(err, tag.ErrNotFound):
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
