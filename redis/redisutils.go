package redisutils

import (
	"context"
	"fmt"
	"github.com/allisson/go-env"
	"github.com/redis/go-redis/v9"
	"time"
)

// New создает новое подключение к Redis.
func New(ctx context.Context) (*redis.Client, error) {
	addr := env.GetString("REDIS_ADDRESS", "")
	pass := env.GetString("REDIS_PASSWORD", "")

	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: pass,
	})

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to Redis: %w", err)
	}

	return rdb, nil
}
