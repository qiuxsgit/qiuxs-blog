package httpapi

import (
	"errors"
	"reflect"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/article"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

// AdminHandler is the single generated Admin API implementation. Authentication
// endpoints delegate to AuthHandler; every content endpoint authenticates before
// decoding or invoking a domain service.
type AdminHandler struct {
	auth      *AuthHandler
	articles  article.Service
	revisions revision.Service
	tags      tag.Service
	media     media.Service
	site      settings.SiteService
	hotlink   settings.HotlinkService
	releases  *ReleaseHandler
}

func NewAdminHandler(
	authHandler *AuthHandler,
	articles article.Service,
	revisions revision.Service,
	tags tag.Service,
	mediaService media.Service,
	site settings.SiteService,
	hotlink settings.HotlinkService,
	releases *ReleaseHandler,
) (*AdminHandler, error) {
	switch {
	case authHandler == nil || authHandler.initErr != nil:
		return nil, errors.New("auth handler is required")
	case nilAdminDependency(articles):
		return nil, errors.New("article service is required")
	case nilAdminDependency(revisions):
		return nil, errors.New("revision service is required")
	case nilAdminDependency(tags):
		return nil, errors.New("tag service is required")
	case nilAdminDependency(mediaService):
		return nil, errors.New("media service is required")
	case nilAdminDependency(site):
		return nil, errors.New("site settings service is required")
	case nilAdminDependency(hotlink):
		return nil, errors.New("hotlink settings service is required")
	case releases == nil:
		return nil, errors.New("release handler is required")
	}
	return &AdminHandler{
		auth: authHandler, articles: articles, revisions: revisions, tags: tags,
		media: mediaService, site: site, hotlink: hotlink, releases: releases,
	}, nil
}

// RegisterAuthHandlers keeps the pre-Stage-2 application composition source
// compatible until Task 12 wires the complete AdminHandler. The paths and
// methods intentionally mirror the generated contract exactly.
func RegisterAuthHandlers(router gin.IRouter, handler *AuthHandler) {
	router.POST("/api/admin/v1/session", handler.LoginAdmin)
	router.DELETE("/api/admin/v1/session", handler.LogoutAdmin)
	router.GET("/api/admin/v1/me", handler.GetCurrentAdmin)
}

// RegisterAdminHandlers is the only supported registrar for the complete Admin
// API. Protected route middleware runs before generated parameter binding, and
// generated binding failures are always rendered as sanitized Problems.
func RegisterAdminHandlers(router gin.IRouter, handler *AdminHandler) {
	registerAdminHandlersWithErrorHandler(router, handler, adminGeneratedErrorHandler)
}

func registerAdminHandlersWithErrorHandler(router gin.IRouter, handler *AdminHandler, errorHandler func(*gin.Context, error, int)) {
	wrapper := &ServerInterfaceWrapper{Handler: handler, ErrorHandler: errorHandler}
	protected := requireAdminBeforeBinding()

	// Existing authentication operations retain their original behavior.
	router.DELETE("/api/admin/v1/session", wrapper.LogoutAdmin)
	router.POST("/api/admin/v1/session", wrapper.LoginAdmin)
	router.GET("/api/admin/v1/me", wrapper.GetCurrentAdmin)

	// RequireAdmin is a Gin route handler here, so it runs before each generated
	// wrapper can bind any attacker-controlled path or query value.
	router.GET("/api/admin/v1/articles", protected, wrapper.ListArticles)
	router.POST("/api/admin/v1/articles", protected, wrapper.CreateArticle)
	router.GET("/api/admin/v1/articles/:articleId", protected, wrapper.GetArticle)
	router.PUT("/api/admin/v1/articles/:articleId/draft", protected, wrapper.SaveArticleDraft)
	router.GET("/api/admin/v1/articles/:articleId/preview", protected, wrapper.GetArticlePreview)
	router.GET("/api/admin/v1/articles/:articleId/versions", protected, wrapper.ListArticleVersions)
	router.POST("/api/admin/v1/articles/:articleId/versions", protected, wrapper.CreateArticleVersion)
	router.POST("/api/admin/v1/articles/:articleId/versions/:revisionId/restore", protected, wrapper.RestoreArticleVersion)
	router.POST("/api/admin/v1/articles/:articleId/trash", protected, wrapper.TrashArticle)
	router.POST("/api/admin/v1/articles/:articleId/untrash", protected, wrapper.UntrashArticle)
	router.GET("/api/admin/v1/tags", protected, wrapper.ListTags)
	router.POST("/api/admin/v1/tags", protected, wrapper.CreateTag)
	router.PATCH("/api/admin/v1/tags/:tagId", protected, wrapper.RenameTag)
	router.POST("/api/admin/v1/media/upload-policy", protected, wrapper.CreateMediaUploadPolicy)
	router.POST("/api/admin/v1/media", protected, wrapper.RegisterMedia)
	router.GET("/api/admin/v1/settings/site", protected, wrapper.GetSiteSettings)
	router.PUT("/api/admin/v1/settings/site", protected, wrapper.PutSiteSettings)
	router.GET("/api/admin/v1/settings/hotlink", protected, wrapper.GetHotlinkSettings)
	router.PUT("/api/admin/v1/settings/hotlink", protected, wrapper.PutHotlinkSettings)
	router.GET("/api/admin/v1/builder", protected, wrapper.GetBuilderConfig)
	router.PUT("/api/admin/v1/builder", protected, wrapper.PutBuilderConfig)
	router.POST("/api/admin/v1/builder/test", protected, wrapper.TestBuilderConfig)
	router.GET("/api/admin/v1/releases", protected, wrapper.ListReleases)
	router.POST("/api/admin/v1/releases", protected, wrapper.CreateRelease)
	router.GET("/api/admin/v1/releases/:releaseId", protected, wrapper.GetRelease)
	router.POST("/api/admin/v1/releases/:releaseId/retry", protected, wrapper.RetryRelease)
}

// RegisterInternalReleaseHandlers deliberately uses the root router. Bundle
// Bearer and callback HMAC authentication are isolated from Origin and Admin
// cookie middleware, and execute before generated path binding.
func RegisterInternalReleaseHandlers(router gin.IRouter, handler *AdminHandler, bundleToken []byte, verifier jenkinsCallbackVerifier) {
	wrapper := &ServerInterfaceWrapper{Handler: handler, ErrorHandler: adminGeneratedErrorHandler}
	router.GET("/api/internal/v1/releases/:releaseId/bundle", RequireBundleToken(bundleToken), wrapper.GetReleaseBundle)
	router.POST("/api/internal/v1/jenkins/callback", VerifyJenkinsCallback(verifier), wrapper.AcceptJenkinsCallback)
}

func requireAdminBeforeBinding() gin.HandlerFunc {
	return func(c *gin.Context) {
		admin, ok := requireAdmin(c)
		if !ok {
			return
		}
		setAdminLogContext(c, admin.ID)
		c.Next()
	}
}

func (h *AdminHandler) LoginAdmin(c *gin.Context) {
	if h == nil || h.auth == nil {
		WriteProblem(c, auth.ErrInternal)
		return
	}
	h.auth.LoginAdmin(c)
}

func (h *AdminHandler) LogoutAdmin(c *gin.Context) {
	if h == nil || h.auth == nil {
		WriteProblem(c, auth.ErrInternal)
		return
	}
	h.auth.LogoutAdmin(c)
}

func (h *AdminHandler) GetCurrentAdmin(c *gin.Context) {
	if h == nil || h.auth == nil {
		WriteProblem(c, auth.ErrInternal)
		return
	}
	h.auth.GetCurrentAdmin(c)
}

func (h *AdminHandler) GetBuilderConfig(c *gin.Context) {
	if h == nil || h.releases == nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	h.releases.GetBuilderConfig(c)
}

func (h *AdminHandler) PutBuilderConfig(c *gin.Context) {
	if h == nil || h.releases == nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	h.releases.PutBuilderConfig(c)
}

func (h *AdminHandler) TestBuilderConfig(c *gin.Context) {
	if h == nil || h.releases == nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	h.releases.TestBuilderConfig(c)
}

func (h *AdminHandler) ListReleases(c *gin.Context, params ListReleasesParams) {
	if h == nil || h.releases == nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	h.releases.ListReleases(c, params)
}

func (h *AdminHandler) CreateRelease(c *gin.Context) {
	if h == nil || h.releases == nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	h.releases.CreateRelease(c)
}

func (h *AdminHandler) GetRelease(c *gin.Context, releaseID ReleaseId) {
	if h == nil || h.releases == nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	h.releases.GetRelease(c, releaseID)
}

func (h *AdminHandler) RetryRelease(c *gin.Context, releaseID ReleaseId) {
	if h == nil || h.releases == nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	h.releases.RetryRelease(c, releaseID)
}

func (h *AdminHandler) AcceptJenkinsCallback(c *gin.Context) {
	if h == nil || h.releases == nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	h.releases.AcceptJenkinsCallback(c)
}

func (h *AdminHandler) GetReleaseBundle(c *gin.Context, releaseID ReleaseId) {
	if h == nil || h.releases == nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	h.releases.GetReleaseBundle(c, releaseID)
}

func (h *AdminHandler) authenticate(c *gin.Context) bool {
	if !h.authenticateAllowQuery(c) {
		return false
	}
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		WriteProblem(c, ErrInvalidRequest)
		return false
	}
	return true
}

func (h *AdminHandler) authenticateAllowQuery(c *gin.Context) bool {
	if h == nil {
		WriteProblem(c, auth.ErrInternal)
		return false
	}
	admin, ok := requireAdmin(c)
	if !ok {
		return false
	}
	setAdminLogContext(c, admin.ID)
	return true
}

// adminGeneratedErrorHandler is reached only after the outer RequireAdmin
// route handler has authenticated the request.
func adminGeneratedErrorHandler(c *gin.Context, _ error, _ int) {
	WriteProblem(c, ErrInvalidRequest)
}

func writeStage2Problem(c *gin.Context, err error) {
	if knownStage2Error(err) {
		WriteProblem(c, err)
		return
	}
	WriteProblem(c, ErrDependencyUnavailable)
}

func knownStage2Error(err error) bool {
	return errors.Is(err, article.ErrNotFound) || errors.Is(err, article.ErrSlugConflict) ||
		errors.Is(err, article.ErrMustBeUnpublished) || errors.Is(err, article.ErrStateConflict) ||
		errors.Is(err, revision.ErrNotFound) || errors.Is(err, revision.ErrConflict) ||
		errors.Is(err, revision.ErrInvalidContent) || errors.Is(err, revision.ErrNotFrozen) ||
		errors.Is(err, revision.ErrArticleInactive) || errors.Is(err, tag.ErrNotFound) ||
		errors.Is(err, tag.ErrNameConflict) || errors.Is(err, tag.ErrSlugConflict) ||
		errors.Is(err, tag.ErrInvalidName) || errors.Is(err, tag.ErrInvalidSelection) ||
		errors.Is(err, media.ErrNotFound) || errors.Is(err, media.ErrInvalidMetadata) ||
		errors.Is(err, media.ErrPublicKeyConflict) || errors.Is(err, media.ErrGFSFileIDConflict) ||
		errors.Is(err, media.ErrDependencyUnavailable) || errors.Is(err, settings.ErrConflict) ||
		errors.Is(err, settings.ErrInvalid)
}

func nilAdminDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

var _ ServerInterface = (*AdminHandler)(nil)
