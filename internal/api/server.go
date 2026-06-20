// Package api serves the daemon's two channels: a local HTTP API the hooks call
// (/v1/check) and a WebSocket the dashboard uses for live updates, plus the
// static dashboard assets and a REST snapshot.
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jagannivas/throttle/internal/tally"
)

// Server wires the tracker, the WS hub, the hook checker, and the web assets.
type Server struct {
	tracker  *tally.Tracker
	checker  Checker
	hub      *hub
	upgrader websocket.Upgrader
	web      fs.FS
}

// New builds the API server. webFS may be nil (then the dashboard route 404s).
func New(tr *tally.Tracker, checker Checker, webFS fs.FS) *Server {
	if checker == nil {
		checker = AllowAll{}
	}
	return &Server{
		tracker: tr,
		checker: checker,
		hub:     newHub(),
		web:     webFS,
		upgrader: websocket.Upgrader{
			// The dashboard is served from the same local origin; allow all so
			// localhost dev/tools work.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

// Broadcast is the tracker sink: it serializes an update and fans it to clients.
func (s *Server) Broadcast(u tally.Update) {
	b, err := json.Marshal(u)
	if err != nil {
		return
	}
	s.hub.broadcast(b)
}

// Handler returns the configured HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/check", s.handleCheck)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/ws", s.handleWS)
	if s.web != nil {
		mux.Handle("/", http.FileServer(http.FS(s.web)))
	}
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "clients": s.hub.count()})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.tracker.Snapshot())
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Malformed request: fail-open (allow) — never block the agent on our bug.
		writeJSON(w, CheckResponse{Decision: DecisionAllow, Reason: "bad request (fail-open)"})
		return
	}
	writeJSON(w, s.checker.Check(req))
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan []byte, 64)}
	s.hub.add(c)

	// Send the current snapshot so a freshly connected dashboard is in sync.
	for _, sess := range s.tracker.Snapshot() {
		if b, err := json.Marshal(tally.Update{Kind: tally.SessionNew, Session: sess}); err == nil {
			select {
			case c.send <- b:
			default:
			}
		}
	}

	go s.writePump(c)
	s.readPump(c) // blocks until the client disconnects
}

// writePump drains the client's send channel to the socket and pings to keep
// the connection alive.
func (s *Server) writePump(c *client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump discards client messages (the dashboard control channel is REST in
// M1) and detects disconnect.
func (s *Server) readPump(c *client) {
	defer s.hub.remove(c)
	c.conn.SetReadLimit(1 << 20)
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
