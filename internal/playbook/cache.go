package playbook

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache adapts *redis.Client to the store's Cache interface.
type RedisCache struct{ R *redis.Client }

func (c RedisCache) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := c.R.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (c RedisCache) Set(ctx context.Context, key, val string, ttl time.Duration) error {
	return c.R.Set(ctx, key, val, ttl).Err()
}

func (c RedisCache) SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error) {
	return c.R.SetNX(ctx, key, val, ttl).Result()
}

func (c RedisCache) Del(ctx context.Context, key string) error {
	return c.R.Del(ctx, key).Err()
}
