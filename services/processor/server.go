package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"switchboard/internal/config"
	"switchboard/internal/events"
	kafkaclient "switchboard/internal/messaging/kafka"
	natsclient "switchboard/internal/messaging/nats"
	pgstore "switchboard/internal/store/postgres"

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
	producer sarama.SyncProducer
	consumer *kafkaclient.ConsumerGroup
}

func NewServer(cfg *config.Config, log zerolog.Logger, pool *pgxpool.Pool, rdb *redis.Client, nc *nats.Conn, producer sarama.SyncProducer) *Server {
	s := &Server{cfg: cfg, log: log, pool: pool, rdb: rdb, nc: nc, producer: producer}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)

	s.http = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// processMessage consume un mensaje Kafka, escribe en Postgres y emite a NATS.
func (s *Server) processMessage(msg *sarama.ConsumerMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s.log.Info().
		Str("topic", msg.Topic).
		Int64("offset", msg.Offset).
		Str("payload", string(msg.Value)).
		Msg("kafka: message received")

	// Cachear en Redis el último mensaje por topic
	cacheKey := fmt.Sprintf("last:%s", msg.Topic)
	s.rdb.Set(ctx, cacheKey, string(msg.Value), 5*time.Minute)

	// Escribir en Postgres
	id, err := pgstore.InsertEvent(ctx, s.pool, pgstore.Event{
		Source:  "kafka",
		Topic:   msg.Topic,
		Payload: msg.Value,
	})
	if err != nil {
		return fmt.Errorf("postgres insert: %w", err)
	}

	s.log.Info().Int64("id", id).Str("topic", msg.Topic).Msg("event saved to postgres")

	// Emitir resultado a NATS para que el gateway lo retransmita via SSE
	result := struct {
		EventID int64  `json:"event_id"`
		Topic   string `json:"topic"`
		Source  string `json:"source"`
	}{
		EventID: id,
		Topic:   msg.Topic,
		Source:  "kafka",
	}

	data, _ := json.Marshal(result)
	if err := natsclient.Publish(s.nc, "switchboard.processed", data); err != nil {
		s.log.Warn().Err(err).Msg("nats publish result failed")
	}

	events.Emit(s.nc, s.log, events.FirehoseEvent{
		Type:    events.TypeKafka,
		Service: "processor",
		Action:  "processed",
		Payload: fmt.Sprintf("id:%d topic:%s", id, msg.Topic),
	})

	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"processor"}`))
}

func (s *Server) Start() error {
	var err error
	s.consumer, err = kafkaclient.NewConsumerGroup(
		s.cfg.Kafka,
		[]string{"switchboard.events"},
		s.processMessage,
		s.log,
	)
	if err != nil {
		return fmt.Errorf("kafka consumer: %w", err)
	}

	// Consumer corre en su propia goroutine
	go func() {
		s.log.Info().Msg("kafka consumer started")
		if err := s.consumer.Start(context.Background()); err != nil {
			s.log.Error().Err(err).Msg("kafka consumer error")
		}
	}()

	s.log.Info().Str("addr", s.http.Addr).Msg("processor listening")
	return s.http.ListenAndServe()
}

func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s.consumer != nil {
		s.consumer.Close()
	}
	if err := s.http.Shutdown(ctx); err != nil {
		s.log.Error().Err(err).Msg("shutdown error")
	}
}
