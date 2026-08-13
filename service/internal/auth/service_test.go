package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type serviceRepositoryFake struct {
	adminByUsername Admin
	findUsernameErr error
	adminByID       Admin
	findIDErr       error
	updateErr       error
	events          *[]string
	findUsername    string
	findID          int64
	updatedID       int64
	updatedAt       time.Time
}

func (r *serviceRepositoryFake) Count(context.Context) (int, error) { return 0, nil }

func (r *serviceRepositoryFake) Create(context.Context, string, string) (Admin, error) {
	return Admin{}, errors.New("not implemented")
}

func (r *serviceRepositoryFake) FindByUsername(_ context.Context, username string) (Admin, error) {
	r.findUsername = username
	r.record("find")
	return r.adminByUsername, r.findUsernameErr
}

func (r *serviceRepositoryFake) FindByID(_ context.Context, id int64) (Admin, error) {
	r.findID = id
	r.record("find-id")
	return r.adminByID, r.findIDErr
}

func (r *serviceRepositoryFake) UpdateLastLogin(_ context.Context, id int64, at time.Time) error {
	r.updatedID = id
	r.updatedAt = at
	r.record("update")
	return r.updateErr
}

func (r *serviceRepositoryFake) record(event string) {
	if r.events != nil {
		*r.events = append(*r.events, event)
	}
}

type serviceLimiterFake struct {
	decision       LimitDecision
	allowErr       error
	recordErr      error
	resetErr       error
	events         *[]string
	allowUsername  string
	allowIP        string
	recordUsername string
	recordIP       string
	recordCalls    int
	resetUsername  string
	resetCalls     int
}

func (l *serviceLimiterFake) Allow(_ context.Context, username, ip string) (LimitDecision, error) {
	l.allowUsername = username
	l.allowIP = ip
	l.record("allow")
	return l.decision, l.allowErr
}

func (l *serviceLimiterFake) RecordFailure(_ context.Context, username, ip string) error {
	l.recordUsername = username
	l.recordIP = ip
	l.recordCalls++
	l.record("failure")
	return l.recordErr
}

func (l *serviceLimiterFake) ResetUsername(_ context.Context, username string) error {
	l.resetUsername = username
	l.resetCalls++
	l.record("reset")
	return l.resetErr
}

func (l *serviceLimiterFake) record(event string) {
	if l.events != nil {
		*l.events = append(*l.events, event)
	}
}

type serviceSessionStoreFake struct {
	setErr            error
	getSession        Session
	getErr            error
	deleteErr         error
	events            *[]string
	setCalls          int
	deleteCalls       int
	deleteContextErr  error
	deleteDeadline    time.Time
	deleteHasDeadline bool
}

func (s *serviceSessionStoreFake) Set(_ context.Context, _ string, _ Session, _ time.Duration) error {
	s.setCalls++
	s.record("session-create")
	return s.setErr
}

func (s *serviceSessionStoreFake) Get(_ context.Context, _ string) (Session, error) {
	s.record("session-get")
	return s.getSession, s.getErr
}

func (s *serviceSessionStoreFake) Delete(ctx context.Context, _ string) error {
	s.deleteCalls++
	s.deleteContextErr = ctx.Err()
	s.deleteDeadline, s.deleteHasDeadline = ctx.Deadline()
	s.record("session-delete")
	return s.deleteErr
}

func (s *serviceSessionStoreFake) record(event string) {
	if s.events != nil {
		*s.events = append(*s.events, event)
	}
}

func testServiceHasher(t *testing.T) (PasswordHasher, string) {
	t.Helper()
	hasher := PasswordHasher{
		memory:      8,
		iterations:  1,
		parallelism: 1,
		saltLength:  16,
		keyLength:   16,
		rand:        strings.NewReader("0123456789abcdef"),
	}
	hash, err := hasher.Hash("correct-password")
	require.NoError(t, err)
	return hasher, hash
}

func newServiceForTest(t *testing.T, repo Repository, store SessionStore, limiter LoginLimiter, now time.Time) Service {
	t.Helper()
	hasher, _ := testServiceHasher(t)
	manager := NewSessionManager(store, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), func() time.Time { return now })
	return NewService(repo, hasher, manager, limiter, func() time.Time { return now })
}

func TestServiceLoginCreatesSessionThenUpdatesLoginThenResetsLimiter(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	hasher, hash := testServiceHasher(t)
	events := []string{}
	repo := &serviceRepositoryFake{
		adminByUsername: Admin{ID: 42, Username: "admin.user", PasswordHash: hash, State: "active"},
		events:          &events,
	}
	store := &serviceSessionStoreFake{events: &events}
	limiter := &serviceLimiterFake{decision: LimitDecision{Allowed: true}, events: &events}
	sessions := NewSessionManager(store, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), func() time.Time { return now })
	service := NewService(repo, hasher, sessions, limiter, func() time.Time { return now })

	result, err := service.Login(context.Background(), "  ADMIN.User ", "correct-password", "192.0.2.10")

	require.NoError(t, err)
	require.Equal(t, []string{"allow", "find", "session-create", "update", "reset"}, events)
	require.Equal(t, "admin.user", repo.findUsername)
	require.Equal(t, "admin.user", limiter.allowUsername)
	require.Equal(t, "192.0.2.10", limiter.allowIP)
	require.Equal(t, "admin.user", limiter.resetUsername)
	require.Equal(t, int64(42), repo.updatedID)
	require.Equal(t, now.UTC(), repo.updatedAt)
	require.Equal(t, Admin{ID: 42, Username: "admin.user", State: "active"}, result.Admin)
	require.Equal(t, "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", result.Token)
	require.Equal(t, Session{AdminID: 42, Username: "admin.user", ExpiresAt: now.Add(time.Hour)}, result.Session)
	require.Zero(t, limiter.recordCalls)
}

func TestServiceLoginMakesUnknownWrongAndDisabledCredentialsIndistinguishable(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	hasher, hash := testServiceHasher(t)
	tests := []struct {
		name     string
		admin    Admin
		findErr  error
		password string
	}{
		{name: "unknown username", findErr: ErrAdminNotFound, password: "correct-password"},
		{name: "wrong password", admin: Admin{ID: 42, Username: "admin.user", PasswordHash: hash, State: "active"}, password: "wrong-password"},
		{name: "disabled administrator", admin: Admin{ID: 42, Username: "admin.user", PasswordHash: hash, State: "disabled"}, password: "correct-password"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &serviceSessionStoreFake{}
			limiter := &serviceLimiterFake{decision: LimitDecision{Allowed: true}}
			repo := &serviceRepositoryFake{adminByUsername: tt.admin, findUsernameErr: tt.findErr}
			sessions := NewSessionManager(store, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), func() time.Time { return now })
			service := NewService(repo, hasher, sessions, limiter, func() time.Time { return now })

			result, err := service.Login(context.Background(), "ADMIN.User", tt.password, "192.0.2.10")

			require.Equal(t, LoginResult{}, result)
			require.Equal(t, ErrInvalidCredentials, err)
			require.Equal(t, 1, limiter.recordCalls)
			require.Equal(t, "admin.user", limiter.recordUsername)
			require.Equal(t, "192.0.2.10", limiter.recordIP)
			require.Zero(t, store.setCalls)
		})
	}
}

func TestServiceLoginReturnsTypedRateLimitDecisionWithoutDependencies(t *testing.T) {
	retryAfter := 1500 * time.Millisecond
	repo := &serviceRepositoryFake{}
	store := &serviceSessionStoreFake{}
	limiter := &serviceLimiterFake{decision: LimitDecision{Allowed: false, RetryAfter: retryAfter}}
	service := newServiceForTest(t, repo, store, limiter, time.Now())

	result, err := service.Login(context.Background(), "admin.user", "password", "192.0.2.10")

	require.Equal(t, LoginResult{}, result)
	require.ErrorIs(t, err, ErrRateLimited)
	var rateLimitErr RateLimitError
	require.ErrorAs(t, err, &rateLimitErr)
	require.Equal(t, retryAfter, rateLimitErr.RetryAfter)
	require.Empty(t, repo.findUsername)
	require.Zero(t, limiter.recordCalls)
	require.Zero(t, store.setCalls)
}

func TestServiceLoginMapsLimiterAndSessionFailuresToDependencyUnavailable(t *testing.T) {
	now := time.Now()
	hasher, hash := testServiceHasher(t)
	tests := []struct {
		name    string
		limiter *serviceLimiterFake
		store   *serviceSessionStoreFake
	}{
		{
			name:    "limiter unavailable",
			limiter: &serviceLimiterFake{allowErr: errors.New("redis password secret")},
			store:   &serviceSessionStoreFake{},
		},
		{
			name:    "session unavailable",
			limiter: &serviceLimiterFake{decision: LimitDecision{Allowed: true}},
			store:   &serviceSessionStoreFake{setErr: errors.New("redis password secret")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &serviceRepositoryFake{adminByUsername: Admin{ID: 42, Username: "admin.user", PasswordHash: hash, State: "active"}}
			sessions := NewSessionManager(tt.store, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), func() time.Time { return now })
			service := NewService(repo, hasher, sessions, tt.limiter, func() time.Time { return now })

			result, err := service.Login(context.Background(), "admin.user", "correct-password", "192.0.2.10")

			require.Equal(t, LoginResult{}, result)
			require.ErrorIs(t, err, ErrDependencyUnavailable)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestServiceLoginCompensatesForPostSessionFailures(t *testing.T) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	now := time.Now()
	hasher, hash := testServiceHasher(t)
	tests := []struct {
		name       string
		updateErr  error
		resetErr   error
		wantEvents []string
	}{
		{
			name:       "last login update",
			updateErr:  errors.New("mysql unavailable"),
			wantEvents: []string{"allow", "find", "session-create", "update", "session-delete"},
		},
		{
			name:       "username counter reset",
			resetErr:   errors.New("redis unavailable"),
			wantEvents: []string{"allow", "find", "session-create", "update", "reset", "session-delete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := []string{}
			repo := &serviceRepositoryFake{
				adminByUsername: Admin{ID: 42, Username: "admin.user", PasswordHash: hash, State: "active"},
				updateErr:       tt.updateErr,
				events:          &events,
			}
			store := &serviceSessionStoreFake{deleteErr: errors.New("compensation also failed"), events: &events}
			limiter := &serviceLimiterFake{decision: LimitDecision{Allowed: true}, resetErr: tt.resetErr, events: &events}
			sessions := NewSessionManager(store, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), func() time.Time { return now })
			service := NewService(repo, hasher, sessions, limiter, func() time.Time { return now })

			result, err := service.Login(context.Background(), "admin.user", "correct-password", "192.0.2.10")

			require.Equal(t, LoginResult{}, result)
			require.ErrorIs(t, err, ErrDependencyUnavailable)
			require.Equal(t, tt.wantEvents, events)
			require.Equal(t, 1, store.deleteCalls)
			require.NotContains(t, err.Error(), "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")
		})
	}
}

func TestServiceLoginFailsClosedWhenRecordingInvalidCredentialsFails(t *testing.T) {
	now := time.Now()
	hasher, hash := testServiceHasher(t)
	repo := &serviceRepositoryFake{adminByUsername: Admin{ID: 42, Username: "admin.user", PasswordHash: hash, State: "active"}}
	store := &serviceSessionStoreFake{}
	limiter := &serviceLimiterFake{decision: LimitDecision{Allowed: true}, recordErr: errors.New("redis unavailable")}
	sessions := NewSessionManager(store, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), func() time.Time { return now })
	service := NewService(repo, hasher, sessions, limiter, func() time.Time { return now })

	result, err := service.Login(context.Background(), "admin.user", "wrong-password", "192.0.2.10")

	require.Equal(t, LoginResult{}, result)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.Equal(t, 1, limiter.recordCalls)
	require.Zero(t, store.setCalls)
}

func TestServiceLoginTreatsMalformedStoredHashAsSanitizedInternalFailure(t *testing.T) {
	now := time.Now()
	hasher, _ := testServiceHasher(t)
	repo := &serviceRepositoryFake{adminByUsername: Admin{
		ID:           42,
		Username:     "admin.user",
		PasswordHash: "$argon2id$malformed-secret-hash",
		State:        "active",
	}}
	store := &serviceSessionStoreFake{}
	limiter := &serviceLimiterFake{decision: LimitDecision{Allowed: true}}
	sessions := NewSessionManager(store, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), func() time.Time { return now })
	service := NewService(repo, hasher, sessions, limiter, func() time.Time { return now })

	result, err := service.Login(context.Background(), "admin.user", "correct-password", "192.0.2.10")

	require.Equal(t, LoginResult{}, result)
	require.Equal(t, ErrInternal, err)
	require.NotErrorIs(t, err, ErrInvalidCredentials)
	require.NotErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), "malformed-secret-hash")
	require.Zero(t, limiter.recordCalls)
	require.Zero(t, store.setCalls)
}

func TestServiceLoginLogsSanitizedCompensationFailure(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	now := time.Now()
	hasher, hash := testServiceHasher(t)
	repo := &serviceRepositoryFake{
		adminByUsername: Admin{ID: 42, Username: "admin.user", PasswordHash: hash, State: "active"},
		updateErr:       errors.New("mysql unavailable"),
	}
	store := &serviceSessionStoreFake{deleteErr: errors.New("redis password secret")}
	limiter := &serviceLimiterFake{decision: LimitDecision{Allowed: true}}
	sessions := NewSessionManager(store, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), func() time.Time { return now })
	service := NewServiceWithLogger(repo, hasher, sessions, limiter, func() time.Time { return now }, logger)

	_, err := service.Login(context.Background(), "admin.user", "correct-password", "192.0.2.10")

	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.Contains(t, logs.String(), "login session compensation failed")
	require.NotContains(t, logs.String(), "redis password secret")
	require.NotContains(t, logs.String(), "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")
	require.NotContains(t, logs.String(), "ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0")
}

func TestServiceLoginCompensationSurvivesRequestCancellationWithBoundedContext(t *testing.T) {
	now := time.Now()
	hasher, hash := testServiceHasher(t)
	repo := &serviceRepositoryFake{
		adminByUsername: Admin{ID: 42, Username: "admin.user", PasswordHash: hash, State: "active"},
		updateErr:       errors.New("mysql unavailable"),
	}
	store := &serviceSessionStoreFake{}
	limiter := &serviceLimiterFake{decision: LimitDecision{Allowed: true}}
	sessions := NewSessionManager(store, time.Hour, strings.NewReader(string(bytesFromZeroTo31())), func() time.Time { return now })
	service := NewService(repo, hasher, sessions, limiter, func() time.Time { return now })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.Login(ctx, "admin.user", "correct-password", "192.0.2.10")

	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.Equal(t, 1, store.deleteCalls)
	require.NoError(t, store.deleteContextErr)
	require.True(t, store.deleteHasDeadline)
	remaining := time.Until(store.deleteDeadline)
	require.Positive(t, remaining)
	require.LessOrEqual(t, remaining, 5*time.Second)
}

func TestServiceLogoutIsIdempotent(t *testing.T) {
	now := time.Now()
	store := &serviceSessionStoreFake{deleteErr: ErrSessionNotFound}
	service := newServiceForTest(t, &serviceRepositoryFake{}, store, &serviceLimiterFake{}, now)

	require.NoError(t, service.Logout(context.Background(), "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"))
	require.NoError(t, service.Logout(context.Background(), "malformed"))
}

func TestServiceCurrentRefetchesActiveAdminAndClearsPasswordHash(t *testing.T) {
	now := time.Now()
	store := &serviceSessionStoreFake{getSession: Session{AdminID: 42, Username: "stale-name", ExpiresAt: now.Add(time.Hour)}}
	repo := &serviceRepositoryFake{adminByID: Admin{ID: 42, Username: "current-name", PasswordHash: "must-not-leak", State: "active"}}
	service := newServiceForTest(t, repo, store, &serviceLimiterFake{}, now)

	admin, err := service.Current(context.Background(), "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")

	require.NoError(t, err)
	require.Equal(t, int64(42), repo.findID)
	require.Equal(t, Admin{ID: 42, Username: "current-name", State: "active"}, admin)
}

func TestServiceCurrentRejectsDisabledOrMissingAdminAndInvalidatesSession(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		admin   Admin
		findErr error
	}{
		{name: "disabled", admin: Admin{ID: 42, Username: "admin.user", PasswordHash: "must-not-leak", State: "disabled"}},
		{name: "deleted", findErr: ErrAdminNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &serviceSessionStoreFake{getSession: Session{AdminID: 42, Username: "admin.user", ExpiresAt: now.Add(time.Hour)}}
			repo := &serviceRepositoryFake{adminByID: tt.admin, findIDErr: tt.findErr}
			service := newServiceForTest(t, repo, store, &serviceLimiterFake{}, now)

			admin, err := service.Current(context.Background(), "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")

			require.Equal(t, Admin{}, admin)
			require.Equal(t, ErrUnauthenticated, err)
			require.Equal(t, 1, store.deleteCalls)
		})
	}
}

func TestServiceCurrentDistinguishesAbsentSessionFromDependencyFailure(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		getErr  error
		wantErr error
	}{
		{name: "absent", getErr: ErrSessionNotFound, wantErr: ErrUnauthenticated},
		{name: "store unavailable", getErr: errors.New("redis unavailable"), wantErr: ErrDependencyUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &serviceSessionStoreFake{getErr: tt.getErr}
			service := newServiceForTest(t, &serviceRepositoryFake{}, store, &serviceLimiterFake{}, now)

			admin, err := service.Current(context.Background(), "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")

			require.Equal(t, Admin{}, admin)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
