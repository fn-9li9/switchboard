package ws

import "sync"

// Message es el mensaje que circula por el hub.
type Message struct {
	Data   []byte
	Sender *Client
}

// Hub mantiene el registro de clientes activos y broadcast.
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan Message
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Message, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run arranca el loop del hub — debe correrse en una goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- msg.Data:
				default:
					// Canal lleno — cliente lento, lo desconectamos
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Count devuelve el número de clientes conectados.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Broadcast envía un mensaje a todos los clientes.
func (h *Hub) Broadcast(data []byte) {
	h.broadcast <- Message{Data: data}
}

// Register expone el canal register públicamente.
func (h *Hub) Register(c *Client) {
	h.register <- c
}