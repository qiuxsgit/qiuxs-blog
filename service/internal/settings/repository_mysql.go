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
	siteColumns                   = "id, site_name, author_name, author_bio, home_status, about_md, social_links_json, seo_default_title, seo_default_description, seo_default_image_media_id, filing_name, filing_number, lock_version, updated_at"
	selectSiteStatement           = "SELECT " + siteColumns + " FROM site_settings WHERE singleton_key = 1"
	insertSiteStatement           = "INSERT INTO site_settings (id, singleton_key, site_name, author_name, author_bio, home_status, about_md, social_links_json, seo_default_title, seo_default_description, seo_default_image_media_id, filing_name, filing_number, lock_version, created_at, updated_at) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)"
	updateSiteStatement           = "UPDATE site_settings SET site_name=?, author_name=?, author_bio=?, home_status=?, about_md=?, social_links_json=?, seo_default_title=?, seo_default_description=?, seo_default_image_media_id=?, filing_name=?, filing_number=?, lock_version=lock_version+1, updated_at=? WHERE singleton_key=1 AND lock_version=?"
	selectHotlinkStatement        = "SELECT id, allow_empty_referer FROM hotlink_settings WHERE singleton_key = 1"
	selectHotlinkEntriesStatement = "SELECT id, hostname, enabled FROM referer_allowlist ORDER BY hostname ASC, id ASC"
	updateHotlinkStatement        = "UPDATE hotlink_settings SET allow_empty_referer=?, updated_at=? WHERE singleton_key=1"
	lockHotlinkStatement          = "SELECT id FROM hotlink_settings WHERE singleton_key=1 FOR UPDATE"
	insertHotlinkStatement        = "INSERT INTO hotlink_settings (id, singleton_key, allow_empty_referer, created_at, updated_at) VALUES (?, 1, ?, ?, ?)"
	deleteHotlinkEntriesStatement = "DELETE FROM referer_allowlist"
	insertHotlinkEntryStatement   = "INSERT INTO referer_allowlist (id, hostname, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?)"
)

var (
	singletonUniquePattern        = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?uk_site_settings_singleton['`]")
	hotlinkSingletonUniquePattern = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?uk_hotlink_settings_singleton['`]")
	hotlinkHostnameUniquePattern  = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?uk_referer_allowlist_hostname['`]")
)

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

func (r *MySQLRepository) GetHotlinkPolicy(ctx context.Context) (HotlinkPolicy, error) {
	if err := r.validate(ctx); err != nil {
		return HotlinkPolicy{}, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return HotlinkPolicy{}, settingsDependencyError("begin hotlink policy read", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var singletonID int64
	var policy HotlinkPolicy
	if err := tx.QueryRowContext(ctx, selectHotlinkStatement).Scan(&singletonID, &policy.AllowEmptyReferer); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return HotlinkPolicy{}, ErrNotFound
		}
		return HotlinkPolicy{}, settingsDependencyError("get hotlink singleton", err)
	}
	if singletonID <= 0 {
		return HotlinkPolicy{}, settingsDependencyError("get hotlink singleton", errors.New("stored singleton ID is invalid"))
	}
	rows, err := tx.QueryContext(ctx, selectHotlinkEntriesStatement)
	if err != nil {
		return HotlinkPolicy{}, settingsDependencyError("get hotlink entries", err)
	}
	defer rows.Close()
	policy.Entries = make([]HotlinkEntry, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var entry HotlinkEntry
		if err := rows.Scan(&entry.ID, &entry.Hostname, &entry.Enabled); err != nil {
			return HotlinkPolicy{}, settingsDependencyError("get hotlink entries", err)
		}
		normalized, normalizeErr := NormalizeHostname(entry.Hostname)
		if entry.ID <= 0 || normalizeErr != nil || normalized != entry.Hostname {
			return HotlinkPolicy{}, settingsDependencyError("get hotlink entries", errors.New("stored hotlink entry is invalid"))
		}
		if _, duplicate := seen[entry.Hostname]; duplicate {
			return HotlinkPolicy{}, settingsDependencyError("get hotlink entries", errors.New("stored hotlink entry is duplicated"))
		}
		seen[entry.Hostname] = struct{}{}
		policy.Entries = append(policy.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return HotlinkPolicy{}, settingsDependencyError("get hotlink entries", err)
	}
	if err := rows.Close(); err != nil {
		return HotlinkPolicy{}, settingsDependencyError("get hotlink entries", err)
	}
	if err := tx.Commit(); err != nil {
		return HotlinkPolicy{}, settingsDependencyError("commit hotlink policy read", err)
	}
	committed = true
	return cloneHotlinkPolicy(policy), nil
}

func (r *MySQLRepository) ReplaceHotlinkPolicy(ctx context.Context, policy HotlinkPolicy, at time.Time) (HotlinkPolicy, error) {
	if err := r.validate(ctx); err != nil {
		return HotlinkPolicy{}, err
	}
	if err := validateHotlinkPolicyForWrite(policy); err != nil {
		return HotlinkPolicy{}, err
	}
	at = at.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return HotlinkPolicy{}, settingsDependencyError("begin hotlink policy replacement", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, updateHotlinkStatement, policy.AllowEmptyReferer, at)
	if err != nil {
		return HotlinkPolicy{}, classifyHotlinkWriteError("update hotlink singleton", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return HotlinkPolicy{}, settingsDependencyError("update hotlink singleton", err)
	}
	if rowsAffected > 1 {
		return HotlinkPolicy{}, settingsDependencyError("update hotlink singleton", errors.New("unexpected affected row count"))
	}
	if rowsAffected == 0 {
		var singletonID int64
		err := tx.QueryRowContext(ctx, lockHotlinkStatement).Scan(&singletonID)
		switch {
		case err == nil && singletonID <= 0:
			return HotlinkPolicy{}, settingsDependencyError("lock hotlink singleton", errors.New("stored singleton ID is invalid"))
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			if insertErr := r.ids.Insert(ctx, dbtable.HotlinkSettings, func(id int64) error {
				insertResult, execErr := tx.ExecContext(ctx, insertHotlinkStatement, id, policy.AllowEmptyReferer, at, at)
				if execErr != nil {
					return execErr
				}
				return requireOneAffectedRow(insertResult, "hotlink singleton insert")
			}); insertErr != nil {
				return HotlinkPolicy{}, classifyHotlinkWriteError("create hotlink singleton", insertErr)
			}
		default:
			return HotlinkPolicy{}, settingsDependencyError("lock hotlink singleton", err)
		}
	}

	if _, err := tx.ExecContext(ctx, deleteHotlinkEntriesStatement); err != nil {
		return HotlinkPolicy{}, settingsDependencyError("delete hotlink entries", err)
	}
	stored := HotlinkPolicy{AllowEmptyReferer: policy.AllowEmptyReferer, Entries: make([]HotlinkEntry, 0, len(policy.Entries))}
	for _, input := range policy.Entries {
		created := input
		if err := r.ids.Insert(ctx, dbtable.RefererAllowlist, func(id int64) error {
			created.ID = id
			insertResult, execErr := tx.ExecContext(ctx, insertHotlinkEntryStatement, id, created.Hostname, created.Enabled, at, at)
			if execErr != nil {
				return execErr
			}
			return requireOneAffectedRow(insertResult, "hotlink entry insert")
		}); err != nil {
			return HotlinkPolicy{}, classifyHotlinkWriteError("create hotlink entry", err)
		}
		stored.Entries = append(stored.Entries, created)
	}
	if err := tx.Commit(); err != nil {
		return HotlinkPolicy{}, settingsDependencyError("commit hotlink policy replacement", err)
	}
	committed = true
	return cloneHotlinkPolicy(stored), nil
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
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return nil, errors.New("social links must be a JSON array")
	}
	links := make([]SocialLink, 0)
	for decoder.More() {
		if len(links) == maximumSocials {
			return nil, errors.New("too many stored social links")
		}
		link, decodeErr := decodeSocialLink(decoder)
		if decodeErr != nil {
			return nil, decodeErr
		}
		links = append(links, link)
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return nil, errors.New("social links array is malformed")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("social links contain trailing JSON")
		}
		return nil, err
	}
	return links, nil
}

func decodeSocialLink(decoder *json.Decoder) (SocialLink, error) {
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return SocialLink{}, errors.New("social link must be a JSON object")
	}
	var link SocialLink
	seenLabel := false
	seenURL := false
	for decoder.More() {
		fieldToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return SocialLink{}, tokenErr
		}
		field, ok := fieldToken.(string)
		if !ok {
			return SocialLink{}, errors.New("social link field name is invalid")
		}
		switch field {
		case "label":
			if seenLabel {
				return SocialLink{}, errors.New("social link label field is duplicated")
			}
			seenLabel = true
			if err := decoder.Decode(&link.Label); err != nil {
				return SocialLink{}, err
			}
		case "url":
			if seenURL {
				return SocialLink{}, errors.New("social link URL field is duplicated")
			}
			seenURL = true
			if err := decoder.Decode(&link.URL); err != nil {
				return SocialLink{}, err
			}
		default:
			return SocialLink{}, errors.New("social link contains an unknown field")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return SocialLink{}, errors.New("social link object is malformed")
	}
	if !seenLabel || !seenURL {
		return SocialLink{}, errors.New("social link is missing a required field")
	}
	return link, nil
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

func validateHotlinkPolicyForWrite(policy HotlinkPolicy) error {
	seen := make(map[string]struct{}, len(policy.Entries))
	for _, entry := range policy.Entries {
		normalized, err := NormalizeHostname(entry.Hostname)
		if err != nil || normalized != entry.Hostname || entry.ID != 0 {
			return settingsDomainError("validate hotlink policy persistence", ErrInvalid, nil)
		}
		if _, duplicate := seen[entry.Hostname]; duplicate {
			return settingsDomainError("validate hotlink policy persistence", ErrInvalid, nil)
		}
		seen[entry.Hostname] = struct{}{}
	}
	return nil
}

func requireOneAffectedRow(result sql.Result, operation string) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return errors.New(operation + " affected an unexpected number of rows")
	}
	return nil
}

func classifyHotlinkWriteError(operation string, err error) error {
	switch {
	case isNamedMySQLDuplicate(err, hotlinkSingletonUniquePattern):
		return settingsDomainError(operation, ErrConflict, err)
	case isNamedMySQLDuplicate(err, hotlinkHostnameUniquePattern):
		return settingsDomainError(operation, ErrInvalid, err)
	default:
		return settingsDependencyError(operation, err)
	}
}

func isNamedMySQLDuplicate(err error, pattern *regexp.Regexp) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && pattern.MatchString(mysqlErr.Message)
}

var _ SiteRepository = (*MySQLRepository)(nil)
var _ HotlinkRepository = (*MySQLRepository)(nil)
