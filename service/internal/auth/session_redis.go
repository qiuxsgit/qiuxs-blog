package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisSessionKeyPrefix = "qiuxs-blog:session:"

// RedisSessionStore stores session records in Redis by SHA-256 token digest.
type RedisSessionStore struct {
	client  *redis.Client
	now     func() time.Time
	initErr error
}

// NewRedisSessionStore constructs a session store. Missing dependencies are
// recorded so subsequent calls return a safe configuration error rather than
// panicking.
func NewRedisSessionStore(client *redis.Client, now func() time.Time) *RedisSessionStore {
	store := &RedisSessionStore{client: client, now: now}
	if client == nil || now == nil {
		store.initErr = errors.New("redis session client is required")
	}
	return store
}

func (s *RedisSessionStore) Set(ctx context.Context, digest string, session Session, ttl time.Duration) error {
	if err := s.configurationError(); err != nil {
		return err
	}
	if !validSession(session, s.now()) || ttl <= 0 || !validSessionDigest(digest) {
		return errors.New("invalid session record")
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		return errors.New("encode session")
	}
	if err := s.client.Set(ctx, redisSessionKey(digest), encoded, ttl).Err(); err != nil {
		return errors.New("store session in redis")
	}
	return nil
}

func (s *RedisSessionStore) Get(ctx context.Context, digest string) (Session, error) {
	if err := s.configurationError(); err != nil {
		return Session{}, err
	}
	if !validSessionDigest(digest) {
		return Session{}, ErrSessionNotFound
	}
	raw, err := s.client.Get(ctx, redisSessionKey(digest)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, errors.New("load session from redis")
	}

	session, ok := decodeStoredSession(raw)
	if !ok || !validSession(session, s.now()) {
		_ = s.client.Del(ctx, redisSessionKey(digest)).Err()
		return Session{}, ErrSessionNotFound
	}
	return session, nil
}

func (s *RedisSessionStore) Delete(ctx context.Context, digest string) error {
	if err := s.configurationError(); err != nil {
		return err
	}
	if !validSessionDigest(digest) {
		return errors.New("invalid session record")
	}
	if err := s.client.Del(ctx, redisSessionKey(digest)).Err(); err != nil {
		return errors.New("delete session from redis")
	}
	return nil
}

func (s *RedisSessionStore) configurationError() error {
	if s == nil || s.client == nil || s.now == nil || s.initErr != nil {
		return errors.New("redis session store is not configured")
	}
	return nil
}

func redisSessionKey(digest string) string {
	return redisSessionKeyPrefix + digest
}

func validSessionDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	for _, character := range digest {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func decodeStoredSession(raw []byte) (Session, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var session Session
	if err := decoder.Decode(&session); err != nil {
		return Session{}, false
	}
	var extra any
	return session, errors.Is(decoder.Decode(&extra), io.EOF)
}
