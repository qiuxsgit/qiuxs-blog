package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/app"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRunConfiguresServerAndShutsDownGracefully(t *testing.T) {
	runtime, closes := validRuntime(t)
	serveDone := make(chan struct{})
	var served *http.Server
	runtime.serve = func(server *http.Server) error {
		served = server
		<-serveDone
		return http.ErrServerClosed
	}
	var shutdownServer *http.Server
	var shutdownRemaining time.Duration
	runtime.shutdown = func(server *http.Server, ctx context.Context) error {
		shutdownServer = server
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		shutdownRemaining = time.Until(deadline)
		close(serveDone)
		return nil
	}
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM
	runtime.signals = signals

	require.NoError(t, run(runtime))
	require.Same(t, served, shutdownServer)
	require.Equal(t, ":19090", served.Addr)
	require.NotNil(t, served.Handler)
	require.Equal(t, 5*time.Second, served.ReadHeaderTimeout)
	require.Equal(t, 15*time.Second, served.ReadTimeout)
	require.Equal(t, 30*time.Second, served.WriteTimeout)
	require.Equal(t, 60*time.Second, served.IdleTimeout)
	require.Greater(t, shutdownRemaining, 29*time.Second)
	require.LessOrEqual(t, shutdownRemaining, 30*time.Second)
	require.Equal(t, 1, closes.mysql)
	require.Equal(t, 1, closes.redis)
}

func TestExecuteReturnsFailureAndClosesOnlyOpenedResources(t *testing.T) {
	failure := errors.New("injected failure")
	tests := []struct {
		name      string
		mutate    func(*runtimeDependencies)
		wantMySQL int
		wantRedis int
	}{
		{
			name: "configuration",
			mutate: func(runtime *runtimeDependencies) {
				runtime.getenv = func(string) string { return "" }
			},
		},
		{
			name: "mysql connection",
			mutate: func(runtime *runtimeDependencies) {
				runtime.openMySQL = func(config.MySQLConfig) (*sql.DB, error) { return nil, failure }
			},
		},
		{
			name: "redis connection",
			mutate: func(runtime *runtimeDependencies) {
				runtime.openRedis = func(config.RedisConfig) (*redis.Client, error) { return nil, failure }
			},
			wantMySQL: 1,
		},
		{
			name: "application build",
			mutate: func(runtime *runtimeDependencies) {
				runtime.build = func(config.Config, app.Dependencies) (http.Handler, error) { return nil, failure }
			},
			wantMySQL: 1,
			wantRedis: 1,
		},
		{
			name: "listen",
			mutate: func(runtime *runtimeDependencies) {
				runtime.serve = func(*http.Server) error { return failure }
			},
			wantMySQL: 1,
			wantRedis: 1,
		},
		{
			name: "shutdown",
			mutate: func(runtime *runtimeDependencies) {
				signals := make(chan os.Signal, 1)
				signals <- os.Interrupt
				runtime.signals = signals
				serveDone := make(chan struct{})
				runtime.serve = func(*http.Server) error {
					<-serveDone
					return http.ErrServerClosed
				}
				runtime.shutdown = func(*http.Server, context.Context) error {
					close(serveDone)
					return failure
				}
			},
			wantMySQL: 1,
			wantRedis: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, closes := validRuntime(t)
			var logs bytes.Buffer
			runtime.logger = slog.New(slog.NewJSONHandler(&logs, nil))
			test.mutate(&runtime)

			require.Equal(t, 1, execute(runtime))
			require.Contains(t, logs.String(), `"msg":"service failed"`)
			require.Equal(t, test.wantMySQL, closes.mysql)
			require.Equal(t, test.wantRedis, closes.redis)
		})
	}
}

type closeCounts struct {
	mysql int
	redis int
}

func validRuntime(t *testing.T) (runtimeDependencies, *closeCounts) {
	t.Helper()
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	miniRedis := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	closes := &closeCounts{}
	return runtimeDependencies{
		getenv: configEnvironment(map[string]string{
			"BLOG_ENV":                    "production",
			"BLOG_HTTP_ADDR":              ":19090",
			"BLOG_MYSQL_DSN":              "user:password@tcp(mysql:3306)/blog",
			"BLOG_REDIS_ADDR":             "redis:6379",
			"BLOG_ADMIN_ORIGIN":           "https://admin.example.com",
			"BLOG_GFS_BASE_URL":           "https://gfs.example.com",
			"BLOG_GFS_APP_ID":             "blog-app",
			"BLOG_GFS_APP_SECRET":         "test-app-secret",
			"BLOG_GFS_PUBLIC_READ_SECRET": "test-public-read-secret",
		}),
		logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
		random:  bytes.NewReader(make([]byte, 128)),
		now:     time.Now,
		signals: make(chan os.Signal),
		openMySQL: func(config.MySQLConfig) (*sql.DB, error) {
			return db, nil
		},
		closeMySQL: func(*sql.DB) error {
			closes.mysql++
			return nil
		},
		openRedis: func(config.RedisConfig) (*redis.Client, error) {
			return redisClient, nil
		},
		closeRedis: func(*redis.Client) error {
			closes.redis++
			return nil
		},
		build: func(config.Config, app.Dependencies) (http.Handler, error) {
			return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil
		},
		serve:    func(*http.Server) error { return errors.New("unexpected serve") },
		shutdown: func(*http.Server, context.Context) error { return errors.New("unexpected shutdown") },
	}, closes
}

func configEnvironment(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
