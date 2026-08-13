package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	loginLimitWindow          = 5 * time.Minute
	loginUsernameFailureLimit = int64(5)
	loginIPFailureLimit       = int64(20)
	redisLoginKeyPrefix       = "qiuxs-blog:login:"
)

const incrementLoginFailureScript = `
local value = redis.call('INCR', KEYS[1])
if value == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return value
`

// RedisLoginLimiter stores fixed-window failed-login counters in Redis.
type RedisLoginLimiter struct {
	client  *redis.Client
	now     func() time.Time
	initErr error
}

// NewRedisLoginLimiter constructs a failed-login limiter. Invalid dependencies
// are recorded so calls fail closed rather than panic.
func NewRedisLoginLimiter(client *redis.Client, now func() time.Time) *RedisLoginLimiter {
	limiter := &RedisLoginLimiter{client: client, now: now}
	if client == nil || now == nil {
		limiter.initErr = errors.New("login limiter dependencies are required")
	}
	return limiter
}

// Allow checks the username and IP counters for the current fixed window.
func (l *RedisLoginLimiter) Allow(ctx context.Context, username, ip string) (LimitDecision, error) {
	if err := l.configurationError(); err != nil {
		return LimitDecision{}, err
	}
	if ctx == nil {
		return LimitDecision{}, errors.New("login limiter context is required")
	}

	normalizedUsername, canonicalIP, err := loginLimitIdentities(username, ip)
	if err != nil {
		return LimitDecision{}, err
	}
	windowStart, remaining := loginLimitWindowAt(l.now())
	values, err := l.client.MGet(
		ctx,
		loginLimitKey("username", normalizedUsername, windowStart),
		loginLimitKey("ip", canonicalIP, windowStart),
	).Result()
	if err != nil {
		return LimitDecision{}, errors.New("read login limits")
	}
	if len(values) != 2 {
		return LimitDecision{}, errors.New("read login limits")
	}

	usernameFailures, ok := loginFailureCount(values[0])
	if !ok {
		return LimitDecision{}, errors.New("read login limits")
	}
	ipFailures, ok := loginFailureCount(values[1])
	if !ok {
		return LimitDecision{}, errors.New("read login limits")
	}
	if usernameFailures >= loginUsernameFailureLimit || ipFailures >= loginIPFailureLimit {
		return LimitDecision{Allowed: false, RetryAfter: remaining}, nil
	}
	return LimitDecision{Allowed: true}, nil
}

// RecordFailure atomically increments and initializes expiration for each
// identity counter. Both increments are sent even if one of them fails.
func (l *RedisLoginLimiter) RecordFailure(ctx context.Context, username, ip string) error {
	if err := l.configurationError(); err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("login limiter context is required")
	}

	normalizedUsername, canonicalIP, err := loginLimitIdentities(username, ip)
	if err != nil {
		return err
	}
	windowStart, remaining := loginLimitWindowAt(l.now())
	expirationSeconds := int64((remaining + time.Second - 1) / time.Second)

	pipeline := l.client.Pipeline()
	pipeline.Eval(ctx, incrementLoginFailureScript, []string{
		loginLimitKey("username", normalizedUsername, windowStart),
	}, expirationSeconds)
	pipeline.Eval(ctx, incrementLoginFailureScript, []string{
		loginLimitKey("ip", canonicalIP, windowStart),
	}, expirationSeconds)
	if _, err := pipeline.Exec(ctx); err != nil {
		return errors.New("record login failure")
	}
	return nil
}

// ResetUsername removes only the username counter in the current window.
func (l *RedisLoginLimiter) ResetUsername(ctx context.Context, username string) error {
	if err := l.configurationError(); err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("login limiter context is required")
	}
	normalizedUsername, err := normalizeLoginLimitUsername(username)
	if err != nil {
		return err
	}
	windowStart, _ := loginLimitWindowAt(l.now())
	if err := l.client.Del(ctx, loginLimitKey("username", normalizedUsername, windowStart)).Err(); err != nil {
		return errors.New("reset login failures")
	}
	return nil
}

func (l *RedisLoginLimiter) configurationError() error {
	if l == nil || l.client == nil || l.now == nil || l.initErr != nil {
		return errors.New("login limiter is not configured")
	}
	return nil
}

func loginLimitIdentities(username, ip string) (string, string, error) {
	normalizedUsername, err := normalizeLoginLimitUsername(username)
	if err != nil {
		return "", "", err
	}
	address, err := netip.ParseAddr(ip)
	if err != nil {
		return "", "", errors.New("invalid login limiter input")
	}
	return normalizedUsername, address.Unmap().String(), nil
}

func normalizeLoginLimitUsername(username string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(username))
	if normalized == "" {
		return "", errors.New("invalid login limiter input")
	}
	return normalized, nil
}

func loginLimitWindowAt(now time.Time) (int64, time.Duration) {
	const windowSeconds = int64(loginLimitWindow / time.Second)
	unix := now.Unix()
	windowStart := unix - unix%windowSeconds
	if unix < 0 && unix%windowSeconds != 0 {
		windowStart -= windowSeconds
	}
	windowEnd := time.Unix(windowStart+windowSeconds, 0).UTC()
	return windowStart, windowEnd.Sub(now)
}

func loginLimitKey(kind, identity string, windowStart int64) string {
	digest := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("%s%s:%s:%d", redisLoginKeyPrefix, kind, hex.EncodeToString(digest[:]), windowStart)
}

func loginFailureCount(value any) (int64, bool) {
	if value == nil {
		return 0, true
	}
	raw, ok := value.(string)
	if !ok {
		return 0, false
	}
	count, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || count < 0 {
		return 0, false
	}
	return count, true
}
