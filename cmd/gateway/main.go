package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"switchboard/internal/config"
	pgstore "switchboard/internal/store/postgres"
	rstore "switchboard/internal/store/redis"
	"switchboard/services/gateway"
)

func main() {
	cfg, err := config.Load("gateway")
	if err != nil {
		os.Stderr.WriteString("error loading config: " + err.Error() + "\n")
		os.Exit(1)
	}

	log := config.InitLogger(cfg.Env, cfg.Service)

	pool, err := pgstore.NewPool(context.Background(), cfg.Postgres, log)
	if err != nil {
		log.Fatal().Err(err).Msg("error connecting postgres")
	}
	defer pool.Close()

	rdb, err := rstore.NewClient(context.Background(), cfg.Redis, log)
	if err != nil {
		log.Fatal().Err(err).Msg("error connecting redis")
	}
	defer rdb.Close()

	srv := gateway.NewServer(cfg, log, pool, rdb)

	go func() {
		if err := srv.Start(); err != nil {
			log.Fatal().Err(err).Msg("server down")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down...")
	srv.Stop()
}
