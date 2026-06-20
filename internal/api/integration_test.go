package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jagannivas/throttle/internal/adapters/claude"
	"github.com/jagannivas/throttle/internal/api"
	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/prices"
	"github.com/jagannivas/throttle/internal/tally"
	"github.com/jagannivas/throttle/internal/watch"
)

func assistantLine(req string, in, out int64) string {
	return `{"type":"assistant","cwd":"C:\\proj\\demo","sessionId":"live-1","requestId":"` + req +
		`","message":{"model":"claude-sonnet-4-6","id":"` + req +
		`","usage":{"input_tokens":` + itoa(in) + `,"output_tokens":` + itoa(out) + `}}}` + "\n"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestLiveSpineEndToEnd proves: a new session file is discovered via OS events,
// pushed to a connected dashboard as session_new, and live cost updates on
// append arrive as session_update — no polling.
func TestLiveSpineEndToEnd(t *testing.T) {
	root := t.TempDir()
	encDir := filepath.Join(root, "C--proj-demo")
	if err := os.MkdirAll(encDir, 0o755); err != nil {
		t.Fatal(err) // pre-create so the watcher's initial scan watches this dir
	}

	tracker := tally.New(prices.Fallback(), []core.Adapter{claude.NewWithRoot(root)})
	srv := api.New(tracker, api.AllowAll{}, nil)
	tracker.SetSink(srv.Broadcast)

	w, err := watch.New(tracker.HandlePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx, []string{root}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Connect the dashboard WebSocket.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()

	readUpdate := func() tally.Update {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, b, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var u tally.Update
		if err := json.Unmarshal(b, &u); err != nil {
			t.Fatalf("ws decode: %v", err)
		}
		return u
	}

	// Create a session file: should arrive as session_new.
	file := filepath.Join(encDir, "live-1.jsonl")
	if err := os.WriteFile(file, []byte(assistantLine("r1", 1000, 1000)), 0o644); err != nil {
		t.Fatal(err)
	}

	u := readUpdate()
	if u.Kind != tally.SessionNew {
		t.Fatalf("first update kind = %q, want session_new", u.Kind)
	}
	if u.Session.ProjectPath != `C:\proj\demo` {
		t.Fatalf("project path = %q", u.Session.ProjectPath)
	}
	// 1000*3e-6 + 1000*15e-6 = 0.018
	if d := u.Session.CostUSD - 0.018; d > 1e-9 || d < -1e-9 {
		t.Fatalf("cost = %.10f, want 0.018", u.Session.CostUSD)
	}

	// Append usage: should arrive as session_update with higher cost.
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(assistantLine("r2", 1000, 1000))
	f.Close()

	u2 := readUpdate()
	if u2.Kind != tally.SessionUpdate {
		t.Fatalf("second update kind = %q, want session_update", u2.Kind)
	}
	if d := u2.Session.CostUSD - 0.036; d > 1e-9 || d < -1e-9 {
		t.Fatalf("cost after append = %.10f, want 0.036", u2.Session.CostUSD)
	}

	// REST snapshot agrees.
	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var snap []core.Session
	json.NewDecoder(resp.Body).Decode(&snap)
	if len(snap) != 1 || snap[0].ID != "live-1" {
		t.Fatalf("snapshot = %+v", snap)
	}
}
