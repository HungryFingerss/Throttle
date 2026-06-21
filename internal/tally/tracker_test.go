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

	// Main transcript only: A (deduped) + B; the inline sidechain line is
	// excluded (subagent spend comes from the nested subagents/ files).
	wantTokens := core.Tokens{Input: 110, Output: 220, CacheRead: 300, CacheCreation: 400}
	if s.Tokens != wantTokens {
		t.Fatalf("tokens = %+v, want %+v (dedup ok, inline sidechain excluded)", s.Tokens, wantTokens)
	}
	if !approx(s.CostUSD, 0.00654) { // A (0.00489 sonnet) + B (0.00165 opus)
		t.Fatalf("cost = %.10f, want 0.00654", s.CostUSD)
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

// stageClaudeSubagent drops the subagent fixture at the real nested path
// <root>/<encoded>/<session>/subagents/<agentFile> so the adapter attributes it
// to the parent session.
func stageClaudeSubagent(t *testing.T, root, sessionID, agentFile string) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/claude_subagent.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "C--Users-jagan-throttle-demo", sessionID, "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, agentFile)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTrackerSubagentBreakdown(t *testing.T) {
	root, file := stageClaudeFixture(t)
	subFile := stageClaudeSubagent(t, root, "sess-1", "agent-aSUB1.jsonl")
	tr := New(prices.Fallback(), []core.Adapter{claude.NewWithRoot(root)})

	tr.HandlePath(file)    // main: {110,220,300,400}, $0.00663
	tr.HandlePath(subFile) // subagent: {90,90}, $0.00054 (haiku)

	s, ok := tr.Get("sess-1")
	if !ok {
		t.Fatal("session not found")
	}

	// Subagent spend is FOLDED INTO the parent total (counted exactly once).
	wantTokens := core.Tokens{Input: 200, Output: 310, CacheRead: 300, CacheCreation: 400}
	if s.Tokens != wantTokens {
		t.Fatalf("total tokens = %+v, want %+v (main + subagent)", s.Tokens, wantTokens)
	}
	if !approx(s.CostUSD, 0.00708) { // 0.00654 main + 0.00054 subagent
		t.Fatalf("total cost = %.10f, want 0.00708", s.CostUSD)
	}

	// The parent's displayed model stays the MAIN agent's, not the subagent's haiku.
	if s.Model != "claude-opus-4-8" {
		t.Fatalf("model = %q, want opus (subagent must not change it)", s.Model)
	}

	// And the subagent is itemized for the accordion: one entry, by day.
	if len(s.Subagents) != 1 {
		t.Fatalf("got %d subagent entries, want 1: %+v", len(s.Subagents), s.Subagents)
	}
	sa := s.Subagents[0]
	if sa.ID != "aSUB1" || sa.Day != "2026-06-21" || sa.Model != "claude-haiku-4-5" || sa.Compact {
		t.Fatalf("subagent entry = %+v", sa)
	}
	if sa.Tokens != (core.Tokens{Input: 90, Output: 90}) {
		t.Fatalf("subagent tokens = %+v, want {in:90 out:90}", sa.Tokens)
	}
	if !approx(sa.CostUSD, 0.00054) {
		t.Fatalf("subagent cost = %.10f, want 0.00054", sa.CostUSD)
	}

	// Re-handling with no new bytes must not double-count (incremental offset).
	tr.HandlePath(subFile)
	s2, _ := tr.Get("sess-1")
	if s2.Tokens != wantTokens || !approx(s2.CostUSD, 0.00708) {
		t.Fatalf("re-handle changed totals: %+v $%.6f", s2.Tokens, s2.CostUSD)
	}
}
