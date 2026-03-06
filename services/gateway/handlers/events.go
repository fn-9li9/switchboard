package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"switchboard/internal/events"
	pgstore "switchboard/internal/store/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

type EventsHandler struct {
	pool *pgxpool.Pool
	log  zerolog.Logger
	nc   *nats.Conn
}

func NewEventsHandler(pool *pgxpool.Pool, log zerolog.Logger, nc *nats.Conn) *EventsHandler {
	return &EventsHandler{pool: pool, log: log, nc: nc}
}

// POST /events — crea un evento desde el form HTMX
func (h *EventsHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	source := r.FormValue("source")
	topic := r.FormValue("topic")
	payload := r.FormValue("payload")

	if source == "" || topic == "" {
		http.Error(w, "source y topic son requeridos", http.StatusBadRequest)
		return
	}

	if payload == "" {
		payload = "{}"
	}

	// Validar que payload sea JSON válido
	if !json.Valid([]byte(payload)) {
		http.Error(w, "payload debe ser JSON válido", http.StatusBadRequest)
		return
	}

	id, err := pgstore.InsertEvent(ctx, h.pool, pgstore.Event{
		Source:  source,
		Topic:   topic,
		Payload: []byte(payload),
	})
	if err != nil {
		h.log.Error().Err(err).Msg("create event")
		http.Error(w, "error guardando evento", http.StatusInternalServerError)
		return
	}

	events.Emit(h.nc, h.log, events.FirehoseEvent{
		Type:    events.TypePostgres,
		Service: "gateway",
		Action:  "insert",
		Payload: fmt.Sprintf("id:%d topic:%s", id, topic),
	})

	h.log.Info().Int64("id", id).Str("topic", topic).Msg("evento creado")

	// HTMX espera el partial del nuevo evento para hx-swap="beforeend"
	event, err := pgstore.GetEvent(ctx, h.pool, id)
	if err != nil {
		http.Error(w, "error leyendo evento", http.StatusInternalServerError)
		return
	}

	renderEventRow(w, event)
}

// GET /events — lista eventos para infinite scroll
func (h *EventsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	events, err := pgstore.ListEvents(ctx, h.pool, limit)
	if err != nil {
		h.log.Error().Err(err).Msg("list events")
		http.Error(w, "error listando eventos", http.StatusInternalServerError)
		return
	}

	for _, e := range events {
		renderEventRow(w, e)
	}
}

// DELETE /events/{id} — elimina un evento
func (h *EventsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "id inválido", http.StatusBadRequest)
		return
	}

	if err := pgstore.DeleteEvent(ctx, h.pool, id); err != nil {
		h.log.Error().Err(err).Int64("id", id).Msg("delete event")
		http.Error(w, "error eliminando evento", http.StatusInternalServerError)
		return
	}

	// HTMX con hx-swap="delete" — respuesta vacía 200
	w.WriteHeader(http.StatusOK)
}

// renderEventRow escribe el HTML de una fila de evento directamente.
func renderEventRow(w http.ResponseWriter, e pgstore.Event) {
	w.Header().Set("Content-Type", "text/html")
	payload := string(e.Payload)
	if len(payload) > 60 {
		payload = payload[:60] + "…"
	}
	fmt.Fprintf(w, `<tr id="event-%d" class="border-b border-ctp-surface0 hover:bg-ctp-surface0 transition-colors">
  <td class="px-4 py-2 text-ctp-overlay1 text-xs">%d</td>
  <td class="px-4 py-2 text-ctp-sapphire text-xs">%s</td>
  <td class="px-4 py-2 text-ctp-mauve text-xs">%s</td>
  <td class="px-4 py-2 text-ctp-subtext0 text-xs font-mono"><code>%s</code></td>
  <td class="px-4 py-2 text-ctp-overlay0 text-xs">%s</td>
  <td class="px-4 py-2">
    <button hx-delete="/events/%d"
            hx-target="#event-%d"
            hx-swap="delete"
            hx-confirm="¿Eliminar evento %d?"
            class="text-ctp-overlay1 hover:text-ctp-red text-xs transition-colors">✕</button>
  </td>
</tr>`,
		e.ID, e.ID, e.Source, e.Topic, payload,
		e.CreatedAt.Format("15:04:05"),
		e.ID, e.ID, e.ID,
	)
}
