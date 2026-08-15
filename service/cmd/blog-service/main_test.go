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
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/app"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestOpenReleaseArtifactAcceptsRegularFileThroughSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	versions := filepath.Join(root, "versions", "v1")
	require.NoError(t, os.MkdirAll(versions, 0o755))
	want := []byte(`{"releaseId":7}`)
	require.NoError(t, os.WriteFile(filepath.Join(versions, "release.json"), want, 0o600))
	require.NoError(t, os.Symlink(versions, filepath.Join(root, "current")))

	reader, err := openReleaseArtifact(filepath.Join(root, "current", "release.json"))
	require.NoError(t, err)
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, want, got)
}

func TestOpenReleaseArtifactRejectsNonRegularFinalEntryWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "regular.json")
	require.NoError(t, os.WriteFile(regular, []byte(`{}`), 0o600))
	symlink := filepath.Join(root, "release-symlink.json")
	require.NoError(t, os.Symlink(regular, symlink))
	fifo := filepath.Join(root, "release-fifo.json")
	require.NoError(t, unix.Mkfifo(fifo, 0o600))

	for _, path := range []string{root, symlink, fifo, "/dev/null"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			result := make(chan error, 1)
			go func() {
				reader, err := openReleaseArtifact(path)
				if reader != nil {
					_ = reader.Close()
				}
				result <- err
			}()
			select {
			case err := <-result:
				require.Error(t, err)
			case <-time.After(time.Second):
				t.Fatal("secure artifact open blocked on a non-regular final entry")
			}
		})
	}
}

func TestOpenReleaseArtifactDetectsReplacementRaceAndClosesDescriptor(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "release.json")
	displaced := filepath.Join(root, "displaced.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"releaseId":7}`), 0o600))
	var opened *os.File

	reader, err := openReleaseArtifactWith(path, func(path string) (*os.File, error) {
		require.NoError(t, os.Rename(path, displaced))
		require.NoError(t, os.WriteFile(path, []byte(`{"releaseId":8}`), 0o600))
		var openErr error
		opened, openErr = openArtifactNoFollow(displaced)
		return opened, openErr
	})

	require.Error(t, err)
	require.Nil(t, reader)
	require.NotNil(t, opened)
	_, statErr := opened.Stat()
	require.ErrorIs(t, statErr, os.ErrClosed)
}

func TestRunConfiguresServerAndShutsDownGracefully(t *testing.T) {
	runtime, closes := validRuntime(t)
	probeTransport := &countingRoundTripper{}
	runtime.httpClient = &http.Client{Timeout: 5 * time.Second, Transport: probeTransport}
	jenkinsProbeTransport := &countingRoundTripper{}
	runtime.jenkinsHTTPClient = &http.Client{Timeout: 5 * time.Second, Transport: jenkinsProbeTransport}
	var buildDependencies app.Dependencies
	prepared := &preparedApplicationFake{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	runtime.prepare = func(_ config.Config, dependencies app.Dependencies) (preparedApplication, error) {
		buildDependencies = dependencies
		return prepared, nil
	}
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
	require.Same(t, runtime.httpClient, buildDependencies.HTTPClient)
	require.Equal(t, 5*time.Second, buildDependencies.HTTPClient.Timeout)
	require.Same(t, runtime.jenkinsHTTPClient, buildDependencies.JenkinsHTTPClient)
	require.NotNil(t, buildDependencies.ReleaseJSONReader)
	require.Equal(t, 1, prepared.reconciles)
	require.Zero(t, probeTransport.calls, "startup must not probe GFS")
	require.Zero(t, jenkinsProbeTransport.calls, "startup must not probe Jenkins")
	require.Greater(t, shutdownRemaining, 29*time.Second)
	require.LessOrEqual(t, shutdownRemaining, 30*time.Second)
	require.Equal(t, 1, closes.mysql)
	require.Equal(t, 1, closes.redis)
}

func TestBlogServiceStartupNeverReadsOrExecutesDevelopmentSQL(t *testing.T) {
	source, err := os.ReadFile("main.go")
	require.NoError(t, err)
	for _, forbidden := range []string{
		"develop.sql",
		"sqls/develop",
		"os.ReadFile(",
		"os.ReadDir(",
		"ExecContext(",
		"QueryContext(",
	} {
		require.NotContains(t, string(source), forbidden)
	}
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
				runtime.prepare = func(config.Config, app.Dependencies) (preparedApplication, error) { return nil, failure }
			},
			wantMySQL: 1,
			wantRedis: 1,
		},
		{
			name: "release reconciliation",
			mutate: func(runtime *runtimeDependencies) {
				runtime.prepare = func(config.Config, app.Dependencies) (preparedApplication, error) {
					return &preparedApplicationFake{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), reconcileErr: failure}, nil
				}
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

type countingRoundTripper struct{ calls int }

func (t *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("unexpected HTTP dependency call")
}

type preparedApplicationFake struct {
	handler      http.Handler
	reconcileErr error
	reconciles   int
}

func (a *preparedApplicationFake) Handler() http.Handler { return a.handler }

func (a *preparedApplicationFake) Reconcile(context.Context) (bool, error) {
	a.reconciles++
	return false, a.reconcileErr
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
			"BLOG_MYSQL_HOST":             "mysql",
			"BLOG_MYSQL_PORT":             "3306",
			"BLOG_MYSQL_USER":             "user",
			"BLOG_MYSQL_PASSWORD":         "password",
			"BLOG_MYSQL_DATABASE":         "blog",
			"BLOG_MYSQL_ARGS":             "parseTime=true&loc=UTC&charset=utf8mb4",
			"BLOG_REDIS_ADDR":             "redis:6379",
			"BLOG_ADMIN_ORIGIN":           "https://admin.example.com",
			"BLOG_GFS_BASE_URL":           "https://gfs.example.com",
			"BLOG_GFS_APP_ID":             "blog-app",
			"BLOG_GFS_APP_SECRET":         "test-app-secret",
			"BLOG_GFS_PUBLIC_READ_SECRET": "test-public-read-secret",
			"BLOG_BUNDLE_TOKEN":           "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"BLOG_CALLBACK_HMAC_KEY":      "hhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhh",
			"BLOG_BUILDER_MASTER_KEY":     "a2tra2tra2tra2tra2tra2tra2tra2tra2tra2tra2s",
		}),
		logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		random: bytes.NewReader(make([]byte, 128)),
		now:    time.Now,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		jenkinsHTTPClient: &http.Client{Timeout: 5 * time.Second},
		openArtifact: func(string) (io.ReadCloser, error) {
			return nil, os.ErrNotExist
		},
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
		prepare: func(config.Config, app.Dependencies) (preparedApplication, error) {
			return &preparedApplicationFake{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}, nil
		},
		serve:    func(*http.Server) error { return errors.New("unexpected serve") },
		shutdown: func(*http.Server, context.Context) error { return errors.New("unexpected shutdown") },
	}, closes
}

func configEnvironment(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
