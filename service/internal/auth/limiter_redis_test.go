package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func openLimiterRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return server, client
}

func TestRedisLoginLimiterDeniesSixthUsernameAttemptUntilNextWindow(t *testing.T) {
	_, client := openLimiterRedis(t)
	now := time.Date(2026, time.August, 13, 12, 1, 30, 0, time.UTC)
	limiter := NewRedisLoginLimiter(client, func() time.Time { return now })
	ctx := context.Background()

	for range 5 {
		decision, err := limiter.Allow(ctx, "  Admin.User  ", "192.0.2.10")
		require.NoError(t, err)
		require.True(t, decision.Allowed)
		require.Zero(t, decision.RetryAfter)
		require.NoError(t, limiter.RecordFailure(ctx, "  Admin.User  ", "192.0.2.10"))
	}

	decision, err := limiter.Allow(ctx, "admin.user", "192.0.2.11")
	require.NoError(t, err)
	require.Equal(t, LimitDecision{Allowed: false, RetryAfter: 3*time.Minute + 30*time.Second}, decision)

	now = time.Date(2026, time.August, 13, 12, 5, 0, 0, time.UTC)
	decision, err = limiter.Allow(ctx, "ADMIN.USER", "192.0.2.11")
	require.NoError(t, err)
	require.Equal(t, LimitDecision{Allowed: true}, decision)
}

func TestRedisLoginLimiterDeniesTwentyFirstCanonicalIPAttempt(t *testing.T) {
	_, client := openLimiterRedis(t)
	now := time.Date(2026, time.August, 13, 12, 4, 59, 500_000_000, time.UTC)
	limiter := NewRedisLoginLimiter(client, func() time.Time { return now })
	ctx := context.Background()

	for attempt := 1; attempt <= 20; attempt++ {
		username := fmt.Sprintf("admin-%02d", attempt)
		require.NoError(t, limiter.RecordFailure(ctx, username, "192.0.2.20"))
	}

	decision, err := limiter.Allow(ctx, "another-admin", "::ffff:192.0.2.20")
	require.NoError(t, err)
	require.Equal(t, LimitDecision{Allowed: false, RetryAfter: 500 * time.Millisecond}, decision)

	now = time.Date(2026, time.August, 13, 12, 5, 0, 0, time.UTC)
	decision, err = limiter.Allow(ctx, "another-admin", "192.0.2.20")
	require.NoError(t, err)
	require.Equal(t, LimitDecision{Allowed: true}, decision)
}

func TestRedisLoginLimiterStoresOnlyDigestedCanonicalIdentitiesWithRemainingTTL(t *testing.T) {
	server, client := openLimiterRedis(t)
	now := time.Unix(1_800_000_125, 0).UTC()
	limiter := NewRedisLoginLimiter(client, func() time.Time { return now })

	require.NoError(t, limiter.RecordFailure(context.Background(), "  ADMIN.User ", "2001:0db8:0:0:0:0:0:1"))

	wantKeys := []string{
		"qiuxs-blog:login:ip:5afd19e856d1c18d17d600dfd2b5f534992333985e126c2a951047102c1ed536:1800000000",
		"qiuxs-blog:login:username:6e7d1e9378d2a020e2ff311169a44a1bd730269b5010d24d73a1a6fd2ac0084d:1800000000",
	}
	gotKeys := server.Keys()
	sort.Strings(gotKeys)
	require.Equal(t, wantKeys, gotKeys)
	for _, key := range gotKeys {
		require.NotContains(t, key, "admin.user")
		require.NotContains(t, key, "2001:db8::1")
		require.Equal(t, 2*time.Minute+55*time.Second, server.TTL(key))
	}

	server.FastForward(10 * time.Second)
	require.NoError(t, limiter.RecordFailure(context.Background(), "admin.user", "2001:db8::1"))
	for _, key := range gotKeys {
		require.Equal(t, 2*time.Minute+45*time.Second, server.TTL(key), "the second increment must not refresh expiration")
	}
}

func TestRedisLoginLimiterResetDeletesOnlyCurrentUsernameWindow(t *testing.T) {
	server, client := openLimiterRedis(t)
	now := time.Unix(1_800_000_125, 0).UTC()
	limiter := NewRedisLoginLimiter(client, func() time.Time { return now })
	ctx := context.Background()

	require.NoError(t, limiter.RecordFailure(ctx, "previous.user", "192.0.2.44"))
	previousUsernameKey := "qiuxs-blog:login:username:53e084cd98eaab2d058cfa0af9127d1c26ca16859bce38fbb7f0ffa89bfec184:1800000000"

	now = time.Unix(1_800_000_300, 0).UTC()
	require.NoError(t, limiter.RecordFailure(ctx, "previous.user", "192.0.2.44"))
	currentUsernameKey := "qiuxs-blog:login:username:53e084cd98eaab2d058cfa0af9127d1c26ca16859bce38fbb7f0ffa89bfec184:1800000300"
	currentIPKey := "qiuxs-blog:login:ip:1a17a4b9dec75a663e54e523037f05b0e5a5e13d023247a6b0f9774cc80829a2:1800000300"

	require.NoError(t, limiter.ResetUsername(ctx, " PREVIOUS.USER "))
	require.True(t, server.Exists(previousUsernameKey))
	require.False(t, server.Exists(currentUsernameKey))
	require.True(t, server.Exists(currentIPKey))
}

func TestRedisLoginLimiterConcurrentFailuresAreNotLost(t *testing.T) {
	server, client := openLimiterRedis(t)
	now := time.Unix(1_800_000_125, 0).UTC()
	limiter := NewRedisLoginLimiter(client, func() time.Time { return now })

	const failures = 40
	var group sync.WaitGroup
	errorsByAttempt := make(chan error, failures)
	for range failures {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsByAttempt <- limiter.RecordFailure(context.Background(), "admin.user", "2001:db8::1")
		}()
	}
	group.Wait()
	close(errorsByAttempt)
	for err := range errorsByAttempt {
		require.NoError(t, err)
	}

	keys := server.Keys()
	require.Len(t, keys, 2)
	for _, key := range keys {
		value, err := server.Get(key)
		require.NoError(t, err)
		require.Equal(t, "40", value)
	}
}

func TestRedisLoginLimiterRunsBothCountersWhenOneIncrementFails(t *testing.T) {
	server, client := openLimiterRedis(t)
	now := time.Unix(1_800_000_125, 0).UTC()
	limiter := NewRedisLoginLimiter(client, func() time.Time { return now })
	ctx := context.Background()
	usernameKey := "qiuxs-blog:login:username:6e7d1e9378d2a020e2ff311169a44a1bd730269b5010d24d73a1a6fd2ac0084d:1800000000"
	ipKey := "qiuxs-blog:login:ip:5afd19e856d1c18d17d600dfd2b5f534992333985e126c2a951047102c1ed536:1800000000"
	require.NoError(t, server.Set(usernameKey, "not-an-integer"))

	err := limiter.RecordFailure(ctx, "admin.user", "2001:db8::1")

	require.Error(t, err)
	ipFailures, getErr := server.Get(ipKey)
	require.NoError(t, getErr)
	require.Equal(t, "1", ipFailures)
	require.NotContains(t, err.Error(), "admin.user")
	require.NotContains(t, err.Error(), "2001:db8::1")
	require.NotContains(t, err.Error(), "6e7d1e")
}

func TestRedisLoginLimiterFailsClosedWithSanitizedRedisErrors(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	limiter := NewRedisLoginLimiter(client, time.Now)
	require.NoError(t, client.Close())
	const username = "private.admin"
	const ip = "203.0.113.99"

	decision, allowErr := limiter.Allow(context.Background(), username, ip)
	require.Equal(t, LimitDecision{}, decision)
	require.Error(t, allowErr)
	recordErr := limiter.RecordFailure(context.Background(), username, ip)
	require.Error(t, recordErr)
	resetErr := limiter.ResetUsername(context.Background(), username)
	require.Error(t, resetErr)

	for _, err := range []error{allowErr, recordErr, resetErr} {
		require.NotContains(t, err.Error(), username)
		require.NotContains(t, err.Error(), ip)
		require.NotContains(t, strings.ToLower(err.Error()), "redis")
		require.NotContains(t, err.Error(), server.Addr())
	}
}

func TestRedisLoginLimiterRejectsInvalidInputAndZeroValuesSafely(t *testing.T) {
	_, client := openLimiterRedis(t)
	valid := NewRedisLoginLimiter(client, time.Now)
	ctx := context.Background()

	for _, call := range []func() error{
		func() error { _, err := valid.Allow(ctx, "   ", "192.0.2.1"); return err },
		func() error { _, err := valid.Allow(ctx, "admin", "not-an-ip"); return err },
		func() error { return valid.RecordFailure(ctx, "admin", "192.0.2.1:443") },
		func() error { return valid.ResetUsername(ctx, "") },
		func() error { _, err := valid.Allow(nil, "admin", "192.0.2.1"); return err },
	} {
		err := call()
		require.Error(t, err)
		require.NotContains(t, err.Error(), "not-an-ip")
		require.NotContains(t, err.Error(), "192.0.2.1:443")
	}

	var zero RedisLoginLimiter
	var nilLimiter *RedisLoginLimiter
	for _, limiter := range []*RedisLoginLimiter{
		&zero,
		nilLimiter,
		NewRedisLoginLimiter(nil, time.Now),
		NewRedisLoginLimiter(client, nil),
	} {
		decision, err := limiter.Allow(ctx, "admin", "192.0.2.1")
		require.Equal(t, LimitDecision{}, decision)
		require.Error(t, err)
		require.Error(t, limiter.RecordFailure(ctx, "admin", "192.0.2.1"))
		require.Error(t, limiter.ResetUsername(ctx, "admin"))
	}
}

func TestRedisLoginLimiterFailsClosedOnMalformedCounter(t *testing.T) {
	server, client := openLimiterRedis(t)
	now := time.Unix(1_800_000_125, 0).UTC()
	limiter := NewRedisLoginLimiter(client, func() time.Time { return now })
	key := "qiuxs-blog:login:username:6e7d1e9378d2a020e2ff311169a44a1bd730269b5010d24d73a1a6fd2ac0084d:1800000000"
	require.NoError(t, server.Set(key, "not-a-counter"))

	decision, err := limiter.Allow(context.Background(), "admin.user", "2001:db8::1")

	require.Equal(t, LimitDecision{}, decision)
	require.Error(t, err)
	require.False(t, errors.Is(err, redis.Nil))
	require.NotContains(t, err.Error(), key)
}
