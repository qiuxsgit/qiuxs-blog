package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
)

const adminColumns = "id, username, password_hash, state"

var (
	adminUsernamePattern  = regexp.MustCompile(`^[a-z0-9._-]{3,64}$`)
	adminUniqueKeyPattern = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?(?:uk_admins_username|uk_admins_singleton)['`]")
)

// MySQLRepository stores administrators in MySQL and obtains IDs from the
// process-wide generator before every insert.
type MySQLRepository struct {
	db      *sql.DB
	ids     *idgen.Generator
	initErr error
}

func NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository {
	repo := &MySQLRepository{db: db, ids: ids}
	if db == nil {
		repo.initErr = errors.New("admin database is required")
	} else if ids == nil {
		repo.initErr = errors.New("admin ID generator is required")
	}
	return repo
}

// NormalizeUsername returns the canonical administrator username.
func NormalizeUsername(username string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(username))
	if !adminUsernamePattern.MatchString(normalized) {
		return "", errors.New("admin username must match [a-z0-9._-]{3,64}")
	}
	return normalized, nil
}

func (r *MySQLRepository) Count(ctx context.Context) (int, error) {
	if err := r.databaseError(); err != nil {
		return 0, err
	}

	var count int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return count, nil
}

func (r *MySQLRepository) Create(ctx context.Context, username, passwordHash string) (Admin, error) {
	if r == nil {
		return Admin{}, errors.New("admin repository is required")
	}
	if r.initErr != nil {
		return Admin{}, r.initErr
	}

	normalized, err := NormalizeUsername(username)
	if err != nil {
		return Admin{}, err
	}

	admin := Admin{Username: normalized, PasswordHash: passwordHash, State: "active"}
	err = r.ids.Insert(ctx, "admins", func(id int64) error {
		admin.ID = id
		_, insertErr := r.db.ExecContext(
			ctx,
			"INSERT INTO admins (id, singleton_key, username, password_hash, state) VALUES (?, ?, ?, ?, ?)",
			id,
			1,
			normalized,
			passwordHash,
			admin.State,
		)
		return insertErr
	})
	if err != nil {
		if isAdminUniqueConflict(err) {
			return Admin{}, fmt.Errorf("create admin: %w", errors.Join(ErrAdminAlreadyExists, err))
		}
		return Admin{}, fmt.Errorf("create admin: %w", err)
	}
	return admin, nil
}

func (r *MySQLRepository) FindByUsername(ctx context.Context, username string) (Admin, error) {
	if err := r.databaseError(); err != nil {
		return Admin{}, err
	}
	normalized, err := NormalizeUsername(username)
	if err != nil {
		return Admin{}, err
	}

	return scanAdmin(
		r.db.QueryRowContext(ctx, "SELECT "+adminColumns+" FROM admins WHERE username = ?", normalized),
		"find admin by username",
	)
}

func (r *MySQLRepository) FindByID(ctx context.Context, id int64) (Admin, error) {
	if err := r.databaseError(); err != nil {
		return Admin{}, err
	}
	return scanAdmin(
		r.db.QueryRowContext(ctx, "SELECT "+adminColumns+" FROM admins WHERE id = ?", id),
		"find admin by ID",
	)
}

func (r *MySQLRepository) UpdateLastLogin(ctx context.Context, id int64, at time.Time) error {
	if err := r.databaseError(); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, "UPDATE admins SET last_login_at = ? WHERE id = ?", at.UTC(), id); err != nil {
		return fmt.Errorf("update admin last login: %w", err)
	}
	return nil
}

func (r *MySQLRepository) databaseError() error {
	if r == nil {
		return errors.New("admin repository is required")
	}
	if r.db == nil {
		if r.initErr != nil {
			return r.initErr
		}
		return errors.New("admin database is required")
	}
	return nil
}

func scanAdmin(row *sql.Row, operation string) (Admin, error) {
	var admin Admin
	if err := row.Scan(&admin.ID, &admin.Username, &admin.PasswordHash, &admin.State); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Admin{}, fmt.Errorf("%s: %w", operation, ErrAdminNotFound)
		}
		return Admin{}, fmt.Errorf("%s: %w", operation, err)
	}
	return admin, nil
}

func isAdminUniqueConflict(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && adminUniqueKeyPattern.MatchString(mysqlErr.Message)
}
