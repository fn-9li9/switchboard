package events

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

const Subject = "switchboard.firehose"

type EventType string

const (
	TypePostgres  EventType = "postgres"
	TypeRedis     EventType = "redis"
	TypeNATS      EventType = "nats"
	TypeWebSocket EventType = "websocket"
	TypeKafka     EventType = "kafka"
	TypeSystem    EventType = "system"
)

type FirehoseEvent struct {
	Type    EventType `json:"type"`
	Service string    `json:"service"`
	Action  string    `json:"action"`
	Payload string    `json:"payload"`
	At      time.Time `json:"at"`
}

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
