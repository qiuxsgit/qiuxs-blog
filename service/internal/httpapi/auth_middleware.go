package httpapi

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
)

const sessionStateKey = "admin_session_state"

type sessionState struct {
	admin auth.Admin
	err   error
}

// LoadAdminSession optionally resolves the configured Session cookie. Invalid,
// expired, and absent Sessions remain anonymous; operational failures are kept
// in context so protected endpoints can return a dependency Problem.
func LoadAdminSession(service auth.Service, cookieName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		state := sessionState{}
		token, err := c.Cookie(cookieName)
		if err == nil && token != "" {
			admin, currentErr := service.Current(c.Request.Context(), token)
			switch {
			case currentErr == nil:
				admin.PasswordHash = ""
				state.admin = admin
			case errors.Is(currentErr, auth.ErrUnauthenticated):
			default:
				state.err = currentErr
			}
		}
		c.Set(sessionStateKey, state)
		c.Next()
	}
}

// RequireAdmin rejects anonymous requests without redirecting. A Session-load
// dependency failure takes precedence over the anonymous result.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := requireAdmin(c); !ok {
			return
		}
		c.Next()
	}
}

func requireAdmin(c *gin.Context) (auth.Admin, bool) {
	if err := SessionLoadErrorFrom(c); err != nil {
		WriteProblem(c, err)
		c.Abort()
		return auth.Admin{}, false
	}
	admin, ok := AdminFrom(c)
	if !ok {
		WriteProblem(c, auth.ErrUnauthenticated)
		c.Abort()
		return auth.Admin{}, false
	}
	return admin, true
}

func AdminFrom(c *gin.Context) (auth.Admin, bool) {
	state, ok := sessionStateFrom(c)
	if !ok || state.err != nil || state.admin.ID <= 0 || state.admin.State != "active" {
		return auth.Admin{}, false
	}
	state.admin.PasswordHash = ""
	return state.admin, true
}

func SessionLoadErrorFrom(c *gin.Context) error {
	state, ok := sessionStateFrom(c)
	if !ok {
		return nil
	}
	return state.err
}

func sessionStateFrom(c *gin.Context) (sessionState, bool) {
	value, ok := c.Get(sessionStateKey)
	if !ok {
		return sessionState{}, false
	}
	state, ok := value.(sessionState)
	return state, ok
}
