package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

type NATSHandler struct {
	nc  *nats.Conn
	log zerolog.Logger
}

func NewNATSHandler(nc *nats.Conn, log zerolog.Logger) *NATSHandler {
	return &NATSHandler{nc: nc, log: log}
}

// POST /nats/request — request/reply hacia notifier
func (h *NATSHandler) Request(w http.ResponseWriter, r *http.Request) {
	message := r.FormValue("message")
	subject := r.FormValue("subject")
	if subject == "" {
		subject = "switchboard.request"
	}
	if message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	start := time.Now()

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	msg, err := h.nc.RequestWithContext(ctx, subject, []byte(message))
	latency := time.Since(start)

	if err != nil {
		h.log.Error().Err(err).Str("subject", subject).Msg("nats request")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="nats-row error">
  <span class="text-ctp-red font-mono text-xs">ERR</span>
  <span class="text-ctp-overlay0 font-mono text-xs">%s</span>
  <span class="text-ctp-red font-mono text-xs">%s</span>
</div>`, subject, err.Error())
		return
	}

	// Intentar parsear JSON del reply
	var reply map[string]string
	replyText := string(msg.Data)
	if err := json.Unmarshal(msg.Data, &reply); err == nil {
		if v, ok := reply["message"]; ok {
			replyText = v
		}
	}

	h.log.Info().
		Str("subject", subject).
		Dur("latency", latency).
		Msg("nats request/reply")

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="nats-row">
  <span class="text-ctp-sapphire font-mono text-xs font-bold">REQ</span>
  <span class="text-ctp-mauve font-mono text-xs">%s</span>
  <span class="text-ctp-text font-mono text-xs flex-1">%s</span>
  <span class="text-ctp-green font-mono text-xs">↩ %s</span>
  <span class="text-ctp-yellow font-mono text-xs font-bold">%s</span>
  <span class="text-ctp-overlay0 font-mono text-xs">%s</span>
</div>`,
		subject, message, replyText,
		formatLatency(latency),
		time.Now().Format("15:04:05"),
	)
}

// GET /nats/subscribe?subject=... — SSE stream de mensajes NATS
func (h *NATSHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	subject := r.URL.Query().Get("subject")
	if subject == "" {
		subject = "switchboard.>"
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "event: connected\ndata: subscribed to %s\n\n", subject)
	flusher.Flush()

	h.log.Info().Str("subject", subject).Msg("nats sse: client subscribed")

	msgCh := make(chan *nats.Msg, 64)
	sub, err := h.nc.ChanSubscribe(subject, msgCh)
	if err != nil {
		h.log.Error().Err(err).Msg("nats subscribe error")
		return
	}
	defer sub.Unsubscribe()

	for {
		select {
		case msg := <-msgCh:
			h.log.Debug().Str("subject", msg.Subject).Msg("nats sse: message")
			fmt.Fprintf(w,
				"event: message\ndata: <div class=\"nats-row\"><span class=\"text-ctp-teal font-mono text-xs font-bold\">SUB</span><span class=\"text-ctp-mauve font-mono text-xs\">%s</span><span class=\"text-ctp-text font-mono text-xs\">%s</span><span class=\"text-ctp-overlay0 font-mono text-xs\">%s</span></div>\n\n",
				msg.Subject, string(msg.Data), time.Now().Format("15:04:05"),
			)
			flusher.Flush()

		case <-r.Context().Done():
			h.log.Info().Str("subject", subject).Msg("nats sse: client disconnected")
			return
		}
	}
}

func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}
