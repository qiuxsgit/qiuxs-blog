package settings

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
)

type SiteService interface {
	GetSite(context.Context) (Site, error)
	PutSite(context.Context, Site, int64) (Site, error)
}

type ActiveMediaValidator interface {
	RequireActive(context.Context, int64) error
}

type siteService struct {
	repository SiteRepository
	media      ActiveMediaValidator
	now        func() time.Time
}

func NewSiteService(repository SiteRepository, mediaValidator ActiveMediaValidator, now func() time.Time) (SiteService, error) {
	if nilDependency(repository) {
		return nil, errors.New("settings repository is required")
	}
	if nilDependency(mediaValidator) {
		return nil, errors.New("settings active media validator is required")
	}
	if now == nil {
		return nil, errors.New("settings clock is required")
	}
	return &siteService{repository: repository, media: mediaValidator, now: now}, nil
}

func (s *siteService) GetSite(ctx context.Context) (Site, error) {
	if err := s.validate(ctx); err != nil {
		return Site{}, err
	}
	site, err := s.repository.GetSite(ctx)
	if errors.Is(err, ErrNotFound) {
		return DefaultSite(), nil
	}
	if err != nil {
		return Site{}, settingsDependencyError("get site settings", err)
	}
	return cloneSite(site), nil
}

func (s *siteService) PutSite(ctx context.Context, site Site, expectedLock int64) (Site, error) {
	if err := s.validate(ctx); err != nil {
		return Site{}, err
	}
	if expectedLock < 0 {
		return Site{}, settingsDomainError("put site settings", ErrInvalid, nil)
	}
	normalized, err := normalizeSite(site)
	if err != nil {
		return Site{}, err
	}
	if normalized.SEODefaultImageMediaID != nil {
		if err := s.media.RequireActive(ctx, *normalized.SEODefaultImageMediaID); err != nil {
			if errors.Is(err, media.ErrNotFound) || errors.Is(err, media.ErrInvalidMetadata) {
				return Site{}, settingsDomainError("validate site default media", ErrInvalid, err)
			}
			return Site{}, settingsDependencyError("validate site default media", err)
		}
	}
	at := s.now().UTC()
	var stored Site
	if expectedLock == 0 {
		stored, err = s.repository.CreateSite(ctx, normalized, at)
	} else {
		stored, err = s.repository.UpdateSite(ctx, normalized, expectedLock, at)
	}
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return Site{}, settingsDomainError("put site settings", ErrConflict, err)
		}
		return Site{}, settingsDependencyError("put site settings", err)
	}
	return cloneSite(stored), nil
}

func (s *siteService) validate(ctx context.Context) error {
	if s == nil || nilDependency(s.repository) || nilDependency(s.media) || s.now == nil {
		return errors.New("settings service is not configured")
	}
	if nilDependency(ctx) {
		return settingsDomainError("validate settings request", ErrInvalid, nil)
	}
	return nil
}

func nilDependency(value any) bool {
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
