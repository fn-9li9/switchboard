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
	"github.com/rs/zerolog"
)

type Server struct {
	cfg  *config.Config
	log  zerolog.Logger
	http *http.Server
	pool *pgxpool.Pool
}

func NewServer(cfg *config.Config, log zerolog.Logger, pool *pgxpool.Pool) *Server {
	s := &Server{cfg: cfg, log: log, pool: pool}

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

// mustPage parsea base.html + el partial indicado en un template set único.
// Ejecutar "base.html" invoca {{template "content" .}} que resuelve al partial.
func mustPage(partial string) *template.Template {
	// Leer ambos archivos como strings y parsear juntos
	tmpl := template.Must(template.New("base.html").ParseFiles(
		"services/gateway/templates/base.html",
		partial,
	))
	return tmpl
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	evh := handlers.NewEventsHandler(s.pool, s.log)

	indexTmpl := mustPage("services/gateway/templates/index.html")
	postgresTmpl := mustPage("services/gateway/templates/partials/events.html")

	mux.HandleFunc("GET /{$}", s.renderPage(indexTmpl))
	mux.HandleFunc("GET /postgres", s.renderPage(postgresTmpl))
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /events", evh.List)
	mux.HandleFunc("POST /events", evh.Create)
	mux.HandleFunc("DELETE /events/{id}", evh.Delete)
}

func (s *Server) renderPage(tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// DEBUG: lista todos los templates en el set
		for _, t := range tmpl.Templates() {
			s.log.Debug().Str("name", t.Name()).Msg("template disponible")
		}
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
	s.log.Info().Str("addr", s.http.Addr).Msg("gateway escuchando")
	return s.http.ListenAndServe()
}

func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.http.Shutdown(ctx); err != nil {
		s.log.Error().Err(err).Msg("error en shutdown")
	}
}
