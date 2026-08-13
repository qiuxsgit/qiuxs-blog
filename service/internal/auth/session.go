package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"time"
)

const sessionTokenBytes = 32

// Session is the server-side state associated with an opaque admin session token.
type Session struct {
	AdminID   int64     `json:"adminId"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// SessionStore persists sessions by a token digest, never the raw token.
type SessionStore interface {
	Set(ctx context.Context, digest string, session Session, ttl time.Duration) error
	Get(ctx context.Context, digest string) (Session, error)
	Delete(ctx context.Context, digest string) error
}

// SessionManager creates and resolves opaque admin session tokens.
//
// NewSessionManager records invalid dependencies because its established API
// cannot return an error. Calls then fail safely rather than panic.
type SessionManager struct {
	store   SessionStore
	ttl     time.Duration
	random  io.Reader
	now     func() time.Time
	initErr error
}

func NewSessionManager(store SessionStore, ttl time.Duration, random io.Reader, now func() time.Time) SessionManager {
	manager := SessionManager{store: store, ttl: ttl, random: random, now: now}
	switch {
	case isNilDependency(store):
		manager.initErr = errors.New("session store is required")
	case ttl <= 0:
		manager.initErr = errors.New("session TTL must be positive")
	case isNilDependency(random):
		manager.initErr = errors.New("session random source is required")
	case now == nil:
		manager.initErr = errors.New("session clock is required")
	}
	return manager
}

func (m SessionManager) Create(ctx context.Context, admin Admin) (string, Session, error) {
	if err := m.configurationError(); err != nil {
		return "", Session{}, err
	}
	if admin.ID <= 0 || admin.Username == "" {
		return "", Session{}, errors.New("invalid session admin")
	}

	randomBytes := make([]byte, sessionTokenBytes)
	if _, err := io.ReadFull(m.random, randomBytes); err != nil {
		return "", Session{}, errors.New("generate session token")
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	session := Session{
		AdminID:   admin.ID,
		Username:  admin.Username,
		ExpiresAt: m.now().Add(m.ttl),
	}
	if err := m.store.Set(ctx, sessionDigest(token), session, m.ttl); err != nil {
		return "", Session{}, errors.New("store session")
	}
	return token, session, nil
}

func (m SessionManager) Get(ctx context.Context, token string) (Session, error) {
	if err := m.configurationError(); err != nil {
		return Session{}, err
	}
	digest, ok := sessionTokenDigest(token)
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	session, err := m.store.Get(ctx, digest)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return Session{}, ErrSessionNotFound
		}
		return Session{}, errors.New("load session")
	}
	if !validSession(session, m.now()) {
		return Session{}, ErrSessionNotFound
	}
	return session, nil
}

func (m SessionManager) Delete(ctx context.Context, token string) error {
	if err := m.configurationError(); err != nil {
		return err
	}
	digest, ok := sessionTokenDigest(token)
	if !ok {
		return ErrSessionNotFound
	}
	if err := m.store.Delete(ctx, digest); err != nil && !errors.Is(err, ErrSessionNotFound) {
		return errors.New("delete session")
	}
	return nil
}

func sessionTokenDigest(token string) (string, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != sessionTokenBytes || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return "", false
	}
	return sessionDigest(token), true
}

func sessionDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func validSession(session Session, now time.Time) bool {
	return session.AdminID > 0 && session.Username != "" && session.ExpiresAt.After(now)
}

func (m SessionManager) configurationError() error {
	if m.initErr != nil {
		return m.initErr
	}
	return nil
}

func isNilDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
