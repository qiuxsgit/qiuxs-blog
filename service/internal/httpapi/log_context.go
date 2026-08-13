package httpapi

import "github.com/gin-gonic/gin"

const (
	adminIDLogContextKey   = "admin_id"
	articleIDLogContextKey = "article_id"
)

func setAdminLogContext(c *gin.Context, id int64) {
	if c != nil && id > 0 {
		c.Set(adminIDLogContextKey, id)
	}
}

func setArticleLogContext(c *gin.Context, id int64) {
	if c != nil && id > 0 {
		c.Set(articleIDLogContextKey, id)
	}
}

func AdminIDFromLogContext(c *gin.Context) (int64, bool) {
	return numericLogContext(c, adminIDLogContextKey)
}

func ArticleIDFromLogContext(c *gin.Context) (int64, bool) {
	return numericLogContext(c, articleIDLogContextKey)
}

func numericLogContext(c *gin.Context, key string) (int64, bool) {
	if c == nil {
		return 0, false
	}
	value, exists := c.Get(key)
	id, ok := value.(int64)
	return id, exists && ok && id > 0
}
