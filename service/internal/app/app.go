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
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/article"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/builder"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/health"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/httpapi"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/release"
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
	// JenkinsHTTPClient is cloned by the Jenkins adapter. Build never sends a
	// request through it.
	JenkinsHTTPClient *http.Client
	// ReleaseJSONReader is process-owned and is invoked only by Application.Reconcile.
	ReleaseJSONReader release.ArtifactReader

	observeBuild        func(buildObservation)
	mutateBuildArgument func(buildRole, any) any
}

type applicationComponents struct {
	auth       auth.Service
	admin      *httpapi.AdminHandler
	mediaProxy *httpapi.MediaProxyHandler
	callback   *builder.CallbackVerifier
	releases   *release.Service
}

// Application is a composed HTTP application with an explicit startup
// reconciliation step. Prepare and Build never read the deployed artifact.
type Application struct {
	router   *gin.Engine
	releases *release.Service
	artifact release.ArtifactReader
}

// buildObservation is an internal-only wiring audit seam. It records the
// constructor inputs, rather than exposing package-private repository fields or
// introducing global hooks.
type buildObservation struct {
	arguments map[buildRole]any
}

type buildRole string

const (
	buildAuthRepositoryIDs     buildRole = "auth repository ID generator"
	buildArticleRepositoryIDs  buildRole = "article repository ID generator"
	buildRevisionRepositoryIDs buildRole = "revision repository ID generator"
	buildTagRepositoryIDs      buildRole = "tag repository ID generator"
	buildMediaRepositoryIDs    buildRole = "media repository ID generator"
	buildSettingsRepositoryIDs buildRole = "settings repository ID generator"
	buildBuilderRepositoryIDs  buildRole = "builder repository ID generator"
	buildReleaseRepositoryIDs  buildRole = "release repository ID generator"
	buildSnapshotSourceIDs     buildRole = "release snapshot source ID generator"
	buildGFSSignerKeys         buildRole = "GFS signer random key generator"
	buildArticleServiceKeys    buildRole = "article service random key generator"
	buildTagServiceKeys        buildRole = "tag service random key generator"
	buildMediaServiceKeys      buildRole = "media service random key generator"
	buildArticleServiceClock   buildRole = "article service clock"
	buildRevisionServiceClock  buildRole = "revision service clock"
	buildTagServiceClock       buildRole = "tag service clock"
	buildMediaServiceClock     buildRole = "media service clock"
	buildSiteServiceClock      buildRole = "site settings service clock"
	buildHotlinkServiceClock   buildRole = "hotlink settings service clock"
	buildMediaProxyClock       buildRole = "media proxy clock"
	buildSnapshotSourceClock   buildRole = "release snapshot source clock"
	buildReleaseServiceClock   buildRole = "release orchestrator clock"
	buildAuthRandom            buildRole = "auth random source"
	buildSecretBoxRandom       buildRole = "secret box random source"
	buildAdminHotlink          buildRole = "Admin hotlink service"
	buildMediaProxyHotlink     buildRole = "media proxy hotlink service"
)

type buildCapture struct {
	observation buildObservation
	mutate      func(buildRole, any) any
	err         error
}

type buildClock struct {
	now func() time.Time
}

type synchronizedReader struct {
	mu     sync.Mutex
	reader io.Reader
}

func (r *synchronizedReader) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reader.Read(buffer)
}

// Build composes the service's shared repositories and HTTP middleware.
func Build(cfg config.Config, deps Dependencies) (*gin.Engine, error) {
	application, err := Prepare(cfg, deps)
	if err != nil {
		return nil, err
	}
	return application.router, nil
}

// Prepare composes the application without reading the deployed release
// artifact or probing SQL, Redis, GFS, or Jenkins.
func Prepare(cfg config.Config, deps Dependencies) (*Application, error) {
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
	httpapi.RegisterInternalReleaseHandlers(router, components.admin, cfg.Release.BundleToken, components.callback)

	adminRoutes := router.Group("")
	adminRoutes.Use(httpapi.OriginGuard(cfg.HTTP.AdminOrigin), httpapi.LoadAdminSession(components.auth, cfg.Session.CookieName))
	httpapi.RegisterAdminHandlers(adminRoutes, components.admin)

	router.NoRoute(func(c *gin.Context) {
		httpapi.WriteProblem(c, httpapi.ErrNotFound)
	})
	return &Application{router: router, releases: components.releases, artifact: deps.ReleaseJSONReader}, nil
}

// Handler returns the composed HTTP handler.
func (a *Application) Handler() http.Handler {
	if a == nil {
		return nil
	}
	return a.router
}

// Reconcile compares the deployed artifact with persisted release state. A
// missing artifact is a valid first-deployment state.
func (a *Application) Reconcile(ctx context.Context) (bool, error) {
	if a == nil || a.releases == nil || a.artifact == nil {
		return false, errors.New("application reconciliation is not configured")
	}
	return a.releases.Reconcile(ctx, a.artifact)
}

func buildComponents(cfg config.Config, deps Dependencies) (applicationComponents, buildObservation, error) {
	capture := &buildCapture{
		observation: buildObservation{arguments: make(map[buildRole]any)},
		mutate:      deps.mutateBuildArgument,
	}
	clock := &buildClock{now: deps.Now}
	random := &synchronizedReader{reader: deps.Random}
	counter := idgen.NewRedisCounter(deps.Redis)
	ids, err := idgen.New(counter, deps.DB, cfg.IDGen.Offset, cfg.IDGen.Step, cfg.IDGen.Heal)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct ID generator: %w", err)
	}
	keys, err := randomkey.New(random)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct random key generator: %w", err)
	}
	gfsClient, err := media.NewGFSClient(cfg.GFS.BaseURL, deps.HTTPClient)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct GFS metadata client: %w", err)
	}
	gfsSigner, err := media.NewGFSSigner(
		cfg.GFS.BaseURL,
		cfg.GFS.AppID,
		cfg.GFS.AppSecret,
		cfg.GFS.PublicReadSecret,
		captureBuildArgument(capture, buildGFSSignerKeys, keys),
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct GFS signer: %w", err)
	}

	tagRepository := tag.NewMySQLRepository(deps.DB, captureBuildArgument(capture, buildTagRepositoryIDs, ids))
	mediaRepository := media.NewMySQLRepository(deps.DB, captureBuildArgument(capture, buildMediaRepositoryIDs, ids))
	revisionRepository := revision.NewMySQLRepository(deps.DB, captureBuildArgument(capture, buildRevisionRepositoryIDs, ids))
	articleRepository := article.NewMySQLRepository(deps.DB, captureBuildArgument(capture, buildArticleRepositoryIDs, ids))
	settingsRepository := settings.NewMySQLRepository(deps.DB, captureBuildArgument(capture, buildSettingsRepositoryIDs, ids))
	box, err := platform.NewSecretBox(
		cfg.Release.BuilderMasterKey,
		captureBuildArgument(capture, buildSecretBoxRandom, random),
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct builder secret box: %w", err)
	}
	builderRepository := builder.NewMySQLRepository(deps.DB, captureBuildArgument(capture, buildBuilderRepositoryIDs, ids), &box)
	jenkinsClient, err := builder.NewClient(deps.JenkinsHTTPClient)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct Jenkins client: %w", err)
	}
	snapshotSource := release.NewMySQLSnapshotSource(
		captureBuildArgument(capture, buildSnapshotSourceIDs, ids),
		captureBuildClock(capture, buildSnapshotSourceClock, clock),
	)
	releaseRepository := release.NewMySQLRepository(deps.DB, captureBuildArgument(capture, buildReleaseRepositoryIDs, ids), snapshotSource)
	releaseService, err := release.NewService(releaseRepository)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct release service: %w", err)
	}
	targetProvider, err := builder.NewJenkinsTargetProvider(builderRepository, jenkinsClient, &box)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct Jenkins target provider: %w", err)
	}
	orchestrator, err := release.NewOrchestrator(
		releaseService,
		targetProvider,
		deps.ReleaseJSONReader,
		captureBuildClock(capture, buildReleaseServiceClock, clock),
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct release orchestrator: %w", err)
	}
	callbackVerifier, err := builder.NewCallbackVerifier(cfg.Release.CallbackHMACKey, deps.Redis, deps.Now)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct Jenkins callback verifier: %w", err)
	}

	tagService, err := tag.NewService(
		tagRepository,
		captureBuildArgument(capture, buildTagServiceKeys, keys),
		captureBuildClock(capture, buildTagServiceClock, clock),
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct tag service: %w", err)
	}
	mediaService, err := media.NewService(
		mediaRepository,
		gfsClient,
		gfsSigner,
		captureBuildArgument(capture, buildMediaServiceKeys, keys),
		captureBuildClock(capture, buildMediaServiceClock, clock),
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct media service: %w", err)
	}
	revisionService, err := revision.NewService(
		revisionRepository,
		tagService,
		mediaService,
		captureBuildClock(capture, buildRevisionServiceClock, clock),
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct revision service: %w", err)
	}
	articleService, err := article.NewService(
		articleRepository,
		revisionService,
		captureBuildArgument(capture, buildArticleServiceKeys, keys),
		captureBuildClock(capture, buildArticleServiceClock, clock),
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct article service: %w", err)
	}
	siteService, err := settings.NewSiteService(
		settingsRepository,
		mediaService,
		captureBuildClock(capture, buildSiteServiceClock, clock),
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct site settings service: %w", err)
	}
	hotlinkService, err := settings.NewHotlinkService(
		settingsRepository,
		captureBuildClock(capture, buildHotlinkServiceClock, clock),
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct hotlink settings service: %w", err)
	}
	proxyService, err := media.NewProxyService(
		captureBuildArgument(capture, buildMediaProxyHotlink, hotlinkService),
		mediaService,
		gfsSigner,
		captureBuildClock(capture, buildMediaProxyClock, clock),
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct media proxy service: %w", err)
	}
	mediaProxyHandler, err := httpapi.NewMediaProxyHandler(proxyService)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct media proxy handler: %w", err)
	}

	authRepository := auth.NewMySQLRepository(deps.DB, captureBuildArgument(capture, buildAuthRepositoryIDs, ids))
	store := auth.NewRedisSessionStore(deps.Redis, deps.Now)
	sessions := auth.NewSessionManager(store, cfg.Session.TTL, captureBuildArgument(capture, buildAuthRandom, random), deps.Now)
	limiter := auth.NewRedisLoginLimiter(deps.Redis, deps.Now)
	authService := auth.NewServiceWithLogger(authRepository, auth.DefaultPasswordHasher(), sessions, limiter, deps.Now, deps.Logger)
	authHandler := httpapi.NewAuthHandler(authService, cfg.Session)
	releaseHandler, err := httpapi.NewReleaseHandler(
		builderRepository, jenkinsClient, &box, releaseRepository, releaseService, orchestrator,
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct release handler: %w", err)
	}
	adminHandler, err := httpapi.NewAdminHandler(
		authHandler,
		articleService,
		revisionService,
		tagService,
		mediaService,
		siteService,
		captureBuildArgument(capture, buildAdminHotlink, hotlinkService),
		releaseHandler,
	)
	if err != nil {
		return applicationComponents{}, buildObservation{}, fmt.Errorf("construct Admin handler: %w", err)
	}

	if capture.err != nil {
		return applicationComponents{}, capture.observation, capture.err
	}
	return applicationComponents{
		auth: authService, admin: adminHandler, mediaProxy: mediaProxyHandler,
		callback: callbackVerifier, releases: releaseService,
	}, capture.observation, nil
}

func captureBuildArgument[T any](capture *buildCapture, role buildRole, value T) T {
	if capture == nil {
		return value
	}
	if capture.mutate != nil {
		mutated, ok := capture.mutate(role, value).(T)
		if !ok {
			capture.err = errors.New("build argument mutation returned an invalid type")
		} else {
			value = mutated
		}
	}
	capture.observation.arguments[role] = value
	return value
}

func captureBuildClock(capture *buildCapture, role buildRole, clock *buildClock) func() time.Time {
	actual := captureBuildArgument(capture, role, clock)
	if actual == nil {
		return nil
	}
	return actual.now
}

func (o buildObservation) validateShared() error {
	groups := []struct {
		name  string
		roles []buildRole
	}{
		{"ID generator", []buildRole{
			buildAuthRepositoryIDs, buildArticleRepositoryIDs, buildRevisionRepositoryIDs,
			buildTagRepositoryIDs, buildMediaRepositoryIDs, buildSettingsRepositoryIDs,
			buildBuilderRepositoryIDs, buildReleaseRepositoryIDs, buildSnapshotSourceIDs,
		}},
		{"random key generator", []buildRole{
			buildGFSSignerKeys, buildArticleServiceKeys, buildTagServiceKeys, buildMediaServiceKeys,
		}},
		{"clock", []buildRole{
			buildArticleServiceClock, buildRevisionServiceClock, buildTagServiceClock,
			buildMediaServiceClock, buildSiteServiceClock, buildHotlinkServiceClock, buildMediaProxyClock,
			buildSnapshotSourceClock, buildReleaseServiceClock,
		}},
		{"random source", []buildRole{buildAuthRandom, buildSecretBoxRandom}},
		{"hotlink service", []buildRole{buildAdminHotlink, buildMediaProxyHotlink}},
	}
	for _, group := range groups {
		first, exists := o.arguments[group.roles[0]]
		if !exists || isNil(first) {
			return fmt.Errorf("%s wiring was not observed", group.name)
		}
		for _, role := range group.roles[1:] {
			candidate, candidateExists := o.arguments[role]
			if !candidateExists || !sameBuildIdentity(first, candidate) {
				return fmt.Errorf("%s wiring is not shared", group.name)
			}
		}
	}
	return nil
}

func sameBuildIdentity(left, right any) bool {
	leftValue, rightValue := reflect.ValueOf(left), reflect.ValueOf(right)
	if !leftValue.IsValid() || !rightValue.IsValid() || leftValue.Type() != rightValue.Type() {
		return false
	}
	switch leftValue.Kind() {
	case reflect.Ptr, reflect.UnsafePointer:
		return leftValue.Pointer() == rightValue.Pointer()
	default:
		return false
	}
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
	case deps.JenkinsHTTPClient == nil:
		return errors.New("Jenkins HTTP client dependency is required")
	case deps.ReleaseJSONReader == nil:
		return errors.New("release JSON reader dependency is required")
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
		if releaseID, ok := httpapi.ReleaseIDFromLogContext(c); ok {
			attributes = append(attributes, slog.Int64("release_id", releaseID))
		}
		if publishJobID, ok := httpapi.PublishJobIDFromLogContext(c); ok {
			attributes = append(attributes, slog.Int64("publish_job_id", publishJobID))
		}
		if buildNumber, ok := httpapi.JenkinsBuildNumberFromLogContext(c); ok {
			attributes = append(attributes, slog.Int64("jenkins_build_number", buildNumber))
		}
		if result, ok := httpapi.ResultFromLogContext(c); ok {
			attributes = append(attributes, slog.String("result", result))
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
