package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"switchboard/internal/config"
	"switchboard/internal/events"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

type Server struct {
	cfg  *config.Config
	log  zerolog.Logger
	http *http.Server
	nc   *nats.Conn
}

func NewServer(cfg *config.Config, log zerolog.Logger, nc *nats.Conn) *Server {
	s := &Server{cfg: cfg, log: log, nc: nc}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.http = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", s.handleHealth)
}

// Reply es la estructura que el notifier devuelve al gateway.
type Reply struct {
	Service     string `json:"service"`
	Message     string `json:"message"`
	ProcessedAt string `json:"processed_at"`
}

// StartNATSWorker suscribe al subject switchboard.request y responde.
func (s *Server) StartNATSWorker() error {
	_, err := s.nc.Subscribe("switchboard.request", func(msg *nats.Msg) {
		s.log.Info().
			Str("subject", msg.Subject).
			Str("payload", string(msg.Data)).
			Msg("nats: request received")

		reply := Reply{
			Service:     "notifier",
			Message:     fmt.Sprintf("echo: %s", string(msg.Data)),
			ProcessedAt: time.Now().Format(time.RFC3339Nano),
		}

		data, _ := json.Marshal(reply)

		if err := msg.Respond(data); err != nil {
			s.log.Error().Err(err).Msg("nats: respond error")
		}

		events.Emit(s.nc, s.log, events.FirehoseEvent{
			Type:    events.TypeNATS,
			Service: "notifier",
			Action:  "reply",
			Payload: fmt.Sprintf("subject:%s", msg.Subject),
		})
	})
	return err
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"notifier"}`))
}

func (s *Server) Start() error {
	if err := s.StartNATSWorker(); err != nil {
		return fmt.Errorf("nats worker: %w", err)
	}
	s.log.Info().Str("addr", s.http.Addr).Msg("notifier listening")
	return s.http.ListenAndServe()
}

func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.nc.Drain()
	if err := s.http.Shutdown(ctx); err != nil {
		s.log.Error().Err(err).Msg("shutdown error")
	}
}
