package release

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
)

type checksumPayload struct {
	Site     BundleSite      `json:"site"`
	Tags     []BundleTag     `json:"tags"`
	Articles []BundleArticle `json:"articles"`
}

// assembleBundle copies an immutable prepared snapshot into its portable
// representation. Release identity and generation time are intentionally not
// part of the checksum payload.
func assembleBundle(releaseID int64, generatedAt time.Time, prepared PreparedSnapshot) (Bundle, error) {
	if releaseID <= 0 || generatedAt.IsZero() || generatedAt.Location() != time.UTC || generatedAt.Nanosecond()%1000 != 0 {
		return Bundle{}, releaseDomain("assemble release bundle", ErrInvalidSnapshot)
	}
	payload, err := bundlePayloadFromPrepared(prepared)
	if err != nil {
		return Bundle{}, err
	}
	checksum, err := checksumForPayload(payload)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{
		SchemaVersion: 1,
		ReleaseID:     releaseID,
		GeneratedAt:   generatedAt,
		Site:          payload.Site,
		Tags:          payload.Tags,
		Articles:      payload.Articles,
		Checksum:      checksum,
	}, nil
}

// preparedSnapshotChecksum independently derives the persisted checksum from
// the exact public site/tag/article payload. The input Checksum is ignored.
func preparedSnapshotChecksum(prepared PreparedSnapshot) (string, error) {
	payload, err := bundlePayloadFromPrepared(prepared)
	if err != nil {
		return "", err
	}
	return checksumForPayload(payload)
}

func verifyPreparedSnapshotChecksum(prepared PreparedSnapshot) error {
	computed, err := preparedSnapshotChecksum(prepared)
	if err != nil || !releaseChecksumPattern.MatchString(prepared.Checksum) ||
		subtle.ConstantTimeCompare([]byte(computed), []byte(prepared.Checksum)) != 1 {
		return releaseDomain("verify release snapshot checksum", ErrInvalidSnapshot)
	}
	return nil
}

func encodeCanonicalBundle(bundle Bundle) ([]byte, string, error) {
	copy, payload, err := normalizeBundle(bundle)
	if err != nil {
		return nil, "", err
	}
	computed, err := checksumForPayload(payload)
	if err != nil || !releaseChecksumPattern.MatchString(copy.Checksum) ||
		subtle.ConstantTimeCompare([]byte(computed), []byte(copy.Checksum)) != 1 {
		return nil, "", releaseDomain("verify release bundle checksum", ErrInvalidSnapshot)
	}
	encoded, err := canonicalJSON(copy)
	if err != nil {
		return nil, "", releaseDependency("encode canonical release bundle", err)
	}
	return encoded, copy.Checksum, nil
}

func bundlePayloadFromPrepared(prepared PreparedSnapshot) (checksumPayload, error) {
	prepared = normalizePreparedSnapshot(prepared)
	if len(prepared.Articles) > maxReleaseArticles || settings.ValidateReleaseSnapshot(toSettingsSite(prepared.Site)) != nil {
		return checksumPayload{}, releaseDomain("assemble release bundle", ErrInvalidSnapshot)
	}
	articles := cloneArticleSnapshots(prepared.Articles)
	for index := range articles {
		sort.Slice(articles[index].Tags, func(i, j int) bool {
			if articles[index].Tags[i].Slug == articles[index].Tags[j].Slug {
				return articles[index].Tags[i].ID < articles[index].Tags[j].ID
			}
			return articles[index].Tags[i].Slug < articles[index].Tags[j].Slug
		})
		if !validArticleSnapshot(articles[index]) {
			return checksumPayload{}, releaseDomain("assemble release bundle", ErrInvalidSnapshot)
		}
	}
	sort.Slice(articles, func(i, j int) bool {
		if articles[i].PublishedAt.Equal(articles[j].PublishedAt) {
			return articles[i].ArticleID < articles[j].ArticleID
		}
		return articles[i].PublishedAt.Before(articles[j].PublishedAt)
	})

	seenArticles := make(map[int64]struct{}, len(articles))
	seenArticleSlugs := make(map[string]struct{}, len(articles))
	tagsByID := make(map[int64]TagSnapshot)
	tagIDsBySlug := make(map[string]int64)
	bundleArticles := make([]BundleArticle, 0, len(articles))
	for _, article := range articles {
		if _, exists := seenArticles[article.ArticleID]; exists {
			return checksumPayload{}, releaseDomain("assemble release bundle", ErrInvalidSnapshot)
		}
		if _, exists := seenArticleSlugs[article.Slug]; exists {
			return checksumPayload{}, releaseDomain("assemble release bundle", ErrInvalidSnapshot)
		}
		seenArticles[article.ArticleID] = struct{}{}
		seenArticleSlugs[article.Slug] = struct{}{}
		tagSlugs := make([]string, len(article.Tags))
		for index, tag := range article.Tags {
			if prior, exists := tagsByID[tag.ID]; exists && prior != tag {
				return checksumPayload{}, releaseDomain("assemble release bundle", ErrInvalidSnapshot)
			}
			if priorID, exists := tagIDsBySlug[tag.Slug]; exists && priorID != tag.ID {
				return checksumPayload{}, releaseDomain("assemble release bundle", ErrInvalidSnapshot)
			}
			tagsByID[tag.ID] = tag
			tagIDsBySlug[tag.Slug] = tag.ID
			tagSlugs[index] = tag.Slug
		}
		bundleArticles = append(bundleArticles, BundleArticle{
			ArticleID: article.ArticleID, RevisionID: article.RevisionID, Slug: article.Slug,
			Title: article.Title, Summary: article.Summary, ContentMarkdown: article.ContentMarkdown,
			ContentHash: article.ContentHash, PublishedAt: article.PublishedAt, Tags: tagSlugs,
		})
	}
	bundleTags := make([]BundleTag, 0, len(tagsByID))
	for _, tag := range tagsByID {
		bundleTags = append(bundleTags, BundleTag{ID: tag.ID, Name: tag.Name, Slug: tag.Slug})
	}
	sort.Slice(bundleTags, func(i, j int) bool {
		if bundleTags[i].Slug == bundleTags[j].Slug {
			return bundleTags[i].ID < bundleTags[j].ID
		}
		return bundleTags[i].Slug < bundleTags[j].Slug
	})
	socials := append(make([]SocialLink, 0, len(prepared.Site.SocialLinks)), prepared.Site.SocialLinks...)
	return checksumPayload{
		Site: BundleSite{
			Name: prepared.Site.Name, AuthorBio: prepared.Site.AuthorBio,
			AboutMarkdown: prepared.Site.AboutMarkdown, FilingName: prepared.Site.FilingName,
			FilingNumber: prepared.Site.FilingNumber, SocialLinks: socials,
		},
		Tags: bundleTags, Articles: bundleArticles,
	}, nil
}

func normalizeBundle(bundle Bundle) (Bundle, checksumPayload, error) {
	if bundle.SchemaVersion != 1 || bundle.ReleaseID <= 0 || bundle.GeneratedAt.IsZero() ||
		bundle.GeneratedAt.Location() != time.UTC || bundle.GeneratedAt.Nanosecond()%1000 != 0 ||
		bundle.Site.SocialLinks == nil || bundle.Tags == nil || bundle.Articles == nil {
		return Bundle{}, checksumPayload{}, releaseDomain("validate release bundle", ErrInvalidSnapshot)
	}
	tagBySlug := make(map[string]TagSnapshot, len(bundle.Tags))
	for _, tag := range bundle.Tags {
		if _, duplicate := tagBySlug[tag.Slug]; duplicate {
			return Bundle{}, checksumPayload{}, releaseDomain("validate release bundle", ErrInvalidSnapshot)
		}
		tagBySlug[tag.Slug] = TagSnapshot{ID: tag.ID, Name: tag.Name, Slug: tag.Slug}
	}
	prepared := PreparedSnapshot{
		Site: SiteSnapshot{
			Name: bundle.Site.Name, AuthorBio: bundle.Site.AuthorBio,
			AboutMarkdown: bundle.Site.AboutMarkdown, FilingName: bundle.Site.FilingName,
			FilingNumber: bundle.Site.FilingNumber,
			SocialLinks:  append(make([]SocialLink, 0, len(bundle.Site.SocialLinks)), bundle.Site.SocialLinks...),
		},
		Articles: make([]ArticleSnapshot, len(bundle.Articles)),
	}
	usedTags := make(map[string]struct{}, len(bundle.Tags))
	for index, article := range bundle.Articles {
		tags := make([]TagSnapshot, len(article.Tags))
		for tagIndex, slug := range article.Tags {
			tag, exists := tagBySlug[slug]
			if !exists {
				return Bundle{}, checksumPayload{}, releaseDomain("validate release bundle", ErrInvalidSnapshot)
			}
			tags[tagIndex] = tag
			usedTags[slug] = struct{}{}
		}
		prepared.Articles[index] = ArticleSnapshot{
			ArticleID: article.ArticleID, RevisionID: article.RevisionID, Slug: article.Slug,
			Title: article.Title, Summary: article.Summary, ContentMarkdown: article.ContentMarkdown,
			ContentHash: article.ContentHash, PublishedAt: article.PublishedAt, Tags: tags,
		}
	}
	if len(usedTags) != len(tagBySlug) {
		return Bundle{}, checksumPayload{}, releaseDomain("validate release bundle", ErrInvalidSnapshot)
	}
	payload, err := bundlePayloadFromPrepared(prepared)
	if err != nil {
		return Bundle{}, checksumPayload{}, err
	}
	copy := Bundle{
		SchemaVersion: 1, ReleaseID: bundle.ReleaseID, GeneratedAt: bundle.GeneratedAt,
		Site: payload.Site, Tags: payload.Tags, Articles: payload.Articles, Checksum: bundle.Checksum,
	}
	return copy, payload, nil
}

func checksumForPayload(payload checksumPayload) (string, error) {
	encoded, err := canonicalJSON(payload)
	if err != nil {
		return "", releaseDependency("encode release checksum payload", err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(value any) ([]byte, error) {
	if value == nil {
		return nil, errors.New("canonical JSON value is required")
	}
	raw, err := json.Marshal(value)
	if err != nil || !utf8.Valid(raw) {
		return nil, errors.New("canonical JSON value is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, errors.New("canonical JSON value is invalid")
	}
	var output bytes.Buffer
	if err := writeCanonicalJSON(&output, decoded); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeCanonicalJSON(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		output.WriteString(strconv.FormatBool(typed))
	case string:
		encoded, _ := json.Marshal(typed)
		output.Write(encoded)
	case json.Number:
		if _, err := strconv.ParseInt(typed.String(), 10, 64); err != nil {
			return errors.New("canonical JSON number is invalid")
		}
		output.WriteString(typed.String())
	case []any:
		output.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeCanonicalJSON(output, item); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			output.Write(encoded)
			output.WriteByte(':')
			if err := writeCanonicalJSON(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return errors.New("canonical JSON type is invalid")
	}
	return nil
}
