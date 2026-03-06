package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Set guarda un valor con TTL. TTL=0 significa sin expiración.
func Set(ctx context.Context, client *redis.Client, key, value string, ttl time.Duration) error {
	if err := client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("redis: Set %q: %w", key, err)
	}
	return nil
}

// Get devuelve el valor o ("", false, nil) si la clave no existe.
func Get(ctx context.Context, client *redis.Client, key string) (string, bool, error) {
	val, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("redis: Get %q: %w", key, err)
	}
	return val, true, nil
}

// Del elimina una o más claves.
func Del(ctx context.Context, client *redis.Client, keys ...string) error {
	if err := client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis: Del %v: %w", keys, err)
	}
	return nil
}

// TTL devuelve el tiempo restante de una clave. -1 si no tiene TTL, -2 si no existe.
func TTL(ctx context.Context, client *redis.Client, key string) (time.Duration, error) {
	d, err := client.TTL(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("redis: TTL %q: %w", key, err)
	}
	return d, nil
}
