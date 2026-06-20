package api

import (
	"sync"

	"github.com/gorilla/websocket"
)

// hub fans out broadcast messages to all connected dashboard WebSocket clients.
type hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

type client struct {
	conn *websocket.Conn
	send chan []byte
}

func newHub() *hub { return &hub{clients: map[*client]struct{}{}} }

func (h *hub) add(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) remove(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

// broadcast queues a message to every client, dropping it for any client whose
// buffer is full (a slow client must never block the daemon).
func (h *hub) broadcast(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default: // slow client: drop this frame rather than block
		}
	}
}

func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
