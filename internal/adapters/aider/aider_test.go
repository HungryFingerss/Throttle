package aider_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/jagannivas/throttle/internal/adapters/aider"
	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/prices"
	"github.com/jagannivas/throttle/internal/tally"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestParseAiderHistory(t *testing.T) {
	a := aider.NewWithDirs(`C:\proj`)
	res, err := a.Parse("../../../testdata/aider_history.md", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(res.Events), res.Events)
	}
	e0 := res.Events[0]
	if e0.Tokens.Input != 2800 || e0.Tokens.Output != 27 {
		t.Fatalf("e0 tokens = %+v (k-suffix parse?)", e0.Tokens)
	}
	if !e0.HasCostOverride || !approx(e0.CostOverride, 0.0029) {
		t.Fatalf("e0 cost override = %v / %v", e0.HasCostOverride, e0.CostOverride)
	}
	if e0.Model != "gpt-4o" {
		t.Fatalf("e0 model = %q", e0.Model)
	}
	if res.Events[1].Tokens.Input != 3600 || res.Events[1].Tokens.Output != 217 {
		t.Fatalf("e1 tokens = %+v", res.Events[1].Tokens)
	}
}

func TestAiderFullAccounting(t *testing.T) {
	data, _ := os.ReadFile("../../../testdata/aider_history.md")
	dir := t.TempDir()
	hist := filepath.Join(dir, ".aider.chat.history.md")
	os.WriteFile(hist, data, 0o644)

	tr := tally.New(prices.Fallback(), []core.Adapter{aider.NewWithDirs(dir)})
	tr.HandlePath(hist)

	snap := tr.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("got %d sessions, want 1", len(snap))
	}
	s := snap[0]
	if s.Tokens.Input != 6400 || s.Tokens.Output != 244 {
		t.Fatalf("tokens = %+v, want in=6400 out=244", s.Tokens)
	}
	// cost from the file: 0.0029 + 0.0100 = 0.0129 (no pricing guesswork)
	if !approx(s.CostUSD, 0.0129) {
		t.Fatalf("cost = %.6f, want 0.0129", s.CostUSD)
	}
	if s.Estimated {
		t.Fatal("aider cost comes from the file; should not be flagged estimated")
	}
	if s.Model != "gpt-4o" {
		t.Fatalf("model = %q", s.Model)
	}
	if s.Mode != core.ModeAPI {
		t.Fatalf("mode = %q, want api", s.Mode)
	}
	if s.ProjectPath != dir {
		t.Fatalf("project path = %q, want %q", s.ProjectPath, dir)
	}
}

func TestSessionFileID(t *testing.T) {
	a := aider.NewWithDirs(`C:\proj`)
	id, ok := a.SessionFileID(`C:\proj\.aider.chat.history.md`)
	if !ok || id != `C:\proj` {
		t.Fatalf("id=%q ok=%v", id, ok)
	}
	if _, ok := a.SessionFileID(`C:\proj\main.go`); ok {
		t.Fatal("non-history file should be ignored")
	}
}
