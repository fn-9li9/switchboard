package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"switchboard/internal/config"

	"github.com/rs/zerolog"
)

type Server struct {
	cfg  *config.Config
	log  zerolog.Logger
	http *http.Server
}

func NewServer(cfg *config.Config, log zerolog.Logger) *Server {
	s := &Server{cfg: cfg, log: log}

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
	// TODO: rutas HTMX, SSE, WebSocket se agregan en siguientes pasos
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"gateway"}`))
}

func (s *Server) Start() error {
	s.log.Info().
		Str("addr", s.http.Addr).
		Msg("gateway escuchando")
	return s.http.ListenAndServe()
}

func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.http.Shutdown(ctx); err != nil {
		s.log.Error().Err(err).Msg("error en shutdown")
	}
}
