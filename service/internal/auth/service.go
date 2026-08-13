package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const dummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$cWl1eHMtYmxvZy1kdW1teQ$erZZ0+ILSHaNpqTAcuH1AhGE0jZeC5fPeBGmltMGIa0"

var ErrDependencyUnavailable = errors.New("dependency unavailable")

// RateLimitError preserves the limiter's retry decision for the HTTP layer.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e RateLimitError) Error() string { return ErrRateLimited.Error() }

func (e RateLimitError) Unwrap() error { return ErrRateLimited }

type LoginResult struct {
	Admin   Admin
	Token   string
	Session Session
}

// Service orchestrates administrator credentials, rate limits, and sessions.
type Service struct {
	repo     Repository
	hasher   PasswordHasher
	sessions SessionManager
	limiter  LoginLimiter
	now      func() time.Time
	initErr  error
}

func NewService(repo Repository, hasher PasswordHasher, sessions SessionManager, limiter LoginLimiter, now func() time.Time) Service {
	service := Service{
		repo:     repo,
		hasher:   hasher,
		sessions: sessions,
		limiter:  limiter,
		now:      now,
	}
	if isNilDependency(repo) || isNilDependency(limiter) || now == nil {
		service.initErr = ErrDependencyUnavailable
	}
	return service
}

func (s Service) Login(ctx context.Context, username, password, ip string) (LoginResult, error) {
	if s.initErr != nil || isNilDependency(s.repo) || isNilDependency(s.limiter) || s.now == nil {
		return LoginResult{}, dependencyUnavailable()
	}

	normalizedUsername, err := NormalizeUsername(username)
	if err != nil || !validPassword(password) {
		return LoginResult{}, ErrInvalidCredentials
	}

	decision, err := s.limiter.Allow(ctx, normalizedUsername, ip)
	if err != nil {
		return LoginResult{}, dependencyUnavailable()
	}
	if !decision.Allowed {
		return LoginResult{}, RateLimitError{RetryAfter: decision.RetryAfter}
	}

	admin, findErr := s.repo.FindByUsername(ctx, normalizedUsername)
	hash := admin.PasswordHash
	if errors.Is(findErr, ErrAdminNotFound) {
		hash = dummyPasswordHash
	}
	passwordMatches, verifyErr := s.hasher.Verify(password, hash)
	if findErr != nil && !errors.Is(findErr, ErrAdminNotFound) {
		return LoginResult{}, dependencyUnavailable()
	}
	if verifyErr != nil || errors.Is(findErr, ErrAdminNotFound) || !passwordMatches || admin.State != "active" {
		if err := s.limiter.RecordFailure(ctx, normalizedUsername, ip); err != nil {
			return LoginResult{}, dependencyUnavailable()
		}
		return LoginResult{}, ErrInvalidCredentials
	}

	admin.PasswordHash = ""
	token, session, err := s.sessions.Create(ctx, admin)
	if err != nil {
		return LoginResult{}, dependencyUnavailable()
	}
	if err := s.repo.UpdateLastLogin(ctx, admin.ID, s.now().UTC()); err != nil {
		s.compensateSession(ctx, token)
		return LoginResult{}, dependencyUnavailable()
	}
	if err := s.limiter.ResetUsername(ctx, normalizedUsername); err != nil {
		s.compensateSession(ctx, token)
		return LoginResult{}, dependencyUnavailable()
	}

	return LoginResult{Admin: admin, Token: token, Session: session}, nil
}

func (s Service) Logout(ctx context.Context, token string) error {
	if err := s.sessions.Delete(ctx, token); err != nil && !errors.Is(err, ErrSessionNotFound) {
		return dependencyUnavailable()
	}
	return nil
}

func (s Service) Current(ctx context.Context, token string) (Admin, error) {
	if s.initErr != nil || isNilDependency(s.repo) {
		return Admin{}, dependencyUnavailable()
	}

	session, err := s.sessions.Get(ctx, token)
	if errors.Is(err, ErrSessionNotFound) {
		return Admin{}, ErrUnauthenticated
	}
	if err != nil {
		return Admin{}, dependencyUnavailable()
	}

	admin, err := s.repo.FindByID(ctx, session.AdminID)
	if errors.Is(err, ErrAdminNotFound) {
		s.compensateSession(ctx, token)
		return Admin{}, ErrUnauthenticated
	}
	if err != nil {
		return Admin{}, dependencyUnavailable()
	}
	if admin.State != "active" {
		s.compensateSession(ctx, token)
		return Admin{}, ErrUnauthenticated
	}
	admin.PasswordHash = ""
	return admin, nil
}

func (s Service) compensateSession(ctx context.Context, token string) {
	if err := s.sessions.Delete(ctx, token); err != nil {
		slog.ErrorContext(ctx, "login session compensation failed")
	}
}

func dependencyUnavailable() error {
	return ErrDependencyUnavailable
}
