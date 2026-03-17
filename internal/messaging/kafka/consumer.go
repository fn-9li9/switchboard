package kafka

import (
	"context"
	"fmt"

	"switchboard/internal/config"

	"github.com/IBM/sarama"
	"github.com/rs/zerolog"
)

type Handler func(msg *sarama.ConsumerMessage) error

type ConsumerGroup struct {
	group   sarama.ConsumerGroup
	topics  []string
	handler Handler
	log     zerolog.Logger
}

func NewConsumerGroup(cfg config.KafkaConfig, topics []string, handler Handler, log zerolog.Logger) (*ConsumerGroup, error) {
	sc := sarama.NewConfig()
	sc.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	sc.Consumer.Offsets.Initial = sarama.OffsetNewest

	group, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.GroupID, sc)
	if err != nil {
		return nil, fmt.Errorf("kafka: error creando consumer group %q: %w", cfg.GroupID, err)
	}

	log.Info().Strs("brokers", cfg.Brokers).Str("group_id", cfg.GroupID).Strs("topics", topics).Msg("kafka consumer conectado")

	return &ConsumerGroup{
		group:   group,
		topics:  topics,
		handler: handler,
		log:     log,
	}, nil
}

func (c *ConsumerGroup) Start(ctx context.Context) error {
	h := &cgHandler{handler: c.handler, log: c.log}

	for {
		if err := c.group.Consume(ctx, c.topics, h); err != nil {
			return fmt.Errorf("kafka: consume error: %w", err)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func (c *ConsumerGroup) Close() error {
	return c.group.Close()
}

type cgHandler struct {
	handler Handler
	log     zerolog.Logger
}

func (h *cgHandler) Setup(s sarama.ConsumerGroupSession) error {
	h.log.Debug().Msg("kafka: consumer group setup")
	return nil
}

func (h *cgHandler) Cleanup(s sarama.ConsumerGroupSession) error {
	h.log.Debug().Msg("kafka: consumer group cleanup")
	return nil
}

func (h *cgHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		if err := h.handler(msg); err != nil {
			h.log.Error().Err(err).Str("topic", msg.Topic).Int64("offset", msg.Offset).Msg("kafka: error procesando mensaje")
		} else {
			session.MarkMessage(msg, "")
		}
	}
	return nil
}
