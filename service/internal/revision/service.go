package revision

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

type Service interface {
	GetDraft(context.Context, int64) (Draft, error)
	GetDraftAt(context.Context, int64, int64) (Draft, error)
	SaveDraft(context.Context, int64, int64, Content) (Draft, error)
	Preview(context.Context, int64) (Draft, error)
	CreateVersion(context.Context, int64, int64) (Version, Draft, error)
	ListVersions(context.Context, int64) ([]Version, error)
	RestoreVersion(context.Context, int64, int64, int64) (Draft, error)
	ValidateFreezable(Draft) error
}

type service struct {
	repository Repository
	tags       TagResolver
	media      MediaResolver
	now        func() time.Time
}

func NewService(repository Repository, tags TagResolver, mediaResolver MediaResolver, now func() time.Time) (Service, error) {
	if nilRevisionDependency(repository) {
		return nil, errors.New("revision repository is required")
	}
	if nilRevisionDependency(tags) {
		return nil, errors.New("revision tag resolver is required")
	}
	if nilRevisionDependency(mediaResolver) {
		return nil, errors.New("revision media resolver is required")
	}
	if now == nil {
		return nil, errors.New("revision clock is required")
	}
	return &service{repository: repository, tags: tags, media: mediaResolver, now: now}, nil
}

func (s *service) GetDraft(ctx context.Context, articleID int64) (Draft, error) {
	if err := s.validate(ctx, articleID); err != nil {
		return Draft{}, err
	}
	draft, err := s.repository.GetDraft(ctx, articleID)
	if err != nil {
		return Draft{}, revisionSafeWrap("get article draft", err)
	}
	return draft, nil
}

func (s *service) GetDraftAt(ctx context.Context, articleID, revisionID int64) (Draft, error) {
	if err := s.validate(ctx, articleID); err != nil || revisionID <= 0 {
		return Draft{}, ErrInvalidContent
	}
	draft, err := s.repository.GetDraftAt(ctx, articleID, revisionID)
	if err != nil {
		return Draft{}, revisionSafeWrap("get article draft at pointer", err)
	}
	return draft, nil
}

func (s *service) SaveDraft(ctx context.Context, articleID, lockVersion int64, content Content) (Draft, error) {
	if err := s.validate(ctx, articleID); err != nil || lockVersion <= 0 || len(content.TagIDs) > MaxTagCount {
		return Draft{}, ErrInvalidContent
	}
	publicKeys, err := ValidateDraft(content)
	if err != nil {
		return Draft{}, err
	}
	snapshots, err := s.tags.Snapshots(ctx, append([]int64(nil), content.TagIDs...))
	if err != nil {
		if errors.Is(err, tag.ErrNotFound) || errors.Is(err, tag.ErrInvalidSelection) || errors.Is(err, tag.ErrInvalidName) {
			return Draft{}, revisionDomainError("resolve revision tags", ErrInvalidContent, err)
		}
		return Draft{}, revisionSafeWrap("resolve revision tags", err)
	}
	cover, references, err := s.media.ResolveReferences(ctx, content.CoverMediaID, publicKeys)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) || errors.Is(err, media.ErrInvalidMetadata) {
			return Draft{}, revisionDomainError("resolve revision media", ErrInvalidContent, err)
		}
		return Draft{}, revisionSafeWrap("resolve revision media", err)
	}
	prepared := PreparedContent{
		Title:     strings.TrimSpace(content.Title),
		Summary:   strings.TrimSpace(content.Summary),
		Cover:     cover,
		ContentMD: content.ContentMD,
		Tags:      append([]tag.Snapshot(nil), snapshots...),
		Media:     append([]media.Reference(nil), references...),
	}
	prepared.ContentHash = ComputeHash(prepared)
	saved, err := s.repository.SaveDraft(ctx, articleID, lockVersion, prepared, s.now().UTC())
	if err != nil {
		return Draft{}, revisionSafeWrap("save article draft", err)
	}
	return saved, nil
}

func (s *service) Preview(ctx context.Context, articleID int64) (Draft, error) {
	return s.GetDraft(ctx, articleID)
}

func (s *service) CreateVersion(ctx context.Context, articleID, lockVersion int64) (Version, Draft, error) {
	if err := s.validate(ctx, articleID); err != nil || lockVersion <= 0 {
		return Version{}, Draft{}, ErrInvalidContent
	}
	current, err := s.repository.GetDraft(ctx, articleID)
	if err != nil {
		return Version{}, Draft{}, revisionSafeWrap("get draft for manual version", err)
	}
	if current.ID <= 0 || current.ArticleID != articleID {
		return Version{}, Draft{}, revisionSafeWrap("get draft for manual version", errors.New("stored article mismatch"))
	}
	if current.LockVersion != lockVersion {
		return Version{}, Draft{}, ErrConflict
	}
	if err := s.ValidateFreezable(current); err != nil {
		return Version{}, Draft{}, err
	}
	publicKeys, err := ValidateDraft(Content{Title: current.Title, Summary: current.Summary, ContentMD: current.ContentMD})
	if err != nil {
		return Version{}, Draft{}, err
	}
	if _, _, err := s.media.ResolveReferences(ctx, current.CoverMediaID, publicKeys); err != nil {
		if errors.Is(err, media.ErrNotFound) || errors.Is(err, media.ErrInvalidMetadata) {
			return Version{}, Draft{}, revisionDomainError("validate version media", ErrInvalidContent, err)
		}
		return Version{}, Draft{}, revisionSafeWrap("validate version media", err)
	}
	version, draft, err := s.repository.CreateVersion(ctx, articleID, current.ID, lockVersion, s.now().UTC())
	if err != nil {
		return Version{}, Draft{}, revisionSafeWrap("create manual version", err)
	}
	return version, draft, nil
}

func (s *service) ListVersions(ctx context.Context, articleID int64) ([]Version, error) {
	if err := s.validate(ctx, articleID); err != nil {
		return nil, ErrInvalidContent
	}
	versions, err := s.repository.ListVersions(ctx, articleID)
	if err != nil {
		return nil, revisionSafeWrap("list article versions", err)
	}
	return versions, nil
}

func (s *service) RestoreVersion(ctx context.Context, articleID, revisionID, lockVersion int64) (Draft, error) {
	if err := s.validate(ctx, articleID); err != nil || revisionID <= 0 || lockVersion <= 0 {
		return Draft{}, ErrInvalidContent
	}
	current, err := s.repository.GetDraft(ctx, articleID)
	if err != nil {
		return Draft{}, revisionSafeWrap("get draft for version restore", err)
	}
	if current.ID <= 0 || current.ArticleID != articleID {
		return Draft{}, revisionSafeWrap("get draft for version restore", errors.New("stored article mismatch"))
	}
	if current.LockVersion != lockVersion {
		return Draft{}, ErrConflict
	}
	draft, err := s.repository.RestoreVersion(ctx, articleID, revisionID, current.ID, lockVersion, s.now().UTC())
	if err != nil {
		return Draft{}, revisionSafeWrap("restore article version", err)
	}
	return draft, nil
}

func (s *service) ValidateFreezable(draft Draft) error {
	if s == nil {
		return ErrInvalidContent
	}
	return ValidateFreezable(Content{
		Title: draft.Title, Summary: draft.Summary, CoverMediaID: draft.CoverMediaID, ContentMD: draft.ContentMD,
	})
}

func (s *service) validate(ctx context.Context, articleID int64) error {
	if s == nil || nilRevisionDependency(s.repository) || nilRevisionDependency(s.tags) || nilRevisionDependency(s.media) || s.now == nil || nilRevisionDependency(ctx) || articleID <= 0 {
		return ErrInvalidContent
	}
	return nil
}

type revisionSanitizedError struct {
	operation string
	cause     error
}

func (e *revisionSanitizedError) Error() string { return e.operation + " failed" }
func (e *revisionSanitizedError) Unwrap() error { return e.cause }

func revisionSafeWrap(operation string, cause error) error {
	return &revisionSanitizedError{operation: operation, cause: cause}
}

type revisionDomainJoinedError struct {
	operation string
	domain    error
	cause     error
}

func (e *revisionDomainJoinedError) Error() string   { return e.operation + " failed" }
func (e *revisionDomainJoinedError) Unwrap() []error { return []error{e.domain, e.cause} }

func revisionDomainError(operation string, domain, cause error) error {
	return &revisionDomainJoinedError{operation: operation, domain: domain, cause: cause}
}

func nilRevisionDependency(value any) bool {
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

var _ Service = (*service)(nil)
