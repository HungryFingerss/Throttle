package codex_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/jagannivas/throttle/internal/adapters/codex"
	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/prices"
	"github.com/jagannivas/throttle/internal/tally"
)

func stage(t *testing.T, root, name, src string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "2026", "06", "20")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCodexFullAccounting(t *testing.T) {
	root := t.TempDir()
	file := stage(t, root, "rollout-2026-06-20T10-00-00-00000000-0000-0000-0000-000000000001.jsonl",
		"../../../testdata/codex_session.jsonl")

	tr := tally.New(prices.Fallback(), []core.Adapter{codex.NewWithPaths(root, "")})
	tr.HandlePath(file)

	snap := tr.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("got %d sessions, want 1", len(snap))
	}
	s := snap[0]

	// After dedup: Input 500, Output 510, CacheRead 650, Reasoning 50.
	want := core.Tokens{Input: 500, Output: 510, CacheRead: 650, Reasoning: 50}
	if s.Tokens != want {
		t.Fatalf("tokens = %+v, want %+v", s.Tokens, want)
	}
	// gpt-5.5: 400*1.25e-6 + 200*1e-5 + 600*0.125e-6 = 0.002575
	// gpt-5-mini B: 100*0.25e-6 + 300*2e-6 = 0.000625
	// gpt-5-mini C: 10*2e-6 + 50*0.025e-6 = 0.00002125
	if !approx(s.CostUSD, 0.00322125) {
		t.Fatalf("cost = %.10f, want 0.00322125", s.CostUSD)
	}
	if s.Model != "gpt-5-mini" {
		t.Fatalf("model = %q, want gpt-5-mini (most recent)", s.Model)
	}
	if s.ProjectPath != `C:\proj\codex` {
		t.Fatalf("project path = %q", s.ProjectPath)
	}
	if s.Estimated {
		t.Fatal("known models should not be estimated")
	}
}

// The 91× trap: a subagent rollout replays huge parent history. It must NOT
// appear as a row and must NOT add a single token to the machine-wide totals.
func TestSubagentExcluded(t *testing.T) {
	root := t.TempDir()
	main := stage(t, root, "rollout-2026-06-20T10-00-00-00000000-0000-0000-0000-000000000001.jsonl",
		"../../../testdata/codex_session.jsonl")
	sub := stage(t, root, "rollout-2026-06-20T11-00-00-00000000-0000-0000-0000-000000000002.jsonl",
		"../../../testdata/codex_subagent.jsonl")

	tr := tally.New(prices.Fallback(), []core.Adapter{codex.NewWithPaths(root, "")})
	tr.HandlePath(main)
	tr.HandlePath(sub)

	snap := tr.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("got %d rows, want 1 (subagent must be excluded)", len(snap))
	}
	// The subagent's ~2M tokens must not have leaked into the total.
	if snap[0].Tokens.Total() != 1660 {
		t.Fatalf("total tokens = %d, want 1660 (subagent leaked in!)", snap[0].Tokens.Total())
	}
}
