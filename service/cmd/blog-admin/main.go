package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/bootstrap"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/redis/go-redis/v9"
	"golang.org/x/term"
)

type passwordInput interface {
	ReadPassword(prompt string) (string, error)
}

type terminalPasswordInput struct {
	fd  int
	out io.Writer
}

func (i terminalPasswordInput) ReadPassword(prompt string) (string, error) {
	if _, err := fmt.Fprint(i.out, prompt); err != nil {
		return "", fmt.Errorf("write password prompt: %w", err)
	}
	password, err := term.ReadPassword(i.fd)
	if _, newlineErr := fmt.Fprintln(i.out); err == nil && newlineErr != nil {
		return "", fmt.Errorf("write password prompt: %w", newlineErr)
	}
	if err != nil {
		return "", fmt.Errorf("read hidden password: %w", err)
	}
	return string(password), nil
}

type adminInitializer func(context.Context, config.Config, string, string) (auth.Admin, error)

var errInitializeAdministrator = errors.New("initialize administrator")

func run(
	args []string,
	getenv func(string) string,
	input passwordInput,
	stdout io.Writer,
	initialize adminInitializer,
) error {
	if len(args) == 0 || args[0] != "init" {
		return errors.New("usage: blog-admin init --username <username>")
	}

	flags := flag.NewFlagSet("blog-admin init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	usernameFlag := flags.String("username", "", "administrator username")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("usage: blog-admin init --username <username>")
	}
	username, err := auth.NormalizeUsername(*usernameFlag)
	if err != nil {
		return err
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	password, err := adminPassword(getenv, input)
	if err != nil {
		return err
	}
	if initialize == nil {
		return errors.New("admin initializer is required")
	}
	admin, err := initialize(context.Background(), cfg, username, password)
	if err != nil {
		return fmt.Errorf("%w: %w", errInitializeAdministrator, err)
	}
	if _, err := fmt.Fprintf(stdout, "%d %s\n", admin.ID, username); err != nil {
		return fmt.Errorf("write administrator result: %w", err)
	}
	return nil
}

func adminPassword(getenv func(string) string, input passwordInput) (string, error) {
	if password := getenv("BLOG_ADMIN_PASSWORD"); password != "" {
		return password, nil
	}
	if input == nil {
		return "", errors.New("password input is required")
	}
	password, err := input.ReadPassword("Admin password: ")
	if err != nil {
		return "", err
	}
	confirmation, err := input.ReadPassword("Confirm admin password: ")
	if err != nil {
		return "", err
	}
	if password != confirmation {
		return "", errors.New("admin passwords do not match")
	}
	return password, nil
}

func initializeAdmin(ctx context.Context, cfg config.Config, username, password string) (auth.Admin, error) {
	db, err := platform.OpenMySQL(cfg.MySQL)
	if err != nil {
		return auth.Admin{}, err
	}
	defer func() { _ = db.Close() }()

	redisClient, err := platform.OpenRedis(cfg.Redis)
	if err != nil {
		return auth.Admin{}, err
	}
	defer func() { _ = redisClient.Close() }()

	return createAdminWithDependencies(ctx, cfg, db, redisClient, username, password)
}

func createAdminWithDependencies(
	ctx context.Context,
	cfg config.Config,
	db *sql.DB,
	redisClient *redis.Client,
	username string,
	password string,
) (auth.Admin, error) {
	ids, err := idgen.New(
		idgen.NewRedisCounter(redisClient),
		db,
		cfg.IDGen.Offset,
		cfg.IDGen.Step,
		cfg.IDGen.Heal,
	)
	if err != nil {
		return auth.Admin{}, fmt.Errorf("construct ID generator: %w", err)
	}
	repo := auth.NewMySQLRepository(db, ids)
	return bootstrap.CreateFirstAdmin(ctx, repo, auth.DefaultPasswordHasher(), username, password)
}

func execute(
	args []string,
	getenv func(string) string,
	input passwordInput,
	stdout io.Writer,
	stderr io.Writer,
	initialize adminInitializer,
) int {
	if err := run(args, getenv, input, stdout, initialize); err != nil {
		message := err.Error()
		if errors.Is(err, errInitializeAdministrator) {
			message = "initialize administrator: operation failed"
		}
		_, _ = fmt.Fprintln(stderr, message)
		return 1
	}
	return 0
}

func main() {
	input := terminalPasswordInput{fd: int(os.Stdin.Fd()), out: os.Stderr}
	if exitCode := execute(os.Args[1:], os.Getenv, input, os.Stdout, os.Stderr, initializeAdmin); exitCode != 0 {
		os.Exit(exitCode)
	}
}
