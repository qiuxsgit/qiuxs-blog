package idgen

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

var raiseCounterScript = redis.NewScript(`
local function normalize(value)
  local normalized = string.gsub(value, '^0+', '')
  if normalized == '' then return '0' end
  return normalized
end
local current = normalize(redis.call('GET', KEYS[1]) or '0')
local floor = normalize(ARGV[1])
local should_raise = #current < #floor or (#current == #floor and current < floor)
if should_raise then
  redis.call('SET', KEYS[1], floor)
  return floor
end
return current
`)

var errRedisCounterNotConfigured = errors.New("redis counter is not configured")

type RedisCounter struct {
	client *redis.Client
}

func NewRedisCounter(client *redis.Client) *RedisCounter {
	return &RedisCounter{client: client}
}

func (c *RedisCounter) Increment(ctx context.Context, key string) (int64, error) {
	if err := c.configurationError(); err != nil {
		return 0, err
	}
	return c.client.Incr(ctx, key).Result()
}

func (c *RedisCounter) Raise(ctx context.Context, key string, floor int64) (int64, error) {
	// Preserve domain validation before dependency validation so an invalid
	// floor has the same result regardless of Redis wiring.
	if floor <= 0 {
		return 0, fmt.Errorf("counter floor must be positive")
	}
	if err := c.configurationError(); err != nil {
		return 0, err
	}

	value, err := raiseCounterScript.Run(
		ctx,
		c.client,
		[]string{key},
		strconv.FormatInt(floor, 10),
	).Text()
	if err != nil {
		return 0, fmt.Errorf("raise redis counter %q: %w", key, err)
	}

	raised, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse redis counter %q: %w", key, err)
	}
	return raised, nil
}

func (c *RedisCounter) configurationError() error {
	if c == nil || c.client == nil {
		return errRedisCounterNotConfigured
	}
	return nil
}
