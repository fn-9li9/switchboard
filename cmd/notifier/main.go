package main

import (
	"os"
	"os/signal"
	"syscall"

	"switchboard/internal/config"
	natsclient "switchboard/internal/messaging/nats"
	"switchboard/services/notifier"
)

func main() {
	cfg, err := config.Load("notifier")
	if err != nil {
		os.Stderr.WriteString("error loading config: " + err.Error() + "\n")
		os.Exit(1)
	}

	log := config.InitLogger(cfg.Env, cfg.Service)

	nc, err := natsclient.NewConn(cfg.NATS, log)
	if err != nil {
		log.Fatal().Err(err).Msg("error connecting nats")
	}
	defer nc.Close()

	srv := notifier.NewServer(cfg, log, nc)

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
