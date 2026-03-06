package events

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

const Subject = "switchboard.firehose"

// EventType identifica el origen del evento.
type EventType string

const (
	TypePostgres  EventType = "postgres"
	TypeRedis     EventType = "redis"
	TypeNATS      EventType = "nats"
	TypeWebSocket EventType = "websocket"
	TypeKafka     EventType = "kafka"
	TypeSystem    EventType = "system"
)

// FirehoseEvent es el mensaje que todos los servicios emiten.
type FirehoseEvent struct {
	Type    EventType `json:"type"`
	Service string    `json:"service"`
	Action  string    `json:"action"`
	Payload string    `json:"payload"`
	At      time.Time `json:"at"`
}

// Emit publica un FirehoseEvent a NATS. No bloquea — falla silencioso con log.
func Emit(nc *nats.Conn, log zerolog.Logger, evt FirehoseEvent) {
	evt.At = time.Now()
	data, err := json.Marshal(evt)
	if err != nil {
		log.Warn().Err(err).Msg("firehose: marshal error")
		return
	}
	if err := nc.Publish(Subject, data); err != nil {
		log.Warn().Err(err).Msg("firehose: publish error")
	}
}