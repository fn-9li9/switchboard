package nats

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Publish emite un mensaje fire-and-forget a un subject.
func Publish(conn *nats.Conn, subject string, data []byte) error {
	if err := conn.Publish(subject, data); err != nil {
		return fmt.Errorf("nats: Publish %q: %w", subject, err)
	}
	return nil
}

// Subscribe suscribe a un subject y devuelve la suscripción.
// El caller es responsable de llamar sub.Unsubscribe() al terminar.
func Subscribe(conn *nats.Conn, subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	sub, err := conn.Subscribe(subject, handler)
	if err != nil {
		return nil, fmt.Errorf("nats: Subscribe %q: %w", subject, err)
	}
	return sub, nil
}

// Request hace un request/reply con timeout extraído del context.
// Si el context no tiene deadline usa defaultTimeout.
func Request(ctx context.Context, conn *nats.Conn, subject string, data []byte, defaultTimeout time.Duration) (*nats.Msg, error) {
	timeout := defaultTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}

	msg, err := conn.Request(subject, data, timeout)
	if err != nil {
		return nil, fmt.Errorf("nats: Request %q: %w", subject, err)
	}
	return msg, nil
}

// QueueSubscribe suscribe a un subject con queue group para load balancing.
func QueueSubscribe(conn *nats.Conn, subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error) {
	sub, err := conn.QueueSubscribe(subject, queue, handler)
	if err != nil {
		return nil, fmt.Errorf("nats: QueueSubscribe %q [%s]: %w", subject, queue, err)
	}
	return sub, nil
}
