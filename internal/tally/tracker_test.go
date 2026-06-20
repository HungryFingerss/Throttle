package tally

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jagannivas/throttle/internal/adapters/claude"
	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/prices"
)

// stageClaudeFixture copies the repo Claude fixture into a temp projects tree
// shaped like <root>/<encoded>/<id>.jsonl so the adapter recognizes it.
func stageClaudeFixture(t *testing.T) (root, file string) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/claude_session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	dir := filepath.Join(root, "C--Users-jagan-throttle-demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(dir, "sess-1.jsonl")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, file
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestTrackerFullAccounting(t *testing.T) {
	root, file := stageClaudeFixture(t)
	tr := New(prices.Fallback(), []core.Adapter{claude.NewWithRoot(root)})

	var updates []Update
	tr.SetSink(func(u Update) { updates = append(updates, u) })

	tr.HandlePath(file)

	snap := tr.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("got %d sessions, want 1", len(snap))
	}
	s := snap[0]

	wantTokens := core.Tokens{Input: 115, Output: 225, CacheRead: 300, CacheCreation: 400}
	if s.Tokens != wantTokens {
		t.Fatalf("tokens = %+v, want %+v (dedup/sidechain folding wrong)", s.Tokens, wantTokens)
	}
	if !approx(s.CostUSD, 0.00663) {
		t.Fatalf("cost = %.10f, want 0.00663", s.CostUSD)
	}
	if s.Model != "claude-opus-4-8" {
		t.Fatalf("model = %q, want claude-opus-4-8 (most recent)", s.Model)
	}
	if s.ProjectPath != `C:\Users\jagan\throttle-demo` {
		t.Fatalf("project path = %q", s.ProjectPath)
	}
	if s.Estimated {
		t.Fatal("all models are known; should not be flagged estimated")
	}

	// first event should be a session_new
	if len(updates) == 0 || updates[0].Kind != SessionNew {
		t.Fatalf("expected a session_new update, got %+v", updates)
	}
}

func TestTrackerIncrementalIdempotent(t *testing.T) {
	root, file := stageClaudeFixture(t)
	tr := New(prices.Fallback(), []core.Adapter{claude.NewWithRoot(root)})

	tr.HandlePath(file)
	first := tr.Snapshot()[0]

	// Re-handle with no new bytes: totals must not change (incremental offset).
	tr.HandlePath(file)
	second := tr.Snapshot()[0]

	if first.Tokens != second.Tokens || !approx(first.CostUSD, second.CostUSD) {
		t.Fatalf("re-handling changed totals: %+v $%.6f -> %+v $%.6f",
			first.Tokens, first.CostUSD, second.Tokens, second.CostUSD)
	}
}

func TestTrackerIdleSweep(t *testing.T) {
	root, file := stageClaudeFixture(t)
	tr := New(prices.Fallback(), []core.Adapter{claude.NewWithRoot(root)})

	clock := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	tr.SetClock(func() time.Time { return clock })
	tr.SetIdleAfter(60 * time.Second)
	tr.HandlePath(file)

	// LastSeen comes from the fixture's last event timestamp (2026-06-20T10:00:03Z),
	// which is well before the clock → should sweep to idle.
	tr.SweepIdle()
	if s, _ := tr.Get("sess-1"); s.Status != core.StatusIdle {
		t.Fatalf("status = %q, want idle", s.Status)
	}
}
