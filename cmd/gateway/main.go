package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"switchboard/internal/config"
	pgstore "switchboard/internal/store/postgres"
	"switchboard/services/gateway"
)

func main() {
	cfg, err := config.Load("gateway")
	if err != nil {
		os.Stderr.WriteString("error cargando config: " + err.Error() + "\n")
		os.Exit(1)
	}

	log := config.InitLogger(cfg.Env, cfg.Service)

	pool, err := pgstore.NewPool(context.Background(), cfg.Postgres, log)
	if err != nil {
		log.Fatal().Err(err).Msg("error conectando postgres")
	}
	defer pool.Close()

	srv := gateway.NewServer(cfg, log, pool)

	go func() {
		if err := srv.Start(); err != nil {
			log.Fatal().Err(err).Msg("servidor caído")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("apagando servidor...")
	srv.Stop()
}
