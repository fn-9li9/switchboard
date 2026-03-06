package main

import (
	"os"
	"os/signal"
	"syscall"

	"switchboard/internal/config"
	"switchboard/services/notifier"
)

func main() {
	cfg, err := config.Load("notifier")
	if err != nil {
		os.Stderr.WriteString("error cargando config: " + err.Error() + "\n")
		os.Exit(1)
	}

	log := config.InitLogger(cfg.Env, cfg.Service)

	srv := notifier.NewServer(cfg, log)

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
