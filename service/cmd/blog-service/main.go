package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/app"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/redis/go-redis/v9"
)

const shutdownTimeout = 30 * time.Second

type runtimeDependencies struct {
	getenv  func(string) string
	logger  *slog.Logger
	random  io.Reader
	now     func() time.Time
	signals <-chan os.Signal

	openMySQL  func(config.MySQLConfig) (*sql.DB, error)
	closeMySQL func(*sql.DB) error
	openRedis  func(config.RedisConfig) (*redis.Client, error)
	closeRedis func(*redis.Client) error
	build      func(config.Config, app.Dependencies) (http.Handler, error)
	serve      func(*http.Server) error
	shutdown   func(*http.Server, context.Context) error
}

func run(runtime runtimeDependencies) (resultErr error) {
	cfg, err := config.Load(runtime.getenv)
	if err != nil {
		return errors.New("load configuration: operation failed")
	}

	db, err := runtime.openMySQL(cfg.MySQL)
	if err != nil {
		return errors.New("open mysql: operation failed")
	}
	defer func() {
		if err := runtime.closeMySQL(db); err != nil {
			resultErr = errors.Join(resultErr, errors.New("close mysql: operation failed"))
		}
	}()

	redisClient, err := runtime.openRedis(cfg.Redis)
	if err != nil {
		return errors.New("open redis: operation failed")
	}
	defer func() {
		if err := runtime.closeRedis(redisClient); err != nil {
			resultErr = errors.Join(resultErr, errors.New("close redis: operation failed"))
		}
	}()

	handler, err := runtime.build(cfg, app.Dependencies{
		DB:     db,
		Redis:  redisClient,
		Logger: runtime.logger,
		Random: runtime.random,
		Now:    runtime.now,
	})
	if err != nil {
		return errors.New("build application: operation failed")
	}

	server := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- runtime.serve(server) }()

	select {
	case err := <-serveErrors:
		if err == nil {
			return errors.New("serve HTTP: server stopped unexpectedly")
		}
		return errors.New("serve HTTP: operation failed")
	case <-runtime.signals:
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		err := runtime.shutdown(server, shutdownContext)
		cancel()
		if err != nil {
			_ = server.Close()
			return errors.New("shutdown HTTP server: operation failed")
		}

		if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return errors.New("serve HTTP: operation failed")
		}
		return nil
	}
}

func execute(runtime runtimeDependencies) int {
	if err := run(runtime); err != nil {
		runtime.logger.Error("service failed", slog.String("error", err.Error()))
		return 1
	}
	return 0
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	runtime := runtimeDependencies{
		getenv:     os.Getenv,
		logger:     logger,
		random:     rand.Reader,
		now:        time.Now,
		signals:    signals,
		openMySQL:  platform.OpenMySQL,
		closeMySQL: func(db *sql.DB) error { return db.Close() },
		openRedis:  platform.OpenRedis,
		closeRedis: func(client *redis.Client) error { return client.Close() },
		build: func(cfg config.Config, deps app.Dependencies) (http.Handler, error) {
			return app.Build(cfg, deps)
		},
		serve:    func(server *http.Server) error { return server.ListenAndServe() },
		shutdown: func(server *http.Server, ctx context.Context) error { return server.Shutdown(ctx) },
	}

	exitCode := execute(runtime)
	signal.Stop(signals)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
