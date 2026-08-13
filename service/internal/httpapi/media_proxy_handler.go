package httpapi

import (
	"errors"
	"net/http"
	"reflect"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
)

type MediaProxyHandler struct {
	service media.ProxyService
}

func NewMediaProxyHandler(service media.ProxyService) (*MediaProxyHandler, error) {
	if nilMediaProxyService(service) {
		return nil, errors.New("media proxy service is required")
	}
	return &MediaProxyHandler{service: service}, nil
}

func RegisterMediaProxy(router gin.IRoutes, handler *MediaProxyHandler) {
	router.GET("/img/proxy/:publicKey", handler.Get)
}

func (h *MediaProxyHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h == nil || nilMediaProxyService(h.service) {
		WriteProblem(c, media.ErrDependencyUnavailable)
		return
	}
	target, err := h.service.Redirect(c.Request.Context(), c.Param("publicKey"), c.GetHeader("Referer"))
	if err != nil {
		WriteProblem(c, err)
		return
	}
	c.Header("Location", target)
	c.Status(http.StatusFound)
}

func nilMediaProxyService(service media.ProxyService) bool {
	if service == nil {
		return true
	}
	value := reflect.ValueOf(service)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
