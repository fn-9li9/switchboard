package redis

import (
	"context"
	"fmt"

	"switchboard/internal/config"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// NewClient crea y valida un cliente Redis.
func NewClient(ctx context.Context, cfg config.RedisConfig, log zerolog.Logger) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis: ping fallido: %w", err)
	}

	log.Info().
		Str("addr", cfg.Addr).
		Int("db", cfg.DB).
		Msg("redis conectado")

	return client, nil
}
