package tag

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
)

const maximumSlugRetries = 5

type Service interface {
	Create(context.Context, string) (Tag, error)
	List(context.Context) ([]Tag, error)
	Rename(context.Context, int64, string) (Tag, error)
	Snapshots(context.Context, []int64) ([]Snapshot, error)
}

type service struct {
	repository Repository
	keys       *randomkey.Generator
	now        func() time.Time
}

func NewService(repository Repository, keys *randomkey.Generator, now func() time.Time) (Service, error) {
	if nilInterface(repository) {
		return nil, errors.New("tag repository is required")
	}
	if keys == nil {
		return nil, errors.New("tag random key generator is required")
	}
	if now == nil {
		return nil, errors.New("tag clock is required")
	}
	return &service{repository: repository, keys: keys, now: now}, nil
}

func NormalizeName(name string) (string, error) {
	normalized := strings.Join(strings.Fields(name), " ")
	if normalized == "" || utf8.RuneCountInString(normalized) > 64 {
		return "", ErrInvalidName
	}
	return normalized, nil
}

func (s *service) Create(ctx context.Context, name string) (Tag, error) {
	if err := s.validate(ctx); err != nil {
		return Tag{}, err
	}
	normalized, err := NormalizeName(name)
	if err != nil {
		return Tag{}, err
	}
	at := s.now().UTC()
	for attempt := 0; attempt <= maximumSlugRetries; attempt++ {
		slug, keyErr := s.keys.TagSlug()
		if keyErr != nil {
			return Tag{}, safeWrap("generate tag slug", keyErr)
		}
		created, createErr := s.repository.Create(ctx, normalized, slug, at)
		if createErr == nil {
			return created, nil
		}
		if !errors.Is(createErr, ErrSlugConflict) {
			return Tag{}, safeWrap("create tag", createErr)
		}
	}
	return Tag{}, safeWrap("create tag after slug retries", ErrSlugConflict)
}

func (s *service) List(ctx context.Context) ([]Tag, error) {
	if err := s.validate(ctx); err != nil {
		return nil, err
	}
	tags, err := s.repository.List(ctx)
	if err != nil {
		return nil, safeWrap("list tags", err)
	}
	return tags, nil
}

func (s *service) Rename(ctx context.Context, id int64, name string) (Tag, error) {
	if err := s.validate(ctx); err != nil {
		return Tag{}, err
	}
	if id <= 0 {
		return Tag{}, ErrInvalidSelection
	}
	normalized, err := NormalizeName(name)
	if err != nil {
		return Tag{}, err
	}
	renamed, err := s.repository.Rename(ctx, id, normalized, s.now().UTC())
	if err != nil {
		return Tag{}, safeWrap("rename tag", err)
	}
	return renamed, nil
}

func (s *service) Snapshots(ctx context.Context, ids []int64) ([]Snapshot, error) {
	if err := s.validate(ctx); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []Snapshot{}, nil
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrInvalidSelection
		}
		if _, exists := seen[id]; exists {
			return nil, ErrInvalidSelection
		}
		seen[id] = struct{}{}
	}

	stored, err := s.repository.FindByIDs(ctx, append([]int64(nil), ids...))
	if err != nil {
		return nil, safeWrap("find tags for snapshots", err)
	}
	byID := make(map[int64]Tag, len(stored))
	for _, item := range stored {
		byID[item.ID] = item
	}
	snapshots := make([]Snapshot, 0, len(ids))
	for position, id := range ids {
		item, exists := byID[id]
		if !exists {
			return nil, ErrNotFound
		}
		snapshots = append(snapshots, Snapshot{
			TagID:    item.ID,
			Name:     item.Name,
			Slug:     item.Slug,
			Position: position,
		})
	}
	return snapshots, nil
}

func (s *service) validate(ctx context.Context) error {
	if s == nil || nilInterface(s.repository) || s.keys == nil || s.now == nil || nilInterface(ctx) {
		return ErrInvalidSelection
	}
	return nil
}

type sanitizedError struct {
	operation string
	cause     error
}

func (e *sanitizedError) Error() string { return e.operation + " failed" }
func (e *sanitizedError) Unwrap() error { return e.cause }

func safeWrap(operation string, cause error) error {
	return &sanitizedError{operation: operation, cause: cause}
}

func nilInterface(value any) bool {
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
