package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"switchboard/internal/events"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

type FirehoseHandler struct {
	nc  *nats.Conn
	log zerolog.Logger
}

func NewFirehoseHandler(nc *nats.Conn, log zerolog.Logger) *FirehoseHandler {
	return &FirehoseHandler{nc: nc, log: log}
}

// GET /firehose/stream — SSE con todos los eventos de todos los servicios
func (h *FirehoseHandler) Stream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "event: connected\ndata: firehose connected\n\n")
	flusher.Flush()

	h.log.Info().Msg("firehose: client connected")

	msgCh := make(chan *nats.Msg, 256)
	sub, err := h.nc.ChanSubscribe(events.Subject, msgCh)
	if err != nil {
		h.log.Error().Err(err).Msg("firehose: subscribe error")
		return
	}
	defer sub.Unsubscribe()

	for {
		select {
		case msg := <-msgCh:
			var evt events.FirehoseEvent
			if err := json.Unmarshal(msg.Data, &evt); err != nil {
				continue
			}
			fmt.Fprintf(w, "event: event\ndata: %s\n\n", renderFirehoseRow(evt))
			flusher.Flush()

		case <-r.Context().Done():
			h.log.Info().Msg("firehose: client disconnected")
			return
		}
	}
}

// renderFirehoseRow genera el HTML de una fila del firehose.
func renderFirehoseRow(evt events.FirehoseEvent) string {
	typeColor := typeToColor(evt.Type)
	serviceColor := serviceToColor(evt.Service)

	payload := evt.Payload
	if len(payload) > 80 {
		payload = payload[:80] + "…"
	}

	return fmt.Sprintf(
		`<div class="fh-row">`+
			`<span class="text-ctp-overlay0 font-mono text-xs w-16 shrink-0">%s</span>`+
			`<span class="%s font-mono text-xs w-20 shrink-0 font-bold">%s</span>`+
			`<span class="%s font-mono text-xs w-20 shrink-0">%s</span>`+
			`<span class="text-ctp-subtext0 font-mono text-xs w-24 shrink-0">%s</span>`+
			`<span class="text-ctp-text text-xs flex-1 truncate">%s</span>`+
			`</div>`,
		evt.At.Format("15:04:05"),
		typeColor, evt.Type,
		serviceColor, evt.Service,
		evt.Action,
		payload,
	)
}

func typeToColor(t events.EventType) string {
	switch t {
	case events.TypePostgres:
		return "text-ctp-green"
	case events.TypeRedis:
		return "text-ctp-red"
	case events.TypeNATS:
		return "text-ctp-sapphire"
	case events.TypeWebSocket:
		return "text-ctp-yellow"
	case events.TypeKafka:
		return "text-ctp-peach"
	case events.TypeSystem:
		return "text-ctp-mauve"
	default:
		return "text-ctp-overlay1"
	}
}

func serviceToColor(s string) string {
	switch s {
	case "gateway":
		return "text-ctp-mauve"
	case "notifier":
		return "text-ctp-sapphire"
	case "processor":
		return "text-ctp-peach"
	default:
		return "text-ctp-overlay1"
	}
}

// formatDuration no se usa externamente pero la dejamos por si acaso
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}