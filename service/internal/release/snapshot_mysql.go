package release

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/dbtable"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

const (
	snapshotSiteSQL         = "SELECT site_name, author_bio, about_md, social_links_json, filing_name, filing_number FROM site_settings WHERE singleton_key = 1 FOR UPDATE"
	snapshotArticleSQL      = "SELECT slug, draft_revision_id FROM articles WHERE id = ? AND state = 'active' FOR UPDATE"
	snapshotDraftSQL        = "SELECT id, revision_no, title, summary, cover_media_id, content_md, content_hash, lock_version FROM article_revisions WHERE id = ? AND article_id = ? AND status = 'editing' FOR UPDATE"
	snapshotTagsSQL         = "SELECT tag_id, tag_name, tag_slug, position FROM article_revision_tags WHERE revision_id = ? ORDER BY position ASC FOR UPDATE"
	snapshotMediaSQL        = "SELECT arm.media_id, m.public_key, arm.purpose, arm.position FROM article_revision_media arm JOIN media m ON m.id = arm.media_id AND m.state = 'active' WHERE arm.revision_id = ? ORDER BY arm.position ASC FOR UPDATE"
	freezeSnapshotSQL       = "UPDATE article_revisions SET status = 'frozen', reason = 'publish_snapshot', updated_at = ? WHERE id = ? AND status = 'editing' AND lock_version = ?"
	insertSnapshotDraftSQL  = "INSERT INTO article_revisions (id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at) VALUES (?, ?, ?, 'editing', 'draft', ?, ?, ?, ?, ?, 1, ?, ?)"
	insertSnapshotTagSQL    = "INSERT INTO article_revision_tags (id, revision_id, tag_id, tag_name, tag_slug, position, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
	insertSnapshotMediaSQL  = "INSERT INTO article_revision_media (id, revision_id, media_id, purpose, position, created_at) VALUES (?, ?, ?, ?, ?, ?)"
	replaceSnapshotDraftSQL = "UPDATE articles SET draft_revision_id = ?, updated_at = ? WHERE id = ? AND draft_revision_id = ? AND state = 'active'"
)

// MySQLSnapshotSource freezes the selected Stage 2 draft through the exact
// transaction executor owned by release creation.
type MySQLSnapshotSource struct {
	ids *idgen.Generator
	now func() time.Time
}

func NewMySQLSnapshotSource(ids *idgen.Generator, now func() time.Time) *MySQLSnapshotSource {
	return &MySQLSnapshotSource{ids: ids, now: now}
}

func (s *MySQLSnapshotSource) PrepareSnapshot(ctx context.Context, executor SnapshotExecutor, request SnapshotRequest) (PreparedSnapshot, error) {
	if s == nil || s.ids == nil || s.now == nil || nilReleaseInterface(ctx) || nilReleaseInterface(executor) {
		return PreparedSnapshot{}, errors.New("release snapshot source is not configured")
	}
	prepared := clonePreparedSnapshot(request.Base)
	if prepared.Articles == nil {
		prepared.Articles = make([]ArticleSnapshot, 0)
	}
	if request.CurrentReleaseID == 0 || request.Mode == PublishSettings {
		site, err := loadMutableSiteSnapshot(ctx, executor)
		if err != nil {
			return PreparedSnapshot{}, err
		}
		prepared.Site = site
	}

	switch request.Mode {
	case PublishSettings:
	case UnpublishArticle:
		index := snapshotArticleIndex(prepared.Articles, request.ArticleID)
		if index < 0 {
			return PreparedSnapshot{}, ErrInvalidSnapshot
		}
		prepared.Articles = append(prepared.Articles[:index:index], prepared.Articles[index+1:]...)
	case PublishArticle:
		article, err := s.freezeArticle(ctx, executor, request.ArticleID)
		if err != nil {
			return PreparedSnapshot{}, err
		}
		index := snapshotArticleIndex(prepared.Articles, request.ArticleID)
		if index >= 0 {
			article.Slug = prepared.Articles[index].Slug
			prepared.Articles[index] = article
		} else {
			prepared.Articles = append(prepared.Articles, article)
		}
	default:
		return PreparedSnapshot{}, ErrInvalidSnapshot
	}
	checksum, err := preparedSnapshotChecksum(prepared)
	if err != nil {
		return PreparedSnapshot{}, err
	}
	prepared.Checksum = checksum
	return prepared, nil
}

func loadMutableSiteSnapshot(ctx context.Context, executor SnapshotExecutor) (SiteSnapshot, error) {
	var site SiteSnapshot
	var raw []byte
	err := executor.QueryRowContext(ctx, snapshotSiteSQL).Scan(
		&site.Name, &site.AuthorBio, &site.AboutMarkdown, &raw, &site.FilingName, &site.FilingNumber,
	)
	if errors.Is(err, sql.ErrNoRows) {
		defaults := settings.DefaultSite()
		return SiteSnapshot{
			Name: defaults.SiteName, AuthorBio: defaults.AuthorBio, AboutMarkdown: defaults.AboutMD,
			FilingName: defaults.FilingName, FilingNumber: defaults.FilingNumber,
			SocialLinks: make([]SocialLink, 0),
		}, nil
	}
	if err != nil {
		return SiteSnapshot{}, err
	}
	if !utf8.Valid(raw) {
		return SiteSnapshot{}, errors.New("stored site social links are invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	site.SocialLinks, err = decodeSocialLinks(decoder)
	if err != nil || site.SocialLinks == nil {
		return SiteSnapshot{}, errors.New("stored site social links are invalid")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return SiteSnapshot{}, errors.New("stored site social links have trailing data")
	}
	return site, nil
}

func (s *MySQLSnapshotSource) freezeArticle(ctx context.Context, executor SnapshotExecutor, articleID int64) (ArticleSnapshot, error) {
	article := ArticleSnapshot{ArticleID: articleID}
	var revisionNo, lockVersion int64
	var cover sql.NullInt64
	if err := executor.QueryRowContext(ctx, snapshotArticleSQL, articleID).Scan(&article.Slug, &article.RevisionID); err != nil {
		return ArticleSnapshot{}, err
	}
	if err := executor.QueryRowContext(ctx, snapshotDraftSQL, article.RevisionID, articleID).Scan(
		&article.RevisionID, &revisionNo,
		&article.Title, &article.Summary, &cover, &article.ContentMarkdown, &article.ContentHash, &lockVersion,
	); err != nil {
		return ArticleSnapshot{}, err
	}
	if article.ArticleID != articleID || article.ArticleID <= 0 || article.RevisionID <= 0 || revisionNo <= 0 || lockVersion <= 0 {
		return ArticleSnapshot{}, errors.New("stored publish draft identity is invalid")
	}
	tags, err := loadSnapshotTags(ctx, executor, article.RevisionID)
	if err != nil {
		return ArticleSnapshot{}, err
	}
	references, err := loadSnapshotMedia(ctx, executor, article.RevisionID)
	if err != nil {
		return ArticleSnapshot{}, err
	}
	content := revision.Content{
		Title: article.Title, Summary: article.Summary, ContentMD: article.ContentMarkdown,
	}
	publicKeys, err := revision.ValidateDraft(content)
	if err != nil {
		return ArticleSnapshot{}, err
	}
	if err := revision.ValidateFreezable(content); err != nil {
		return ArticleSnapshot{}, err
	}
	if !validSnapshotAssociations(tags, references, cover, publicKeys) {
		return ArticleSnapshot{}, errors.New("stored publish draft associations are invalid")
	}
	var coverMedia *media.Media
	if cover.Valid {
		coverMedia = &media.Media{ID: cover.Int64, PublicKey: references[0].PublicKey}
	}
	actualHash := revision.ComputeHash(revision.PreparedContent{
		Title: article.Title, Summary: article.Summary, Cover: coverMedia, ContentMD: article.ContentMarkdown,
		Tags: tags, Media: references,
	})
	if actualHash != article.ContentHash {
		return ArticleSnapshot{}, errors.New("stored publish draft content hash is invalid")
	}
	article.ContentHash = actualHash
	at := s.now().UTC().Truncate(time.Microsecond)
	if at.IsZero() {
		return ArticleSnapshot{}, errors.New("release snapshot clock is invalid")
	}
	if err := snapshotExecOne(ctx, executor, freezeSnapshotSQL, at, article.RevisionID, lockVersion); err != nil {
		return ArticleSnapshot{}, err
	}
	var nextID int64
	if err := s.ids.Insert(ctx, dbtable.ArticleRevisions, func(id int64) error {
		nextID = id
		_, insertErr := executor.ExecContext(ctx, insertSnapshotDraftSQL,
			id, article.ArticleID, revisionNo+1, article.Title, article.Summary, nullableSnapshotID(cover),
			article.ContentMarkdown, article.ContentHash, at, at,
		)
		return insertErr
	}); err != nil {
		return ArticleSnapshot{}, err
	}
	for _, snapshot := range tags {
		item := snapshot
		if err := s.ids.Insert(ctx, dbtable.ArticleRevisionTags, func(id int64) error {
			_, insertErr := executor.ExecContext(ctx, insertSnapshotTagSQL, id, nextID, item.TagID, item.Name, item.Slug, item.Position, at)
			return insertErr
		}); err != nil {
			return ArticleSnapshot{}, err
		}
	}
	for _, reference := range references {
		item := reference
		if err := s.ids.Insert(ctx, dbtable.ArticleRevisionMedia, func(id int64) error {
			_, insertErr := executor.ExecContext(ctx, insertSnapshotMediaSQL, id, nextID, item.MediaID, item.Purpose, item.Position, at)
			return insertErr
		}); err != nil {
			return ArticleSnapshot{}, err
		}
	}
	if err := snapshotExecOne(ctx, executor, replaceSnapshotDraftSQL, nextID, at, article.ArticleID, article.RevisionID); err != nil {
		return ArticleSnapshot{}, err
	}
	article.PublishedAt = at
	article.ContentHash = "sha256:" + article.ContentHash
	article.Tags = make([]TagSnapshot, len(tags))
	for index, snapshot := range tags {
		article.Tags[index] = TagSnapshot{ID: snapshot.TagID, Name: snapshot.Name, Slug: snapshot.Slug}
	}
	sort.Slice(article.Tags, func(left, right int) bool {
		if article.Tags[left].Slug == article.Tags[right].Slug {
			return article.Tags[left].ID < article.Tags[right].ID
		}
		return article.Tags[left].Slug < article.Tags[right].Slug
	})
	return article, nil
}

func validSnapshotAssociations(tags []tag.Snapshot, references []media.Reference, cover sql.NullInt64, publicKeys []string) bool {
	if len(tags) > revision.MaxTagCount || len(references) > revision.MaxBodyMediaCount+1 {
		return false
	}
	seenTags := make(map[int64]struct{}, len(tags))
	for index, item := range tags {
		if item.TagID <= 0 || item.Position != index || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Slug) == "" {
			return false
		}
		if _, duplicate := seenTags[item.TagID]; duplicate {
			return false
		}
		seenTags[item.TagID] = struct{}{}
	}
	coverSeen := false
	contentCount := 0
	for index, item := range references {
		if item.MediaID <= 0 || strings.TrimSpace(item.PublicKey) == "" || item.Position != index {
			return false
		}
		switch item.Purpose {
		case "cover":
			if coverSeen || index != 0 || !cover.Valid || cover.Int64 != item.MediaID {
				return false
			}
			coverSeen = true
		case "content":
			if contentCount >= len(publicKeys) || item.PublicKey != publicKeys[contentCount] {
				return false
			}
			contentCount++
			if contentCount > revision.MaxBodyMediaCount {
				return false
			}
		default:
			return false
		}
	}
	return cover.Valid == coverSeen && (!cover.Valid || cover.Int64 > 0) && contentCount == len(publicKeys)
}

func loadSnapshotTags(ctx context.Context, executor SnapshotExecutor, revisionID int64) ([]tag.Snapshot, error) {
	rows, err := executor.QueryContext(ctx, snapshotTagsSQL, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]tag.Snapshot, 0)
	for rows.Next() {
		var item tag.Snapshot
		if err := rows.Scan(&item.TagID, &item.Name, &item.Slug, &item.Position); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadSnapshotMedia(ctx context.Context, executor SnapshotExecutor, revisionID int64) ([]media.Reference, error) {
	rows, err := executor.QueryContext(ctx, snapshotMediaSQL, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]media.Reference, 0)
	for rows.Next() {
		var item media.Reference
		if err := rows.Scan(&item.MediaID, &item.PublicKey, &item.Purpose, &item.Position); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func snapshotExecOne(ctx context.Context, executor SnapshotExecutor, statement string, arguments ...any) error {
	result, err := executor.ExecContext(ctx, statement, arguments...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("release snapshot write did not affect exactly one row")
	}
	return nil
}

func snapshotArticleIndex(articles []ArticleSnapshot, articleID int64) int {
	for index := range articles {
		if articles[index].ArticleID == articleID {
			return index
		}
	}
	return -1
}

func nullableSnapshotID(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

var _ SnapshotSource = (*MySQLSnapshotSource)(nil)
