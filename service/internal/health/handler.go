package health

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Checker interface {
	Check(context.Context) error
}

type CheckFunc func(context.Context) error

func (f CheckFunc) Check(ctx context.Context) error {
	return f(ctx)
}

func RegisterRoutes(router gin.IRouter, mysql, redis Checker) {
	router.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/health/ready", func(c *gin.Context) {
		ctx := c.Request.Context()
		if mysql.Check(ctx) != nil || redis.Check(ctx) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}
