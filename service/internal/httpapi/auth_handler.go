package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
)

const (
	maxLoginBodyBytes = 16 * 1024
	adminCookiePath   = "/api/admin/v1"
)

type AuthHandler struct {
	service auth.Service
	session config.SessionConfig
	initErr error
}

func NewAuthHandler(service auth.Service, session config.SessionConfig) *AuthHandler {
	handler := &AuthHandler{service: service, session: session}
	if err := config.ValidateSessionCookieName(session.CookieName); err != nil {
		handler.initErr = auth.ErrInternal
	}
	return handler
}

func (h *AuthHandler) LoginAdmin(c *gin.Context) {
	if !h.valid(c) {
		return
	}
	request, err := decodeLoginRequest(c)
	if err != nil {
		WriteProblem(c, ErrInvalidRequest)
		return
	}

	result, err := h.service.Login(c.Request.Context(), request.Username, request.Password, c.ClientIP())
	if err != nil {
		WriteProblem(c, err)
		return
	}

	h.setSessionCookie(c, result.Token, result.Session.ExpiresAt)
	c.JSON(http.StatusOK, AdminView{Id: result.Admin.ID, Username: result.Admin.Username})
}

func (h *AuthHandler) LogoutAdmin(c *gin.Context) {
	if !h.valid(c) {
		return
	}
	token, err := c.Cookie(h.session.CookieName)
	if err == nil && token != "" {
		if err := h.service.Logout(c.Request.Context(), token); err != nil {
			WriteProblem(c, err)
			return
		}
	}

	h.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

func (h *AuthHandler) GetCurrentAdmin(c *gin.Context) {
	if !h.valid(c) {
		return
	}
	admin, ok := requireAdmin(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, AdminView{Id: admin.ID, Username: admin.Username})
}

func (h *AuthHandler) valid(c *gin.Context) bool {
	if h == nil || h.initErr != nil {
		WriteProblem(c, auth.ErrInternal)
		return false
	}
	return true
}

func decodeLoginRequest(c *gin.Context) (LoginRequest, error) {
	contentTypes := c.Request.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return LoginRequest{}, ErrInvalidRequest
	}
	mediaType, params, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || !strings.EqualFold(mediaType, "application/json") || !validJSONMediaTypeParams(params) {
		return LoginRequest{}, ErrInvalidRequest
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxLoginBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	request, err := decodeLoginObject(decoder)
	if err != nil {
		return LoginRequest{}, ErrInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return LoginRequest{}, ErrInvalidRequest
	}
	if !validLoginRequest(request) {
		return LoginRequest{}, ErrInvalidRequest
	}
	return request, nil
}

func decodeLoginObject(decoder *json.Decoder) (LoginRequest, error) {
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return LoginRequest{}, ErrInvalidRequest
	}

	request := LoginRequest{}
	seen := map[string]bool{}
	for decoder.More() {
		propertyToken, err := decoder.Token()
		if err != nil {
			return LoginRequest{}, ErrInvalidRequest
		}
		property, ok := propertyToken.(string)
		if !ok || (property != "username" && property != "password") || seen[property] {
			return LoginRequest{}, ErrInvalidRequest
		}

		var value any
		if err := decoder.Decode(&value); err != nil {
			return LoginRequest{}, ErrInvalidRequest
		}
		stringValue, ok := value.(string)
		if !ok {
			return LoginRequest{}, ErrInvalidRequest
		}

		seen[property] = true
		switch property {
		case "username":
			request.Username = stringValue
		case "password":
			request.Password = stringValue
		}
	}

	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !seen["username"] || !seen["password"] {
		return LoginRequest{}, ErrInvalidRequest
	}
	return request, nil
}

func validJSONMediaTypeParams(params map[string]string) bool {
	if len(params) == 0 {
		return true
	}
	charset, ok := params["charset"]
	return len(params) == 1 && ok && strings.EqualFold(charset, "utf-8")
}

func validLoginRequest(request LoginRequest) bool {
	usernameLength := utf8.RuneCountInString(request.Username)
	return usernameLength >= 1 && usernameLength <= 64 &&
		len(request.Password) >= 1 && len(request.Password) <= 256
}

func (h *AuthHandler) setSessionCookie(c *gin.Context, token string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     h.session.CookieName,
		Value:    token,
		Path:     adminCookiePath,
		Expires:  expiresAt.UTC(),
		MaxAge:   cookieMaxAge(h.session.TTL),
		Secure:   h.session.CookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *AuthHandler) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     h.session.CookieName,
		Value:    "",
		Path:     adminCookiePath,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		Secure:   h.session.CookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func cookieMaxAge(ttl time.Duration) int {
	if ttl <= 0 {
		return 0
	}
	seconds := int(ttl / time.Second)
	if ttl%time.Second != 0 {
		seconds++
	}
	return seconds
}
