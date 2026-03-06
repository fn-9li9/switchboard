package postgres

import (
	"context"
	"fmt"
	"time"

	"switchboard/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// NewPool crea y valida un pool de conexiones pgx/v5.
func NewPool(ctx context.Context, cfg config.PostgresConfig, log zerolog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: DSN inválido: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.MaxConns)
	poolCfg.MinConns = int32(cfg.MinConns)
	poolCfg.MaxConnIdleTime = time.Duration(cfg.MaxIdleSecs) * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: error creando pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("postgres: ping fallido: %w", err)
	}

	log.Info().
		Str("dsn_host", poolCfg.ConnConfig.Host).
		Int32("max_conns", poolCfg.MaxConns).
		Msg("postgres conectado")

	return pool, nil
}
