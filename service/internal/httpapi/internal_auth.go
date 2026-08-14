package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/builder"
)

const (
	maxInternalCallbackBodyBytes = 16 * 1024
	jenkinsCallbackContextKey    = "jenkins_callback_claim"
)

type jenkinsCallbackVerifier interface {
	VerifyAndClaim(context.Context, []byte, string) (builder.CallbackPayload, bool, error)
}

type jenkinsCallbackClaim struct {
	payload   builder.CallbackPayload
	duplicate bool
}

// RequireBundleToken authenticates only the Internal Bundle route. Admin
// cookies are deliberately ignored and exactly one Bearer value is required.
func RequireBundleToken(token []byte) gin.HandlerFunc {
	expected := append([]byte(nil), token...)
	return func(c *gin.Context) {
		values := c.Request.Header.Values("Authorization")
		if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(values[0], "Bearer ")), expected) != 1 {
			WriteProblem(c, ErrInternalUnauthorized)
			c.Abort()
			return
		}
		c.Next()
	}
}

// VerifyJenkinsCallback owns the callback body. It verifies one exact JSON
// content type and one signature, reads the bounded stream once, and stores the
// authenticated claim for the generated handler without decoding it again.
func VerifyJenkinsCallback(verifier jenkinsCallbackVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if nilAdminDependency(verifier) {
			WriteProblem(c, ErrDependencyUnavailable)
			c.Abort()
			return
		}
		if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
			WriteProblem(c, ErrInvalidRequest)
			c.Abort()
			return
		}
		if contentTypes := c.Request.Header.Values("Content-Type"); len(contentTypes) != 1 || contentTypes[0] != "application/json" {
			WriteProblem(c, ErrInvalidRequest)
			c.Abort()
			return
		}
		signatures := c.Request.Header.Values("X-Jenkins-Signature")
		if len(signatures) != 1 || signatures[0] == "" {
			WriteProblem(c, ErrInternalUnauthorized)
			c.Abort()
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxInternalCallbackBodyBytes)
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil || len(raw) == 0 {
			WriteProblem(c, ErrInvalidRequest)
			c.Abort()
			return
		}
		payload, duplicate, err := verifier.VerifyAndClaim(c.Request.Context(), raw, signatures[0])
		if err != nil {
			writeCallbackProblem(c, err)
			c.Abort()
			return
		}
		c.Set(jenkinsCallbackContextKey, jenkinsCallbackClaim{payload: payload, duplicate: duplicate})
		c.Next()
	}
}

func JenkinsCallbackFrom(c *gin.Context) (builder.CallbackPayload, bool, bool) {
	if c == nil {
		return builder.CallbackPayload{}, false, false
	}
	value, exists := c.Get(jenkinsCallbackContextKey)
	claim, ok := value.(jenkinsCallbackClaim)
	return claim.payload, claim.duplicate, exists && ok
}

func writeCallbackProblem(c *gin.Context, err error) {
	switch {
	case errors.Is(err, builder.ErrInvalidCallback):
		WriteProblem(c, ErrInvalidRequest)
	case errors.Is(err, builder.ErrCallbackUnauthorized):
		WriteProblem(c, ErrInternalUnauthorized)
	case errors.Is(err, builder.ErrCallbackReplay):
		WriteProblem(c, builder.ErrCallbackReplay)
	default:
		WriteProblem(c, ErrDependencyUnavailable)
	}
}
