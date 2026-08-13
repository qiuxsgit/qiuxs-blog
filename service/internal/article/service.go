package article

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
)

const maximumSlugRetries = 5

type Detail struct {
	Article Article
	Draft   revision.Draft
}

type Service interface {
	Create(context.Context) (Detail, error)
	Get(context.Context, int64) (Detail, error)
	List(context.Context, State) ([]Summary, error)
	Trash(context.Context, int64) error
	Untrash(context.Context, int64) error
}

type service struct {
	repository Repository
	drafts     DraftReader
	keys       *randomkey.Generator
	now        func() time.Time
}

func NewService(repository Repository, drafts DraftReader, keys *randomkey.Generator, now func() time.Time) (Service, error) {
	if nilArticleInterface(repository) {
		return nil, errors.New("article repository is required")
	}
	if nilArticleInterface(drafts) {
		return nil, errors.New("article draft reader is required")
	}
	if keys == nil {
		return nil, errors.New("article random key generator is required")
	}
	if now == nil {
		return nil, errors.New("article clock is required")
	}
	return &service{repository: repository, drafts: drafts, keys: keys, now: now}, nil
}

func (s *service) Create(ctx context.Context) (Detail, error) {
	if err := s.validate(ctx); err != nil {
		return Detail{}, err
	}
	at := s.now().UTC()
	for attempt := 0; attempt <= maximumSlugRetries; attempt++ {
		slug, err := s.keys.ArticleSlug()
		if err != nil {
			return Detail{}, articleSafeWrap("generate article slug", err)
		}
		created, err := s.repository.Create(ctx, slug, at)
		if err != nil {
			if errors.Is(err, ErrSlugConflict) {
				if attempt == maximumSlugRetries {
					return Detail{}, articleSafeWrap("create article after slug retries", err)
				}
				continue
			}
			return Detail{}, articleSafeWrap("create article", err)
		}
		draft, err := s.drafts.GetDraft(ctx, created.ID)
		if err != nil {
			return Detail{}, articleSafeWrap("load initial article draft", err)
		}
		return Detail{Article: created, Draft: draft}, nil
	}
	return Detail{}, articleSafeWrap("create article after slug retries", ErrSlugConflict)
}

func (s *service) Get(ctx context.Context, id int64) (Detail, error) {
	if err := s.validateID(ctx, id); err != nil {
		return Detail{}, err
	}
	item, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Detail{}, articleSafeWrap("find article", err)
	}
	draft, err := s.drafts.GetDraft(ctx, item.ID)
	if err != nil {
		return Detail{}, articleSafeWrap("load article draft", err)
	}
	return Detail{Article: item, Draft: draft}, nil
}

func (s *service) List(ctx context.Context, state State) ([]Summary, error) {
	if err := s.validate(ctx); err != nil {
		return nil, err
	}
	if !validState(state) {
		return nil, ErrStateConflict
	}
	items, err := s.repository.List(ctx, state)
	if err != nil {
		return nil, articleSafeWrap("list articles", err)
	}
	return items, nil
}

func (s *service) Trash(ctx context.Context, id int64) error {
	if err := s.validateID(ctx, id); err != nil {
		return err
	}
	item, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return articleSafeWrap("find article to trash", err)
	}
	if item.PublishedRevisionID != nil {
		return ErrMustBeUnpublished
	}
	if item.State != StateActive {
		return ErrStateConflict
	}
	if err := s.repository.SetState(ctx, id, StateActive, StateTrashed, s.now().UTC()); err != nil {
		return articleSafeWrap("trash article", err)
	}
	return nil
}

func (s *service) Untrash(ctx context.Context, id int64) error {
	if err := s.validateID(ctx, id); err != nil {
		return err
	}
	item, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return articleSafeWrap("find article to untrash", err)
	}
	if item.State != StateTrashed {
		return ErrStateConflict
	}
	if err := s.repository.SetState(ctx, id, StateTrashed, StateActive, s.now().UTC()); err != nil {
		return articleSafeWrap("untrash article", err)
	}
	return nil
}

func (s *service) validate(ctx context.Context) error {
	if s == nil || nilArticleInterface(s.repository) || nilArticleInterface(s.drafts) || s.keys == nil || s.now == nil || nilArticleInterface(ctx) {
		return ErrStateConflict
	}
	return nil
}

func (s *service) validateID(ctx context.Context, id int64) error {
	if err := s.validate(ctx); err != nil {
		return err
	}
	if id <= 0 {
		return ErrStateConflict
	}
	return nil
}

func validState(state State) bool {
	return state == StateActive || state == StateTrashed
}

type articleSanitizedError struct {
	operation string
	cause     error
}

func (e *articleSanitizedError) Error() string { return e.operation + " failed" }
func (e *articleSanitizedError) Unwrap() error { return e.cause }

func articleSafeWrap(operation string, cause error) error {
	return &articleSanitizedError{operation: operation, cause: cause}
}

func nilArticleInterface(value any) bool {
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
