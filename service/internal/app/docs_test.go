package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStage2Documentation(t *testing.T) {
	serviceGuide := readDocumentation(t, filepath.Join("..", "..", "README.md"))
	rootGuide := readDocumentation(t, filepath.Join("..", "..", "..", "README.md"))

	t.Run("service operations", func(t *testing.T) {
		requireDocumentationContains(t, serviceGuide, []string{
			"Go 1.25.7",
			"BLOG_GFS_BASE_URL",
			"BLOG_GFS_APP_ID",
			"BLOG_GFS_APP_SECRET",
			"BLOG_GFS_PUBLIC_READ_SECRET",
			"f9b82569ffc5fca078053fd3fe048517fa61ab77",
			"bcf87257e7425a6397c079aa9c9994eccbbf3aaa",
			"dedicated private GFS bucket and application",
			"blog/{{year}}/{{month}}/{{uuid}}.{{fileExt}}",
			"exactly 60 seconds",
			"10 MiB",
			"12000",
			"image/jpeg",
			"image/png",
			"image/webp",
			"image/gif",
			"12-character lowercase URL-safe random values",
			"22 lowercase URL-safe random",
			"200-rune title",
			"600-rune summary",
			"2 MiB Markdown body",
			"32 tags",
			"256 unique",
			"Tag display names permit 64",
			"At most 16 ordered social",
			"GET /alioss/objects/{fileId}/metadata",
			"302 or 307",
			"Cache-Control: no-store",
			"/img/proxy/{publicKey}",
			"/api/admin/v1",
			"`POST` | `/api/admin/v1/session`",
			"`DELETE` | `/api/admin/v1/session`",
			"`GET` | `/api/admin/v1/me`",
			"`GET`, `POST` | `/api/admin/v1/articles`",
			"`GET` | `/api/admin/v1/articles/{articleId}`",
			"`PUT` | `/api/admin/v1/articles/{articleId}/draft`",
			"`GET` | `/api/admin/v1/articles/{articleId}/preview`",
			"`GET`, `POST` | `/api/admin/v1/articles/{articleId}/versions`",
			"`POST` | `/api/admin/v1/articles/{articleId}/versions/{revisionId}/restore`",
			"`POST` | `/api/admin/v1/articles/{articleId}/trash`",
			"`POST` | `/api/admin/v1/articles/{articleId}/untrash`",
			"`GET`, `POST` | `/api/admin/v1/tags`",
			"`PATCH` | `/api/admin/v1/tags/{tagId}`",
			"`POST` | `/api/admin/v1/media/upload-policy`",
			"`POST` | `/api/admin/v1/media`",
			"`GET`, `PUT` | `/api/admin/v1/settings/site`",
			"`GET`, `PUT` | `/api/admin/v1/settings/hotlink`",
			"`GET` | `/health/live`",
			"`GET` | `/health/ready`",
			"`GET` | `/img/proxy/{publicKey}`",
			"sqls/develop/develop.sql",
			"manually from top to bottom, preserving",
			"There is no automatic migration",
			"before starting `blog-service`",
			"configured session",
			"BLOG_SESSION_COOKIE_NAME` (default `qx_blog_session`)",
			"qx_blog_session",
			"https://beian.miit.gov.cn/",
			"长安休息室",
			"浙ICP备17057726号-1",
			"ValidatePublishable",
			"Stage 3",
			"make generate",
			"go test ./...",
			"go test -race ./internal/...",
			"go test ./tests/flow/... -v",
			"GOARCH=amd64 make build",
			"go run ./cmd/blog-service",
			"/health/live",
			"/health/ready",
			"docs/contracts/gfs-blog-media.md",
			"docs/superpowers/plans/2026-08-13-service-content-media.md",
		})
		require.NotContains(t, serviceGuide, "run migrations automatically")
		require.NotContains(t, serviceGuide, "blog-migrate")
	})

	t.Run("secret handling", func(t *testing.T) {
		requireDocumentationContains(t, serviceGuide, []string{
			"raw GFS application secret",
			"MD5 digest",
			"must never be logged",
			"read signatures locally",
			"does not proxy image bytes",
		})
	})

	t.Run("root scope and references", func(t *testing.T) {
		requireDocumentationContains(t, rootGuide, []string{
			"service/README.md",
			"docs/contracts/gfs-blog-media.md",
			"docs/superpowers/plans/2026-08-13-service-content-media.md",
			"docs/superpowers/specs/2026-08-13-qiuxs-blog-design.md",
			"docs/superpowers/plans/2026-08-13-qiuxs-blog-roadmap.md",
			"Stage 2",
		})
		require.NotContains(t, rootGuide, "后续博客管理与公开内容阶段尚未实现")
		require.NotContains(t, strings.ToLower(rootGuide), "later blog-management apis are not implemented")
	})
}

func readDocumentation(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}

func requireDocumentationContains(t *testing.T, contents string, fragments []string) {
	t.Helper()
	for _, fragment := range fragments {
		require.Containsf(t, contents, fragment, "documentation must contain %q", fragment)
	}
}
