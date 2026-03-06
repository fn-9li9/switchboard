package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Publish emite un mensaje a un canal Redis pub/sub.
func Publish(ctx context.Context, client *redis.Client, channel, message string) error {
	if err := client.Publish(ctx, channel, message).Err(); err != nil {
		return fmt.Errorf("redis: Publish %q: %w", channel, err)
	}
	return nil
}

// Subscribe suscribe a uno o más canales y devuelve el PubSub.
// El caller es responsable de cerrar con pubsub.Close().
func Subscribe(ctx context.Context, client *redis.Client, channels ...string) *redis.PubSub {
	return client.Subscribe(ctx, channels...)
}
