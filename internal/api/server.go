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
	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/tally"
)

// Controls is the dashboard→daemon cap-management surface (implemented by the
// enforcer). Optional: when nil, the cap endpoints report empty/unavailable.
type Controls interface {
	SetGlobalCaps(core.Caps)
	SetToolCaps(core.ToolKind, core.Caps)
	SetSessionCaps(string, core.Caps)
	LimitsView() any

	SetGlobalRules([]string)
	SetToolRules(core.ToolKind, []string)
	SetSessionRules(string, []string)
	EnqueueMessage(sessionID, message string)
	RulesView() any
}

// Server wires the tracker, the WS hub, the hook checker, and the web assets.
type Server struct {
	tracker  *tally.Tracker
	checker  Checker
	controls Controls
	hub      *hub
	upgrader websocket.Upgrader
	web      fs.FS
	caps     any
}

// SetControls attaches the cap-management surface (enforcer).
func (s *Server) SetControls(c Controls) { s.controls = c }

// SetCapabilities attaches the per-tool capability map (for honest UI).
func (s *Server) SetCapabilities(v any) { s.caps = v }

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
	mux.HandleFunc("/api/caps", s.handleCaps)
	mux.HandleFunc("/api/rules", s.handleRules)
	mux.HandleFunc("/api/message", s.handleMessage)
	mux.HandleFunc("/api/stop", s.handleStop)
	mux.HandleFunc("/api/capabilities", s.handleCapabilities)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/ws", s.handleWS)
	if s.web != nil {
		mux.Handle("/", http.FileServer(http.FS(s.web)))
	}
	return mux
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.caps == nil {
		writeJSON(w, map[string]any{})
		return
	}
	writeJSON(w, s.caps)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "clients": s.hub.count()})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.tracker.Snapshot())
}

type capRequest struct {
	Scope     string        `json:"scope"` // "global" | "tool" | "session"
	Tool      core.ToolKind `json:"tool"`
	SessionID string        `json:"session_id"`
	Caps      core.Caps     `json:"caps"`
}

func (s *Server) handleCaps(w http.ResponseWriter, r *http.Request) {
	if s.controls == nil {
		writeJSON(w, map[string]any{"error": "caps unavailable"})
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, s.controls.LimitsView())
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	var req capRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch req.Scope {
	case "global":
		s.controls.SetGlobalCaps(req.Caps)
	case "tool":
		s.controls.SetToolCaps(req.Tool, req.Caps)
	case "session":
		s.controls.SetSessionCaps(req.SessionID, req.Caps)
	default:
		http.Error(w, "scope must be global|tool|session", http.StatusBadRequest)
		return
	}
	writeJSON(w, s.controls.LimitsView())
}

type ruleRequest struct {
	Scope     string        `json:"scope"` // "global" | "tool" | "session"
	Tool      core.ToolKind `json:"tool"`
	SessionID string        `json:"session_id"`
	Rules     []string      `json:"rules"`
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	if s.controls == nil {
		writeJSON(w, map[string]any{"error": "rules unavailable"})
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, s.controls.RulesView())
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	var req ruleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch req.Scope {
	case "global":
		s.controls.SetGlobalRules(req.Rules)
	case "tool":
		s.controls.SetToolRules(req.Tool, req.Rules)
	case "session":
		s.controls.SetSessionRules(req.SessionID, req.Rules)
	default:
		http.Error(w, "scope must be global|tool|session", http.StatusBadRequest)
		return
	}
	writeJSON(w, s.controls.RulesView())
}

type messageRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	if s.controls == nil || r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req messageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.controls.EnqueueMessage(req.SessionID, req.Message)
	writeJSON(w, map[string]any{"queued": true})
}

type stopRequest struct {
	SessionID string `json:"session_id"`
	Stop      bool   `json:"stop"`
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req stopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sess, ok := s.tracker.SetStop(req.SessionID, req.Stop)
	if !ok {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	writeJSON(w, sess)
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
