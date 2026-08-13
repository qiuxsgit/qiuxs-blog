package idgen

import (
	"context"
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

type RedisCounter struct {
	client *redis.Client
}

func NewRedisCounter(client *redis.Client) *RedisCounter {
	return &RedisCounter{client: client}
}

func (c *RedisCounter) Increment(ctx context.Context, key string) (int64, error) {
	return c.client.Incr(ctx, key).Result()
}

func (c *RedisCounter) Raise(ctx context.Context, key string, floor int64) (int64, error) {
	if floor <= 0 {
		return 0, fmt.Errorf("counter floor must be positive")
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
