package nats

import (
	"fmt"

	"switchboard/internal/config"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

// NewConn crea y valida una conexión NATS con reconexión automática.
func NewConn(cfg config.NATSConfig, log zerolog.Logger) (*nats.Conn, error) {
	conn, err := nats.Connect(
		cfg.URL,
		nats.Name("switchboard"),
		nats.MaxReconnects(-1), // reconecta indefinidamente
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Warn().Str("url", nc.ConnectedUrl()).Msg("nats: reconectado")
		}),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				log.Error().Err(err).Msg("nats: desconectado")
			}
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			log.Info().Msg("nats: conexión cerrada")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("nats: error conectando a %s: %w", cfg.URL, err)
	}

	log.Info().Str("url", conn.ConnectedUrl()).Msg("nats conectado")
	return conn, nil
}
