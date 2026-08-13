package media

import (
	"context"
	"errors"
	"time"
)

type HotlinkAuthorizer interface {
	AllowsCurrentReferer(context.Context, string) (bool, error)
}

type ProxyService interface {
	Redirect(context.Context, string, string) (string, error)
}

type activePublicKeyFinder interface {
	FindActiveByPublicKey(context.Context, string) (Media, error)
}

type proxyService struct {
	authorizer HotlinkAuthorizer
	finder     activePublicKeyFinder
	signer     ReadURLSigner
	now        func() time.Time
}

func NewProxyService(authorizer HotlinkAuthorizer, finder interface {
	FindActiveByPublicKey(context.Context, string) (Media, error)
}, signer ReadURLSigner, now func() time.Time) (ProxyService, error) {
	if nilMediaDependency(authorizer) {
		return nil, errors.New("hotlink authorizer is required")
	}
	if nilMediaDependency(finder) {
		return nil, errors.New("active media finder is required")
	}
	if nilMediaDependency(signer) {
		return nil, errors.New("media read URL signer is required")
	}
	if now == nil {
		return nil, errors.New("media proxy clock is required")
	}
	return &proxyService{authorizer: authorizer, finder: finder, signer: signer, now: now}, nil
}

func (s *proxyService) Redirect(ctx context.Context, publicKey, referer string) (string, error) {
	if err := s.validate(ctx); err != nil {
		return "", err
	}
	allowed, err := s.authorizer.AllowsCurrentReferer(ctx, referer)
	if err != nil {
		return "", mediaDependencyError("authorize media redirect", err)
	}
	if !allowed {
		return "", mediaDomainError("authorize media redirect", ErrHotlinkForbidden, nil)
	}
	if !mediaPublicKeyPattern.MatchString(publicKey) {
		return "", mediaDomainError("resolve media redirect", ErrNotFound, nil)
	}
	item, err := s.finder.FindActiveByPublicKey(ctx, publicKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", mediaDomainError("resolve media redirect", ErrNotFound, err)
		}
		return "", mediaDependencyError("resolve media redirect", err)
	}
	target, err := s.signer.ReadURL(item, s.now().UTC())
	if err != nil {
		return "", mediaDependencyError("sign media redirect", err)
	}
	return target, nil
}

func (s *proxyService) validate(ctx context.Context) error {
	if s == nil || nilMediaDependency(s.authorizer) || nilMediaDependency(s.finder) || nilMediaDependency(s.signer) || s.now == nil || nilMediaDependency(ctx) {
		return ErrDependencyUnavailable
	}
	return nil
}

var _ ProxyService = (*proxyService)(nil)
