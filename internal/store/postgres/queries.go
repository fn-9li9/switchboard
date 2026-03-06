package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event refleja la tabla events de la migración 000001.
type Event struct {
	ID        int64     `db:"id"`
	Source    string    `db:"source"`
	Topic     string    `db:"topic"`
	Payload   []byte    `db:"payload"`
	CreatedAt time.Time `db:"created_at"`
}

// InsertEvent inserta un evento y devuelve el ID generado.
func InsertEvent(ctx context.Context, pool *pgxpool.Pool, e Event) (int64, error) {
	const q = `
		INSERT INTO events (source, topic, payload)
		VALUES (@source, @topic, @payload)
		RETURNING id`

	args := pgx.NamedArgs{
		"source":  e.Source,
		"topic":   e.Topic,
		"payload": e.Payload,
	}

	var id int64
	if err := pool.QueryRow(ctx, q, args).Scan(&id); err != nil {
		return 0, fmt.Errorf("postgres: InsertEvent: %w", err)
	}
	return id, nil
}

// ListEvents devuelve los últimos n eventos ordenados por id desc.
func ListEvents(ctx context.Context, pool *pgxpool.Pool, limit int) ([]Event, error) {
	const q = `
		SELECT id, source, topic, payload, created_at
		FROM events
		ORDER BY id DESC
		LIMIT @limit`

	rows, err := pool.Query(ctx, q, pgx.NamedArgs{"limit": limit})
	if err != nil {
		return nil, fmt.Errorf("postgres: ListEvents: %w", err)
	}
	defer rows.Close()

	events, err := pgx.CollectRows(rows, pgx.RowToStructByName[Event])
	if err != nil {
		return nil, fmt.Errorf("postgres: ListEvents scan: %w", err)
	}
	return events, nil
}

// GetEvent devuelve un evento por ID.
func GetEvent(ctx context.Context, pool *pgxpool.Pool, id int64) (Event, error) {
	const q = `
		SELECT id, source, topic, payload, created_at
		FROM events
		WHERE id = @id`

	rows, err := pool.Query(ctx, q, pgx.NamedArgs{"id": id})
	if err != nil {
		return Event{}, fmt.Errorf("postgres: GetEvent: %w", err)
	}
	defer rows.Close()

	event, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Event])
	if err != nil {
		return Event{}, fmt.Errorf("postgres: GetEvent scan: %w", err)
	}
	return event, nil
}

// DeleteEvent elimina un evento por ID. Devuelve error si no existía.
func DeleteEvent(ctx context.Context, pool *pgxpool.Pool, id int64) error {
	const q = `DELETE FROM events WHERE id = @id`

	tag, err := pool.Exec(ctx, q, pgx.NamedArgs{"id": id})
	if err != nil {
		return fmt.Errorf("postgres: DeleteEvent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: DeleteEvent: id %d no encontrado", id)
	}
	return nil
}
