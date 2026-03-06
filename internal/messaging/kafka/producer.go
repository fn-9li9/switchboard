package kafka

import (
	"fmt"

	"switchboard/internal/config"

	"github.com/IBM/sarama"
	"github.com/rs/zerolog"
)

// NewProducer crea un SyncProducer con acks=all y reintentos.
func NewProducer(cfg config.KafkaConfig, log zerolog.Logger) (sarama.SyncProducer, error) {
	sc := sarama.NewConfig()
	sc.Producer.Return.Successes = true
	sc.Producer.Return.Errors = true
	sc.Producer.RequiredAcks = sarama.WaitForAll
	sc.Producer.Retry.Max = 3

	producer, err := sarama.NewSyncProducer(cfg.Brokers, sc)
	if err != nil {
		return nil, fmt.Errorf("kafka: error creando producer: %w", err)
	}

	log.Info().Strs("brokers", cfg.Brokers).Msg("kafka producer conectado")
	return producer, nil
}

// Publish envía un mensaje a un topic. Devuelve partición y offset.
func Publish(producer sarama.SyncProducer, topic string, key, value []byte) (int32, int64, error) {
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(value),
	}
	if key != nil {
		msg.Key = sarama.ByteEncoder(key)
	}

	partition, offset, err := producer.SendMessage(msg)
	if err != nil {
		return 0, 0, fmt.Errorf("kafka: Publish %q: %w", topic, err)
	}
	return partition, offset, nil
}
