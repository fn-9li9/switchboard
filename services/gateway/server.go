package gateway

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"time"

	"switchboard/internal/config"
	wstransport "switchboard/internal/transport/ws"
	"switchboard/services/gateway/handlers"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Server struct {
	cfg      *config.Config
	log      zerolog.Logger
	http     *http.Server
	pool     *pgxpool.Pool
	rdb      *redis.Client
	nc       *nats.Conn
	hub      *wstransport.Hub
	producer sarama.SyncProducer
}

func NewServer(cfg *config.Config, log zerolog.Logger, pool *pgxpool.Pool, rdb *redis.Client, nc *nats.Conn, producer sarama.SyncProducer) *Server {
	s := &Server{cfg: cfg, log: log, pool: pool, rdb: rdb, nc: nc, producer: producer}

	s.hub = wstransport.NewHub()
	go s.hub.Run()

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.http = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0,
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
	evh := handlers.NewEventsHandler(s.pool, s.log, s.nc)
	rdh := handlers.NewRedisHandler(s.rdb, s.log, s.nc)
	nath := handlers.NewNATSHandler(s.nc, s.log)
	wsh := handlers.NewWSHandler(s.hub, s.log, s.nc)
	kafkah := handlers.NewKafkaHandler(s.producer, s.nc, s.log)
	firehose := handlers.NewFirehoseHandler(s.nc, s.log)

	indexTmpl := mustPage("services/gateway/templates/index.html")
	postgresTmpl := mustPage("services/gateway/templates/partials/events.html")
	redisTmpl := mustPage("services/gateway/templates/partials/redis.html")
	natsTmpl := mustPage("services/gateway/templates/partials/nats.html")
	firehoseTmpl := mustPage("services/gateway/templates/partials/firehose.html")
	wsTmpl := mustPage("services/gateway/templates/partials/ws.html")
	kafkaTmpl := mustPage("services/gateway/templates/partials/kafka.html")

	// Pages
	mux.HandleFunc("GET /{$}", s.renderPage(indexTmpl))
	mux.HandleFunc("GET /postgres", s.renderPage(postgresTmpl))
	mux.HandleFunc("GET /redis", s.renderPage(redisTmpl))
	mux.HandleFunc("GET /nats", s.renderPage(natsTmpl))
	mux.HandleFunc("GET /ws", s.renderPage(wsTmpl))
	mux.HandleFunc("GET /kafka", s.renderPage(kafkaTmpl))

	// Health
	mux.HandleFunc("GET /health", s.handleHealth)
	// Health checks proxy — evita CORS en el browser
	mux.HandleFunc("GET /health/gateway", s.handleHealth)
	mux.HandleFunc("GET /health/notifier", s.proxyHealth("http://localhost:8081/health"))
	mux.HandleFunc("GET /health/processor", s.proxyHealth("http://localhost:8082/health"))

	// Postgres
	mux.HandleFunc("GET /events", evh.List)
	mux.HandleFunc("POST /events", evh.Create)
	mux.HandleFunc("DELETE /events/{id}", evh.Delete)

	// Redis
	mux.HandleFunc("POST /redis/set", rdh.Set)
	mux.HandleFunc("GET /redis/get", rdh.Get)
	mux.HandleFunc("DELETE /redis/del", rdh.Del)
	mux.HandleFunc("POST /redis/publish", rdh.Publish)
	mux.HandleFunc("GET /redis/subscribe", rdh.Subscribe)

	// NATS
	mux.HandleFunc("POST /nats/request", nath.Request)
	mux.HandleFunc("GET /nats/subscribe", nath.Subscribe)

	// WebSocket
	mux.HandleFunc("GET /ws/connect", wsh.Connect)
	mux.HandleFunc("GET /ws/count", wsh.Count)

	// Kafka
	mux.HandleFunc("POST /kafka/produce", kafkah.Produce)
	mux.HandleFunc("GET /kafka/results", kafkah.Results) // SSE — resultados del processor

	// Firehose
	mux.HandleFunc("GET /firehose", s.renderPage(firehoseTmpl))
	mux.HandleFunc("GET /firehose/stream", firehose.Stream)
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

func (s *Server) proxyHealth(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(target)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"error","service":"unreachable"}`))
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}
