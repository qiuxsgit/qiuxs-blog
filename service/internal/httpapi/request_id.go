package httpapi

import (
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDKey = "request_id"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if !validRequestID.MatchString(requestID) {
			requestID = uuid.NewString()
		}

		c.Set(requestIDKey, requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func RequestIDFrom(c *gin.Context) string {
	requestID, _ := c.Get(requestIDKey)
	value, _ := requestID.(string)
	return value
}
