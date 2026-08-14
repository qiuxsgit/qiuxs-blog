package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/release"
)

const (
	adminIDLogContextKey            = "admin_id"
	articleIDLogContextKey          = "article_id"
	releaseIDLogContextKey          = "release_id"
	publishJobIDLogContextKey       = "publish_job_id"
	jenkinsBuildNumberLogContextKey = "jenkins_build_number"
	resultLogContextKey             = "result"
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

func setReleaseLogContext(c *gin.Context, releaseID, publishJobID, buildNumber int64, result release.JobStatus) {
	if c == nil {
		return
	}
	if releaseID > 0 {
		c.Set(releaseIDLogContextKey, releaseID)
	}
	if publishJobID > 0 {
		c.Set(publishJobIDLogContextKey, publishJobID)
	}
	if buildNumber > 0 {
		c.Set(jenkinsBuildNumberLogContextKey, buildNumber)
	}
	if validReleaseLogResult(result) {
		c.Set(resultLogContextKey, string(result))
	}
}

func AdminIDFromLogContext(c *gin.Context) (int64, bool) {
	return numericLogContext(c, adminIDLogContextKey)
}

func ArticleIDFromLogContext(c *gin.Context) (int64, bool) {
	return numericLogContext(c, articleIDLogContextKey)
}

func ReleaseIDFromLogContext(c *gin.Context) (int64, bool) {
	return numericLogContext(c, releaseIDLogContextKey)
}

func PublishJobIDFromLogContext(c *gin.Context) (int64, bool) {
	return numericLogContext(c, publishJobIDLogContextKey)
}

func JenkinsBuildNumberFromLogContext(c *gin.Context) (int64, bool) {
	return numericLogContext(c, jenkinsBuildNumberLogContextKey)
}

func ResultFromLogContext(c *gin.Context) (string, bool) {
	if c == nil {
		return "", false
	}
	value, exists := c.Get(resultLogContextKey)
	result, ok := value.(string)
	return result, exists && ok && validReleaseLogResult(release.JobStatus(result))
}

func numericLogContext(c *gin.Context, key string) (int64, bool) {
	if c == nil {
		return 0, false
	}
	value, exists := c.Get(key)
	id, ok := value.(int64)
	return id, exists && ok && id > 0
}

func validReleaseLogResult(result release.JobStatus) bool {
	switch result {
	case release.JobPending, release.JobQueued, release.JobBuilding, release.JobDeploying, release.JobSuccess, release.JobFailed:
		return true
	default:
		return false
	}
}
