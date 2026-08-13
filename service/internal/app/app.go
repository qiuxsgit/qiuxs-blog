package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/article"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/health"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/httpapi"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/redis/go-redis/v9"
)

// Dependencies are process-owned resources used by the HTTP application.
// Build does not close them; the process entrypoint remains their owner.
type Dependencies struct {
	DB         *sql.DB
	Redis      *redis.Client
	Logger     *slog.Logger
	Random     io.Reader
	Now        func() time.Time
	HTTPClient *http.Client

	observeBuild func(buildObservation)
}

type applicationComponents struct {
	auth       auth.Service
	admin      *httpapi.AdminHandler
	mediaProxy *httpapi.MediaProxyHandler
}

// buildObservation is an internal-only wiring audit seam. It records the
// constructor inputs, rather than exposing package-private repository fields or
// introducing global hooks.
type buildObservation struct {
	ids                   *idgen.Generator
	articleRepositoryIDs  *idgen.Generator
	revisionRepositoryIDs *idgen.Generator
	tagRepositoryIDs      *idgen.Generator
	mediaRepositoryIDs    *idgen.Generator
	settingsRepositoryIDs *idgen.Generator

	keys        *randomkey.Generator
	articleKeys *randomkey.Generator
	tagKeys     *randomkey.Generator
	mediaKeys   *randomkey.Generator
	signerKeys  *randomkey.Generator

	articleNow  func() time.Time
	revisionNow func() time.Time
	mediaNow    func() time.Time
	siteNow     func() time.Time
	hotlinkNow  func() time.Time
	proxyNow    func() time.Time

	hotlinkForAdmin settings.HotlinkService
	hotlinkForProxy settings.HotlinkService
}

// Build composes the service's shared repositories and HTTP middleware.
func Build(cfg config.Config, deps Dependencies) (*gin.Engine, error) {
	if err := validate(cfg, deps); err != nil {
		return nil, err
	}
	components, observation, err := buildComponents(cfg, deps)
	if err != nil {
		return nil, err
	}
	if deps.observeBuild != nil {
		deps.observeBuild(observation)
	}

	router := gin.New()
	router.Use(httpapi.RequestID(), accessLog(deps.Logger), recoverProblem(deps.Logger))
	if err := router.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("disable trusted proxies: %w", err)
	}

	health.RegisterRoutes(
		router,
		health.CheckFunc(func(ctx context.Context) error { return deps.DB.PingContext(ctx) }),
		health.CheckFunc(func(ctx context.Context) error { return deps.Redis.Ping(ctx).Err() }),
	)
	httpapi.RegisterMediaProxy(router, components.mediaProxy)

	adminRoutes := router.Group("")
	adminRoutes.Use(httpapi.OriginGuard(cfg.HTTP.AdminOrigin), httpapi.LoadAdminSession(components.auth, cfg.Session.CookieName))
	httpapi.RegisterAdminHandlers(adminRoutes, components.admin)

	router.NoRoute(func(c *gin.Context) {
		httpapi.WriteProblem(c, httpapi.ErrNotFound)
	})
	return router, nil
}

func buildComponents(cfg config.Config, deps Dependencies) (applicationComponents, buildObservation, error) {
	counter := idgen.NewRedisCounter(deps.Redis)
	ids, err := idgen.New(counter, deps.DB, cfg.IDGen.Offset, cfg.IDGen.Step, cfg.IDGen.Heal)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct ID generator: %w", err)
	}
	keys, err := randomkey.New(deps.Random)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct random key generator: %w", err)
	}
	gfsClient, err := media.NewGFSClient(cfg.GFS.BaseURL, deps.HTTPClient)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct GFS metadata client: %w", err)
	}
	gfsSigner, err := media.NewGFSSigner(cfg.GFS.BaseURL, cfg.GFS.AppID, cfg.GFS.AppSecret, cfg.GFS.PublicReadSecret, keys)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct GFS signer: %w", err)
	}

	tagRepository := tag.NewMySQLRepository(deps.DB, ids)
	mediaRepository := media.NewMySQLRepository(deps.DB, ids)
	revisionRepository := revision.NewMySQLRepository(deps.DB, ids)
	articleRepository := article.NewMySQLRepository(deps.DB, ids)
	settingsRepository := settings.NewMySQLRepository(deps.DB, ids)

	tagService, err := tag.NewService(tagRepository, keys, deps.Now)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct tag service: %w", err)
	}
	mediaService, err := media.NewService(mediaRepository, gfsClient, gfsSigner, keys, deps.Now)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct media service: %w", err)
	}
	revisionService, err := revision.NewService(revisionRepository, tagService, mediaService, deps.Now)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct revision service: %w", err)
	}
	articleService, err := article.NewService(articleRepository, revisionService, keys, deps.Now)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct article service: %w", err)
	}
	siteService, err := settings.NewSiteService(settingsRepository, mediaService, deps.Now)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct site settings service: %w", err)
	}
	hotlinkService, err := settings.NewHotlinkService(settingsRepository, deps.Now)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct hotlink settings service: %w", err)
	}
	proxyService, err := media.NewProxyService(hotlinkService, mediaService, gfsSigner, deps.Now)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct media proxy service: %w", err)
	}
	mediaProxyHandler, err := httpapi.NewMediaProxyHandler(proxyService)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct media proxy handler: %w", err)
	}

	authRepository := auth.NewMySQLRepository(deps.DB, ids)
	store := auth.NewRedisSessionStore(deps.Redis, deps.Now)
	sessions := auth.NewSessionManager(store, cfg.Session.TTL, deps.Random, deps.Now)
	limiter := auth.NewRedisLoginLimiter(deps.Redis, deps.Now)
	authService := auth.NewServiceWithLogger(authRepository, auth.DefaultPasswordHasher(), sessions, limiter, deps.Now, deps.Logger)
	authHandler := httpapi.NewAuthHandler(authService, cfg.Session)
	adminHandler, err := httpapi.NewAdminHandler(authHandler, articleService, revisionService, tagService, mediaService, siteService, hotlinkService)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct Admin handler: %w", err)
	}

	return applicationComponents{auth: authService, admin: adminHandler, mediaProxy: mediaProxyHandler}, buildObservation{
		ids: ids, articleRepositoryIDs: ids, revisionRepositoryIDs: ids, tagRepositoryIDs: ids,
		mediaRepositoryIDs: ids, settingsRepositoryIDs: ids,
		keys: keys, articleKeys: keys, tagKeys: keys, mediaKeys: keys, signerKeys: keys,
		articleNow: deps.Now, revisionNow: deps.Now, mediaNow: deps.Now, siteNow: deps.Now, hotlinkNow: deps.Now, proxyNow: deps.Now,
		hotlinkForAdmin: hotlinkService, hotlinkForProxy: hotlinkService,
	}, nil
}

func validate(cfg config.Config, deps Dependencies) error {
	if err := config.Validate(cfg); err != nil {
		return err
	}
	switch {
	case !databaseConfigured(deps.DB):
		return errors.New("database dependency is required")
	case !redisConfigured(deps.Redis):
		return errors.New("redis dependency is required")
	case deps.Logger == nil || deps.Logger.Handler() == nil:
		return errors.New("logger dependency is required")
	case isNil(deps.Random):
		return errors.New("random dependency is required")
	case deps.Now == nil:
		return errors.New("clock dependency is required")
	case deps.HTTPClient == nil:
		return errors.New("HTTP client dependency is required")
	}
	return nil
}

func databaseConfigured(db *sql.DB) (configured bool) {
	if db == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			configured = false
		}
	}()
	return db.Driver() != nil
}

func redisConfigured(client *redis.Client) (configured bool) {
	if client == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			configured = false
		}
	}()
	return client.Options() != nil
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func accessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()

		attributes := []slog.Attr{
			slog.String("request_id", httpapi.RequestIDFrom(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", routeTemplate(c)),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("duration_ms", time.Since(started).Milliseconds()),
		}
		if adminID, ok := httpapi.AdminIDFromLogContext(c); ok {
			attributes = append(attributes, slog.Int64("admin_id", adminID))
		}
		if articleID, ok := httpapi.ArticleIDFromLogContext(c); ok {
			attributes = append(attributes, slog.Int64("article_id", articleID))
		}
		logger.LogAttrs(c.Request.Context(), slog.LevelInfo, "http request", attributes...)
	}
}

func recoverProblem(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() == nil {
				return
			}

			logger.LogAttrs(
				c.Request.Context(),
				slog.LevelError,
				"panic recovered",
				slog.String("request_id", httpapi.RequestIDFrom(c)),
				slog.String("method", c.Request.Method),
				slog.String("path", routeTemplate(c)),
			)
			httpapi.WriteProblem(c, auth.ErrInternal)
			c.Abort()
		}()
		c.Next()
	}
}

func routeTemplate(c *gin.Context) string {
	if c != nil && c.FullPath() != "" {
		return c.FullPath()
	}
	return "<unmatched>"
}
