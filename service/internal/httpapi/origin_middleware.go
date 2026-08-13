package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// OriginGuard requires unsafe requests to carry the single exact configured
// administrator origin. It deliberately emits no CORS response headers.
func OriginGuard(adminOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requiresOrigin(c.Request.Method) {
			c.Next()
			return
		}

		origins := c.Request.Header.Values("Origin")
		if len(origins) != 1 || adminOrigin == "" || origins[0] != adminOrigin {
			WriteProblem(c, ErrOriginForbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}

func requiresOrigin(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
