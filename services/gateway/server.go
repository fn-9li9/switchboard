package gateway

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"switchboard/internal/config"
	"switchboard/services/gateway/handlers"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Server struct {
	cfg  *config.Config
	log  zerolog.Logger
	http *http.Server
	pool *pgxpool.Pool
	rdb  *redis.Client
}

func NewServer(cfg *config.Config, log zerolog.Logger, pool *pgxpool.Pool, rdb *redis.Client) *Server {
	s := &Server{cfg: cfg, log: log, pool: pool, rdb: rdb}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.http = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // 0 = sin timeout para SSE
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func mustPage(partial string) *template.Template {
	return template.Must(template.ParseFiles(
		"services/gateway/templates/base.html",
		partial,
	))
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	evh := handlers.NewEventsHandler(s.pool, s.log)
	rdh := handlers.NewRedisHandler(s.rdb, s.log)

	indexTmpl := mustPage("services/gateway/templates/index.html")
	postgresTmpl := mustPage("services/gateway/templates/partials/events.html")
	redisTmpl := mustPage("services/gateway/templates/partials/redis.html")

	// Pages
	mux.HandleFunc("GET /{$}", s.renderPage(indexTmpl))
	mux.HandleFunc("GET /postgres", s.renderPage(postgresTmpl))
	mux.HandleFunc("GET /redis", s.renderPage(redisTmpl))

	// Health
	mux.HandleFunc("GET /health", s.handleHealth)

	// Postgres
	mux.HandleFunc("GET /events", evh.List)
	mux.HandleFunc("POST /events", evh.Create)
	mux.HandleFunc("DELETE /events/{id}", evh.Delete)

	// Redis
	mux.HandleFunc("POST /redis/set", rdh.Set)
	mux.HandleFunc("GET /redis/get", rdh.Get)
	mux.HandleFunc("DELETE /redis/del", rdh.Del)
	mux.HandleFunc("POST /redis/publish", rdh.Publish)
	mux.HandleFunc("GET /redis/subscribe", rdh.Subscribe) // SSE
}

func (s *Server) renderPage(tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if err := tmpl.ExecuteTemplate(w, "base.html", nil); err != nil {
			s.log.Error().Err(err).Msg("render page")
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"gateway"}`))
}

func (s *Server) Start() error {
	s.log.Info().Str("addr", s.http.Addr).Msg("gateway listening")
	return s.http.ListenAndServe()
}

func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.http.Shutdown(ctx); err != nil {
		s.log.Error().Err(err).Msg("shutdown error")
	}
}
