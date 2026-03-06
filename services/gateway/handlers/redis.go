package handlers

import (
	"context"
	"fmt"
	"net/http"
	"switchboard/internal/events"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type RedisHandler struct {
	rdb *redis.Client
	log zerolog.Logger
	nc  *nats.Conn
}

func NewRedisHandler(rdb *redis.Client, log zerolog.Logger, nc *nats.Conn) *RedisHandler {
	return &RedisHandler{rdb: rdb, log: log, nc: nc}
}

// POST /redis/set — guarda key/value con TTL opcional
func (h *RedisHandler) Set(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	key := r.FormValue("key")
	value := r.FormValue("value")
	ttlS := r.FormValue("ttl") // segundos, opcional

	if key == "" || value == "" {
		http.Error(w, "key and value are required", http.StatusBadRequest)
		return
	}

	var ttl time.Duration
	if ttlS != "" {
		if d, err := time.ParseDuration(ttlS + "s"); err == nil {
			ttl = d
		}
	}

	if err := h.rdb.Set(ctx, key, value, ttl).Err(); err != nil {
		h.log.Error().Err(err).Msg("redis set")
		http.Error(w, "error setting key", http.StatusInternalServerError)
		return
	}

	events.Emit(h.nc, h.log, events.FirehoseEvent{
		Type:    events.TypeRedis,
		Service: "gateway",
		Action:  "set",
		Payload: fmt.Sprintf("%s = %s", key, value),
	})

	h.log.Info().Str("key", key).Msg("redis set")
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="result-row">
  <span class="text-ctp-green font-mono text-xs">SET</span>
  <span class="text-ctp-mauve font-mono text-xs">%s</span>
  <span class="text-ctp-text font-mono text-xs">%s</span>
  <span class="text-ctp-overlay0 font-mono text-xs">%s</span>
</div>`, key, value, time.Now().Format("15:04:05"))
}

// GET /redis/get?key=... — lee un valor
func (h *RedisHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}

	val, err := h.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="result-row">
  <span class="text-ctp-yellow font-mono text-xs">GET</span>
  <span class="text-ctp-mauve font-mono text-xs">%s</span>
  <span class="text-ctp-overlay0 font-mono text-xs italic">nil</span>
  <span class="text-ctp-overlay0 font-mono text-xs">%s</span>
</div>`, key, time.Now().Format("15:04:05"))
		return
	}
	if err != nil {
		h.log.Error().Err(err).Msg("redis get")
		http.Error(w, "error getting key", http.StatusInternalServerError)
		return
	}

	ttl, _ := h.rdb.TTL(ctx, key).Result()
	ttlStr := "∞"
	if ttl > 0 {
		ttlStr = ttl.Round(time.Second).String()
	}

	h.log.Info().Str("key", key).Msg("redis get")
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="result-row">
  <span class="text-ctp-sapphire font-mono text-xs">GET</span>
  <span class="text-ctp-mauve font-mono text-xs">%s</span>
  <span class="text-ctp-text font-mono text-xs">%s</span>
  <span class="text-ctp-overlay0 font-mono text-xs">ttl: %s · %s</span>
</div>`, key, val, ttlStr, time.Now().Format("15:04:05"))
}

// DELETE /redis/del — elimina una key
func (h *RedisHandler) Del(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	key := r.FormValue("key")
	if key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}

	n, err := h.rdb.Del(ctx, key).Result()
	if err != nil {
		h.log.Error().Err(err).Msg("redis del")
		http.Error(w, "error deleting key", http.StatusInternalServerError)
		return
	}

	label := "DEL"
	color := "text-ctp-red"
	note := "deleted"
	if n == 0 {
		color = "text-ctp-overlay0"
		note = "not found"
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="result-row">
  <span class="%s font-mono text-xs">%s</span>
  <span class="text-ctp-mauve font-mono text-xs">%s</span>
  <span class="text-ctp-overlay0 font-mono text-xs italic">%s</span>
  <span class="text-ctp-overlay0 font-mono text-xs">%s</span>
</div>`, color, label, key, note, time.Now().Format("15:04:05"))
}

// POST /redis/publish — publica en un canal
func (h *RedisHandler) Publish(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	channel := r.FormValue("channel")
	message := r.FormValue("message")

	if channel == "" || message == "" {
		http.Error(w, "channel and message are required", http.StatusBadRequest)
		return
	}

	if err := h.rdb.Publish(ctx, channel, message).Err(); err != nil {
		h.log.Error().Err(err).Msg("redis publish")
		http.Error(w, "error publishing", http.StatusInternalServerError)
		return
	}

	events.Emit(h.nc, h.log, events.FirehoseEvent{
		Type:    events.TypeRedis,
		Service: "gateway",
		Action:  "publish",
		Payload: fmt.Sprintf("channel:%s msg:%s", channel, message),
	})
	h.log.Info().Str("channel", channel).Str("message", message).Msg("redis publish")
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="pubsub-row">
  <span class="text-ctp-pink font-mono text-xs">PUB</span>
  <span class="text-ctp-peach font-mono text-xs">%s</span>
  <span class="text-ctp-text font-mono text-xs">%s</span>
  <span class="text-ctp-overlay0 font-mono text-xs">%s</span>
</div>`, channel, message, time.Now().Format("15:04:05"))
}

// GET /redis/subscribe?channel=... — SSE stream de mensajes pub/sub
func (h *RedisHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "switchboard"
	}

	// Headers SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Ping inicial para que HTMX sepa que la conexión está viva
	fmt.Fprintf(w, "event: connected\ndata: subscribed to %s\n\n", channel)
	flusher.Flush()

	h.log.Info().Str("channel", channel).Msg("sse: client subscribed")

	pubsub := h.rdb.Subscribe(r.Context(), channel)
	defer pubsub.Close()

	ch := pubsub.Channel()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			h.log.Debug().Str("channel", msg.Channel).Str("payload", msg.Payload).Msg("sse: message")
			fmt.Fprintf(w, "event: message\ndata: <div class=\"pubsub-row\"><span class=\"text-ctp-teal font-mono text-xs\">SUB</span><span class=\"text-ctp-peach font-mono text-xs\">%s</span><span class=\"text-ctp-text font-mono text-xs\">%s</span><span class=\"text-ctp-overlay0 font-mono text-xs\">%s</span></div>\n\n",
				msg.Channel, msg.Payload, time.Now().Format("15:04:05"))
			flusher.Flush()

		case <-r.Context().Done():
			h.log.Info().Str("channel", channel).Msg("sse: client disconnected")
			return
		}
	}
}
