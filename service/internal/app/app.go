package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/health"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/httpapi"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/redis/go-redis/v9"
)

// Dependencies are process-owned resources used by the HTTP application.
// Build does not close them; the process entrypoint remains their owner.
type Dependencies struct {
	DB     *sql.DB
	Redis  *redis.Client
	Logger *slog.Logger
	Random io.Reader
	Now    func() time.Time
}

// Build composes the service's shared repositories and HTTP middleware.
func Build(cfg config.Config, deps Dependencies) (*gin.Engine, error) {
	if err := validate(cfg, deps); err != nil {
		return nil, err
	}

	counter := idgen.NewRedisCounter(deps.Redis)
	ids, err := idgen.New(counter, deps.DB, cfg.IDGen.Offset, cfg.IDGen.Step, cfg.IDGen.Heal)
	if err != nil {
		return nil, fmt.Errorf("construct ID generator: %w", err)
	}
	repository := auth.NewMySQLRepository(deps.DB, ids)
	store := auth.NewRedisSessionStore(deps.Redis, deps.Now)
	sessions := auth.NewSessionManager(store, cfg.Session.TTL, deps.Random, deps.Now)
	limiter := auth.NewRedisLoginLimiter(deps.Redis, deps.Now)
	service := auth.NewServiceWithLogger(repository, auth.DefaultPasswordHasher(), sessions, limiter, deps.Now, deps.Logger)
	handler := httpapi.NewAuthHandler(service, cfg.Session)

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

	adminRoutes := router.Group("")
	adminRoutes.Use(httpapi.OriginGuard(cfg.HTTP.AdminOrigin), httpapi.LoadAdminSession(service, cfg.Session.CookieName))
	httpapi.RegisterAuthHandlers(adminRoutes, handler)

	router.NoRoute(func(c *gin.Context) {
		httpapi.WriteProblem(c, httpapi.ErrNotFound)
	})
	return router, nil
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

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		logger.LogAttrs(
			c.Request.Context(),
			slog.LevelInfo,
			"http request",
			slog.String("request_id", httpapi.RequestIDFrom(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("duration_ms", time.Since(started).Milliseconds()),
		)
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
				slog.String("path", c.Request.URL.Path),
			)
			httpapi.WriteProblem(c, auth.ErrInternal)
			c.Abort()
		}()
		c.Next()
	}
}
