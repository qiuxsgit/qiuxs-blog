package builder

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"reflect"
	"regexp"

	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/dbtable"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
)

const (
	builderColumns          = "id, name, base_url, username, token_ciphertext, job_name, enabled"
	selectBuilderSQL        = "SELECT " + builderColumns + " FROM builder_config WHERE singleton_key = 1"
	selectBuilderForUpdate  = selectBuilderSQL + " FOR UPDATE"
	insertBuilderSQL        = "INSERT INTO builder_config (id, singleton_key, name, base_url, username, token_ciphertext, job_name, enabled) VALUES (?, 1, ?, ?, ?, ?, ?, ?)"
	updateBuilderSQL        = "UPDATE builder_config SET name = ?, base_url = ?, username = ?, token_ciphertext = ?, job_name = ?, enabled = ? WHERE id = ? AND singleton_key = 1"
	minimumCiphertextLength = 29
)

var builderSingletonUnique = regexp.MustCompile("(?i)(?:key ['`](?:[^'`.]+\\.)?uk_builder_config_singleton['`]|constraint ['`]?uk_builder_config_singleton['`]?)")

type MySQLRepository struct {
	db      *sql.DB
	ids     *idgen.Generator
	box     *platform.SecretBox
	initErr error
}

func NewMySQLRepository(db *sql.DB, ids *idgen.Generator, box *platform.SecretBox) *MySQLRepository {
	repository := &MySQLRepository{db: db, ids: ids, box: box}
	switch {
	case db == nil:
		repository.initErr = errors.New("builder database is required")
	case ids == nil:
		repository.initErr = errors.New("builder ID generator is required")
	case box == nil:
		repository.initErr = errors.New("builder secret box is required")
	}
	return repository
}

func (r *MySQLRepository) Load(ctx context.Context) (StoredConfig, error) {
	if err := r.validate(ctx); err != nil {
		return StoredConfig{}, err
	}
	stored, err := scanStoredBuilder(r.db.QueryRowContext(ctx, selectBuilderSQL))
	if errors.Is(err, sql.ErrNoRows) {
		return StoredConfig{}, builderDomain("load builder configuration", ErrNotFound)
	}
	if err != nil {
		return StoredConfig{}, err
	}
	return cloneStoredConfig(stored), nil
}

func (r *MySQLRepository) Save(ctx context.Context, input ConfigInput) (ConfigView, error) {
	if err := r.validate(ctx); err != nil {
		return ConfigView{}, err
	}
	if err := ValidateConfig(input); err != nil {
		return ConfigView{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ConfigView{}, builderDependency("begin builder configuration save", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, err := scanStoredBuilder(tx.QueryRowContext(ctx, selectBuilderForUpdate))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if input.Token == "" {
			return ConfigView{}, builderDomain("create builder configuration", ErrInvalidConfig)
		}
		created, createErr := r.insert(ctx, tx, input)
		if createErr != nil {
			return ConfigView{}, createErr
		}
		current = created
	case err != nil:
		return ConfigView{}, err
	default:
		updated, updateErr := r.update(ctx, tx, current, input)
		if updateErr != nil {
			return ConfigView{}, updateErr
		}
		current = updated
	}

	if err := tx.Commit(); err != nil {
		return ConfigView{}, builderDependency("commit builder configuration save", err)
	}
	committed = true
	return current.ConfigView, nil
}

func (r *MySQLRepository) insert(ctx context.Context, tx *sql.Tx, input ConfigInput) (StoredConfig, error) {
	ciphertext, err := r.box.Seal([]byte(input.Token))
	if err != nil {
		return StoredConfig{}, builderDependency("encrypt builder token", err)
	}
	var id int64
	err = r.ids.Insert(ctx, dbtable.BuilderConfig, func(candidate int64) error {
		id = candidate
		result, insertErr := tx.ExecContext(ctx, insertBuilderSQL,
			candidate, input.Name, input.BaseURL, input.Username, ciphertext, input.JobName, input.Enabled,
		)
		if insertErr != nil {
			return insertErr
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if rows != 1 {
			return errors.New("unexpected builder insert affected row count")
		}
		return nil
	})
	if err != nil {
		if isBuilderSingletonConflict(err) {
			return StoredConfig{}, builderDomain("create builder configuration", ErrConflict)
		}
		return StoredConfig{}, builderDependency("create builder configuration", err)
	}
	expected := storedFromInput(id, input, ciphertext)
	return reloadExpectedBuilder(ctx, tx, expected, "reload created builder configuration")
}

func (r *MySQLRepository) update(ctx context.Context, tx *sql.Tx, current StoredConfig, input ConfigInput) (StoredConfig, error) {
	ciphertext := current.EncryptedToken
	if input.Token != "" {
		var err error
		ciphertext, err = r.box.Seal([]byte(input.Token))
		if err != nil {
			return StoredConfig{}, builderDependency("encrypt builder token", err)
		}
	}
	result, err := tx.ExecContext(ctx, updateBuilderSQL,
		input.Name, input.BaseURL, input.Username, ciphertext, input.JobName, input.Enabled, current.ID,
	)
	if err != nil {
		return StoredConfig{}, builderDependency("update builder configuration", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return StoredConfig{}, builderDependency("update builder configuration", err)
	}
	if rows != 0 && rows != 1 {
		return StoredConfig{}, builderDependency("update builder configuration", errors.New("unexpected affected row count"))
	}
	expected := storedFromInput(current.ID, input, ciphertext)
	return reloadExpectedBuilder(ctx, tx, expected, "reload updated builder configuration")
}

func reloadExpectedBuilder(ctx context.Context, tx *sql.Tx, expected StoredConfig, operation string) (StoredConfig, error) {
	stored, err := scanStoredBuilder(tx.QueryRowContext(ctx, selectBuilderForUpdate))
	if err != nil {
		return StoredConfig{}, err
	}
	if stored != expected {
		return StoredConfig{}, builderDependency(operation, errors.New("stored builder configuration does not match write"))
	}
	return stored, nil
}

type builderScanner interface{ Scan(...any) error }

func scanStoredBuilder(scanner builderScanner) (StoredConfig, error) {
	var stored StoredConfig
	if err := scanner.Scan(&stored.ID, &stored.Name, &stored.BaseURL, &stored.Username, &stored.EncryptedToken, &stored.JobName, &stored.Enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return StoredConfig{}, sql.ErrNoRows
		}
		return StoredConfig{}, builderDependency("scan builder configuration", err)
	}
	stored.TokenConfigured = stored.EncryptedToken != ""
	if stored.ID <= 0 || ValidateConfig(ConfigInput{
		Name: stored.Name, BaseURL: stored.BaseURL, Username: stored.Username, JobName: stored.JobName, Enabled: stored.Enabled,
	}) != nil || !validEncryptedToken(stored.EncryptedToken) {
		return StoredConfig{}, builderDependency("validate stored builder configuration", errors.New("stored builder configuration is invalid"))
	}
	return stored, nil
}

func validEncryptedToken(encoded string) bool {
	decoded, err := base64.RawStdEncoding.Strict().DecodeString(encoded)
	return err == nil && len(decoded) >= minimumCiphertextLength && base64.RawStdEncoding.EncodeToString(decoded) == encoded
}

func storedFromInput(id int64, input ConfigInput, ciphertext string) StoredConfig {
	return StoredConfig{ConfigView: ConfigView{
		ID: id, Name: input.Name, BaseURL: input.BaseURL, Username: input.Username,
		JobName: input.JobName, Enabled: input.Enabled, TokenConfigured: true,
	}, EncryptedToken: ciphertext}
}

func cloneStoredConfig(stored StoredConfig) StoredConfig { return stored }

func isBuilderSingletonConflict(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && builderSingletonUnique.MatchString(mysqlErr.Message)
}

func (r *MySQLRepository) validate(ctx context.Context) error {
	if r == nil {
		return errors.New("builder repository is required")
	}
	if r.initErr != nil {
		return r.initErr
	}
	if r.db == nil || r.ids == nil || r.box == nil {
		return errors.New("builder repository is not configured")
	}
	if nilBuilderInterface(ctx) {
		return builderDomain("use builder repository", ErrInvalidConfig)
	}
	return nil
}

func nilBuilderInterface(value any) bool {
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
