package settings

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

type HotlinkService interface {
	Get(context.Context) (HotlinkPolicy, error)
	Put(context.Context, bool, []HotlinkEntry) (HotlinkPolicy, error)
	Current(context.Context) (HotlinkPolicy, error)
	AllowsReferer(HotlinkPolicy, string) bool
}

type hotlinkService struct {
	repository HotlinkRepository
	now        func() time.Time

	cacheMu         sync.RWMutex
	cache           HotlinkPolicy
	cacheValid      bool
	cacheGeneration uint64
	loadFlight      *hotlinkLoadFlight
}

type hotlinkLoadFlight struct {
	done             chan struct{}
	policy           HotlinkPolicy
	err              error
	invalidated      bool
	leaderTerminated bool
}

func NewHotlinkService(repository HotlinkRepository, now func() time.Time) (HotlinkService, error) {
	if nilDependency(repository) {
		return nil, errors.New("hotlink repository is required")
	}
	if now == nil {
		return nil, errors.New("hotlink clock is required")
	}
	return &hotlinkService{repository: repository, now: now}, nil
}

func NormalizeHostname(input string) (string, error) {
	hostname := strings.TrimSpace(input)
	if strings.HasSuffix(hostname, ".") {
		hostname = strings.TrimSuffix(hostname, ".")
	}
	if hostname == "" || len(hostname) > 253 {
		return "", settingsDomainError("normalize hotlink hostname", ErrInvalid, nil)
	}
	for _, character := range hostname {
		if character > 127 {
			return "", settingsDomainError("normalize hotlink hostname", ErrInvalid, nil)
		}
	}
	hostname = strings.ToLower(hostname)
	if _, err := netip.ParseAddr(hostname); err == nil || looksLikeNoncanonicalIP(hostname) {
		return "", settingsDomainError("normalize hotlink hostname", ErrInvalid, nil)
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", settingsDomainError("normalize hotlink hostname", ErrInvalid, nil)
		}
		for index := range len(label) {
			character := label[index]
			if character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				return "", settingsDomainError("normalize hotlink hostname", ErrInvalid, nil)
			}
		}
	}
	return hostname, nil
}

func (s *hotlinkService) Get(ctx context.Context) (HotlinkPolicy, error) {
	if err := s.validate(ctx); err != nil {
		return HotlinkPolicy{}, err
	}
	return s.load(ctx)
}

func (s *hotlinkService) Put(ctx context.Context, allowEmptyReferer bool, entries []HotlinkEntry) (HotlinkPolicy, error) {
	if err := s.validate(ctx); err != nil {
		return HotlinkPolicy{}, err
	}
	normalized, err := normalizeHotlinkPolicy(allowEmptyReferer, entries)
	if err != nil {
		return HotlinkPolicy{}, err
	}
	stored, err := s.repository.ReplaceHotlinkPolicy(ctx, normalized, s.now().UTC())
	if err != nil {
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) {
			return HotlinkPolicy{}, settingsDomainError("put hotlink policy", firstHotlinkDomain(err), err)
		}
		return HotlinkPolicy{}, settingsDependencyError("put hotlink policy", err)
	}

	s.cacheMu.Lock()
	s.cacheGeneration++
	s.cache = HotlinkPolicy{}
	s.cacheValid = false
	s.loadFlight = nil
	s.cacheMu.Unlock()
	return cloneHotlinkPolicy(stored), nil
}

func (s *hotlinkService) Current(ctx context.Context) (HotlinkPolicy, error) {
	if err := s.validate(ctx); err != nil {
		return HotlinkPolicy{}, err
	}
	for {
		s.cacheMu.RLock()
		if s.cacheValid {
			cached := cloneHotlinkPolicy(s.cache)
			s.cacheMu.RUnlock()
			return cached, nil
		}
		flight := s.loadFlight
		s.cacheMu.RUnlock()
		if flight != nil {
			loaded, retry, err := waitForHotlinkLoad(ctx, flight)
			if retry {
				continue
			}
			return loaded, err
		}

		s.cacheMu.Lock()
		if s.cacheValid {
			cached := cloneHotlinkPolicy(s.cache)
			s.cacheMu.Unlock()
			return cached, nil
		}
		if s.loadFlight != nil {
			flight = s.loadFlight
			s.cacheMu.Unlock()
			loaded, retry, err := waitForHotlinkLoad(ctx, flight)
			if retry {
				continue
			}
			return loaded, err
		}
		loadGeneration := s.cacheGeneration
		flight = &hotlinkLoadFlight{done: make(chan struct{})}
		s.loadFlight = flight
		s.cacheMu.Unlock()

		loaded, loadErr := s.load(ctx)
		s.cacheMu.Lock()
		generationChanged := s.cacheGeneration != loadGeneration
		flight.policy = cloneHotlinkPolicy(loaded)
		flight.err = loadErr
		flight.invalidated = generationChanged
		leaderErr := ctx.Err()
		flight.leaderTerminated = leaderErr != nil && errors.Is(loadErr, leaderErr)
		if loadErr == nil && !generationChanged {
			s.cache = cloneHotlinkPolicy(loaded)
			s.cacheValid = true
		}
		if s.loadFlight == flight {
			s.loadFlight = nil
		}
		close(flight.done)
		s.cacheMu.Unlock()
		if generationChanged {
			continue
		}
		if loadErr != nil {
			return HotlinkPolicy{}, loadErr
		}
		return cloneHotlinkPolicy(loaded), nil
	}
}

func waitForHotlinkLoad(ctx context.Context, flight *hotlinkLoadFlight) (HotlinkPolicy, bool, error) {
	select {
	case <-flight.done:
		if flight.invalidated || flight.leaderTerminated && ctx.Err() == nil {
			return HotlinkPolicy{}, true, nil
		}
		return cloneHotlinkPolicy(flight.policy), false, flight.err
	case <-ctx.Done():
		return HotlinkPolicy{}, false, settingsDependencyError("wait for hotlink policy", ctx.Err())
	}
}

func (s *hotlinkService) AllowsReferer(policy HotlinkPolicy, referer string) bool {
	if referer == "" {
		return policy.AllowEmptyReferer
	}
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return false
	}
	hostname, err := NormalizeHostname(parsed.Hostname())
	if err != nil {
		return false
	}
	for _, entry := range policy.Entries {
		if !entry.Enabled {
			continue
		}
		allowed, normalizeErr := NormalizeHostname(entry.Hostname)
		if normalizeErr == nil && hostname == allowed {
			return true
		}
	}
	return false
}

func (s *hotlinkService) load(ctx context.Context) (HotlinkPolicy, error) {
	policy, err := s.repository.GetHotlinkPolicy(ctx)
	if errors.Is(err, ErrNotFound) {
		return defaultHotlinkPolicy(), nil
	}
	if err != nil {
		return HotlinkPolicy{}, settingsDependencyError("get hotlink policy", err)
	}
	return cloneHotlinkPolicy(policy), nil
}

func (s *hotlinkService) validate(ctx context.Context) error {
	if s == nil || nilDependency(s.repository) || s.now == nil {
		return errors.New("hotlink service is not configured")
	}
	if nilDependency(ctx) {
		return settingsDomainError("validate hotlink request", ErrInvalid, nil)
	}
	return nil
}

func normalizeHotlinkPolicy(allowEmptyReferer bool, entries []HotlinkEntry) (HotlinkPolicy, error) {
	policy := HotlinkPolicy{AllowEmptyReferer: allowEmptyReferer, Entries: make([]HotlinkEntry, 0, len(entries))}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		hostname, err := NormalizeHostname(entry.Hostname)
		if err != nil {
			return HotlinkPolicy{}, err
		}
		if _, exists := seen[hostname]; exists {
			return HotlinkPolicy{}, settingsDomainError("normalize hotlink policy", ErrInvalid, nil)
		}
		seen[hostname] = struct{}{}
		policy.Entries = append(policy.Entries, HotlinkEntry{Hostname: hostname, Enabled: entry.Enabled})
	}
	return policy, nil
}

func defaultHotlinkPolicy() HotlinkPolicy {
	return HotlinkPolicy{
		AllowEmptyReferer: true,
		Entries: []HotlinkEntry{
			{Hostname: "qiuxs.com", Enabled: true},
			{Hostname: "blog-admin.qiuxs.com", Enabled: true},
		},
	}
}

func firstHotlinkDomain(err error) error {
	if errors.Is(err, ErrConflict) {
		return ErrConflict
	}
	return ErrInvalid
}

var _ HotlinkService = (*hotlinkService)(nil)
