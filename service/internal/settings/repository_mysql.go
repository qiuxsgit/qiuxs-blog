package settings

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/dbtable"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
)

const (
	siteColumns         = "id, site_name, author_name, author_bio, home_status, about_md, social_links_json, seo_default_title, seo_default_description, seo_default_image_media_id, filing_name, filing_number, lock_version, updated_at"
	selectSiteStatement = "SELECT " + siteColumns + " FROM site_settings WHERE singleton_key = 1"
	insertSiteStatement = "INSERT INTO site_settings (id, singleton_key, site_name, author_name, author_bio, home_status, about_md, social_links_json, seo_default_title, seo_default_description, seo_default_image_media_id, filing_name, filing_number, lock_version, created_at, updated_at) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)"
	updateSiteStatement = "UPDATE site_settings SET site_name=?, author_name=?, author_bio=?, home_status=?, about_md=?, social_links_json=?, seo_default_title=?, seo_default_description=?, seo_default_image_media_id=?, filing_name=?, filing_number=?, lock_version=lock_version+1, updated_at=? WHERE singleton_key=1 AND lock_version=?"
)

var singletonUniquePattern = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?uk_site_settings_singleton['`]")

type MySQLRepository struct {
	db      *sql.DB
	ids     *idgen.Generator
	initErr error
}

func NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository {
	repository := &MySQLRepository{db: db, ids: ids}
	if db == nil {
		repository.initErr = errors.New("settings database is required")
	} else if ids == nil {
		repository.initErr = errors.New("settings ID generator is required")
	}
	return repository
}

func (r *MySQLRepository) GetSite(ctx context.Context) (Site, error) {
	if err := r.validate(ctx); err != nil {
		return Site{}, err
	}
	return scanStoredSite(r.db.QueryRowContext(ctx, selectSiteStatement), "get site settings")
}

func (r *MySQLRepository) CreateSite(ctx context.Context, site Site, at time.Time) (Site, error) {
	if err := r.validate(ctx); err != nil {
		return Site{}, err
	}
	if err := validateNormalizedSite(site); err != nil {
		return Site{}, err
	}
	socialsJSON, err := encodeSocialLinks(site.SocialLinks)
	if err != nil {
		return Site{}, settingsDependencyError("encode site social links", err)
	}
	at = at.UTC()
	created := cloneSite(site)
	created.ID = 0
	created.LockVersion = 1
	created.UpdatedAt = at
	err = r.ids.Insert(ctx, dbtable.SiteSettings, func(id int64) error {
		created.ID = id
		result, insertErr := r.db.ExecContext(ctx, insertSiteStatement,
			id, created.SiteName, created.AuthorName, created.AuthorBio, created.HomeStatus, created.AboutMD,
			socialsJSON, created.SEODefaultTitle, created.SEODefaultDescription, nullableID(created.SEODefaultImageMediaID),
			created.FilingName, created.FilingNumber, at, at,
		)
		if insertErr != nil {
			return insertErr
		}
		rowsAffected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if rowsAffected != 1 {
			return errors.New("unexpected site insert affected row count")
		}
		return nil
	})
	if err != nil {
		if isSingletonDuplicate(err) {
			return Site{}, settingsDomainError("create site settings", ErrConflict, err)
		}
		return Site{}, settingsDependencyError("create site settings", err)
	}
	return cloneSite(created), nil
}

func (r *MySQLRepository) UpdateSite(ctx context.Context, site Site, expectedLock int64, at time.Time) (Site, error) {
	if err := r.validate(ctx); err != nil {
		return Site{}, err
	}
	if expectedLock <= 0 {
		return Site{}, settingsDomainError("update site settings", ErrInvalid, nil)
	}
	if err := validateNormalizedSite(site); err != nil {
		return Site{}, err
	}
	socialsJSON, err := encodeSocialLinks(site.SocialLinks)
	if err != nil {
		return Site{}, settingsDependencyError("encode site social links", err)
	}
	at = at.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Site{}, settingsDependencyError("begin site settings update", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, updateSiteStatement,
		site.SiteName, site.AuthorName, site.AuthorBio, site.HomeStatus, site.AboutMD, socialsJSON,
		site.SEODefaultTitle, site.SEODefaultDescription, nullableID(site.SEODefaultImageMediaID),
		site.FilingName, site.FilingNumber, at, expectedLock,
	)
	if err != nil {
		return Site{}, settingsDependencyError("update site settings", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Site{}, settingsDependencyError("update site settings", err)
	}
	if rowsAffected == 0 {
		return Site{}, settingsDomainError("update site settings", ErrConflict, nil)
	}
	if rowsAffected != 1 {
		return Site{}, settingsDependencyError("update site settings", errors.New("unexpected affected row count"))
	}
	stored, err := scanStoredSite(tx.QueryRowContext(ctx, selectSiteStatement), "reload site settings")
	if err != nil {
		return Site{}, err
	}
	if err := tx.Commit(); err != nil {
		return Site{}, settingsDependencyError("commit site settings update", err)
	}
	committed = true
	return cloneSite(stored), nil
}

func (r *MySQLRepository) validate(ctx context.Context) error {
	if r == nil {
		return errors.New("settings repository is required")
	}
	if r.initErr != nil {
		return r.initErr
	}
	if r.db == nil || r.ids == nil {
		return errors.New("settings repository is not configured")
	}
	if nilDependency(ctx) {
		return settingsDomainError("validate settings repository request", ErrInvalid, nil)
	}
	return nil
}

type siteScanner interface {
	Scan(...any) error
}

func scanStoredSite(scanner siteScanner, operation string) (Site, error) {
	var site Site
	var socialJSON []byte
	var imageID sql.NullInt64
	if err := scanner.Scan(
		&site.ID, &site.SiteName, &site.AuthorName, &site.AuthorBio, &site.HomeStatus, &site.AboutMD,
		&socialJSON, &site.SEODefaultTitle, &site.SEODefaultDescription, &imageID,
		&site.FilingName, &site.FilingNumber, &site.LockVersion, &site.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Site{}, ErrNotFound
		}
		return Site{}, settingsDependencyError(operation, err)
	}
	socials, err := decodeSocialLinks(socialJSON)
	if err != nil {
		return Site{}, settingsDependencyError(operation, err)
	}
	site.SocialLinks = socials
	site.UpdatedAt = site.UpdatedAt.UTC()
	if imageID.Valid {
		site.SEODefaultImageMediaID = int64Pointer(imageID.Int64)
	}
	if site.ID <= 0 || site.LockVersion <= 0 || site.UpdatedAt.IsZero() {
		return Site{}, settingsDependencyError(operation, errors.New("stored site settings are invalid"))
	}
	if err := validateStoredSite(site); err != nil {
		return Site{}, settingsDependencyError(operation, errors.New("stored site settings are invalid"))
	}
	return cloneSite(site), nil
}

func encodeSocialLinks(links []SocialLink) (string, error) {
	if links == nil {
		links = make([]SocialLink, 0)
	}
	encoded, err := json.Marshal(links)
	return string(encoded), err
}

func decodeSocialLinks(raw []byte) ([]SocialLink, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var links []SocialLink
	if err := decoder.Decode(&links); err != nil {
		return nil, err
	}
	if links == nil {
		return nil, errors.New("social links must be a JSON array")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("social links contain trailing JSON")
		}
		return nil, err
	}
	return links, nil
}

func nullableID(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}

func int64Pointer(value int64) *int64 { return &value }

func isSingletonDuplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && singletonUniquePattern.MatchString(mysqlErr.Message)
}

var _ SiteRepository = (*MySQLRepository)(nil)
