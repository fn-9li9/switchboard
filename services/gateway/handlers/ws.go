package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	ws "switchboard/internal/transport/ws"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type WSHandler struct {
	hub *ws.Hub
	log zerolog.Logger
}

func NewWSHandler(hub *ws.Hub, log zerolog.Logger) *WSHandler {
	return &WSHandler{hub: hub, log: log}
}

// GET /ws/connect?username=... — upgrade a WebSocket
func (h *WSHandler) Connect(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	if username == "" {
		username = fmt.Sprintf("user-%d", time.Now().UnixMilli()%1000)
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error().Err(err).Msg("ws upgrade error")
		return
	}

	client := ws.NewClient(h.hub, conn, username)
	h.hub.Register(client)

	h.log.Info().Str("username", username).Int("online", h.hub.Count()).Msg("ws: client connected")

	// Notificar a todos que alguien entró
	h.hub.Broadcast(systemMsg(fmt.Sprintf("%s joined · %d online", username, h.hub.Count())))

	go client.WritePump()
	client.ReadPump(func(data []byte) {
		text := strings.TrimSpace(string(data))
		if text == "" {
			return
		}
		h.log.Debug().Str("from", username).Str("msg", text).Msg("ws message")
		h.hub.Broadcast(chatMsg(username, text))
	})

	// Al salir del ReadPump el cliente se desconectó
	h.log.Info().Str("username", username).Int("online", h.hub.Count()).Msg("ws: client disconnected")
	h.hub.Broadcast(systemMsg(fmt.Sprintf("%s left · %d online", username, h.hub.Count())))
}

// GET /ws/count — devuelve el número de clientes online (para HTMX polling)
func (h *WSHandler) Count(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<span class="text-ctp-green font-mono text-xs">%d online</span>`, h.hub.Count())
}

// ── Helpers HTML ──────────────────────────────────────────────────────────

func chatMsg(username, text string) []byte {
	ts := time.Now().Format("15:04:05")
	return []byte(fmt.Sprintf(
		`<div class="chat-msg"><span class="text-ctp-overlay0 font-mono text-xs">%s</span>`+
			`<span class="text-ctp-mauve font-mono text-xs font-bold">%s</span>`+
			`<span class="text-ctp-text text-xs">%s</span></div>`,
		ts, username, text,
	))
}

func systemMsg(text string) []byte {
	ts := time.Now().Format("15:04:05")
	return []byte(fmt.Sprintf(
		`<div class="chat-msg system"><span class="text-ctp-overlay0 font-mono text-xs">%s</span>`+
			`<span class="text-ctp-overlay1 text-xs italic">%s</span></div>`,
		ts, text,
	))
}