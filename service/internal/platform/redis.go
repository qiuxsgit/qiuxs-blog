package platform

import (
	"context"
	"errors"
	"strings"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/redis/go-redis/v9"
)

func OpenRedis(cfg config.RedisConfig) (*redis.Client, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, errors.New("redis address is required")
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), dependencyTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, errors.New("ping redis connection")
	}

	return client, nil
}
