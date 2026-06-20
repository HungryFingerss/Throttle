package gemini_test

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jagannivas/throttle/internal/adapters/gemini"
	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/prices"
	"github.com/jagannivas/throttle/internal/tally"
)

const fixture = "../../../testdata/gemini_telemetry.log"

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestParseGeminiTelemetry(t *testing.T) {
	a := gemini.NewWithRoot(`C:\fake\.gemini`)
	res, err := a.Parse(fixture, 0)
	if err != nil {
		t.Fatal(err)
	}
	// 3 api_response records (api_request skipped).
	if len(res.Events) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(res.Events), res.Events)
	}
	// First sess-A response: Input = 1000-200, CacheRead=200, Output=500, Reasoning=100.
	e0 := res.Events[0]
	if e0.SessionID != "sess-A" || e0.Model != "gemini-2.5-pro" {
		t.Fatalf("e0 routing = %q/%q", e0.SessionID, e0.Model)
	}
	want := core.Tokens{Input: 800, CacheRead: 200, Output: 500, Reasoning: 100}
	if e0.Tokens != want {
		t.Fatalf("e0 tokens = %+v, want %+v", e0.Tokens, want)
	}
	if res.Events[2].SessionID != "sess-B" {
		t.Fatalf("e2 session = %q, want sess-B", res.Events[2].SessionID)
	}
}

func TestGeminiMultiSessionDemux(t *testing.T) {
	data, _ := os.ReadFile(fixture)
	root := t.TempDir()
	tel := filepath.Join(root, "telemetry.log")
	os.WriteFile(tel, data, 0o644)

	tr := tally.New(prices.Fallback(), []core.Adapter{gemini.NewWithRoot(root)})
	tr.HandlePath(tel)

	snap := tr.Snapshot()
	// Two real sessions, and NO "gemini-telemetry" placeholder row.
	if len(snap) != 2 {
		t.Fatalf("got %d sessions, want 2 (no file placeholder): %+v", len(snap), ids(snap))
	}
	sort.Slice(snap, func(i, j int) bool { return snap[i].ID < snap[j].ID })

	a, b := snap[0], snap[1]
	if a.ID != "sess-A" || b.ID != "sess-B" {
		t.Fatalf("ids = %v", ids(snap))
	}
	wantA := core.Tokens{Input: 900, CacheRead: 200, Output: 550, Reasoning: 100}
	if a.Tokens != wantA {
		t.Fatalf("sess-A tokens = %+v, want %+v", a.Tokens, wantA)
	}
	if !approx(a.CostUSD, 0.0066875) {
		t.Fatalf("sess-A cost = %.10f, want 0.0066875", a.CostUSD)
	}
	if !approx(b.CostUSD, 0.0031) {
		t.Fatalf("sess-B cost = %.10f, want 0.0031", b.CostUSD)
	}
}

func TestGeminiTruncatedTail(t *testing.T) {
	root := t.TempDir()
	tel := filepath.Join(root, "telemetry.log")
	full, _ := os.ReadFile(fixture)
	// Write everything except the last 40 bytes (mid-object truncation).
	os.WriteFile(tel, full[:len(full)-40], 0o644)

	a := gemini.NewWithRoot(root)
	r1, err := a.Parse(tel, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The last (truncated) record must not be consumed.
	if r1.NewOffset >= int64(len(full)-40) {
		t.Fatalf("offset %d advanced into the truncated record", r1.NewOffset)
	}

	// Complete the file; the next pass picks up the remaining record(s).
	os.WriteFile(tel, full, 0o644)
	r2, err := a.Parse(tel, r1.NewOffset)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Events) == 0 {
		t.Fatal("completing the file should yield the previously-truncated record")
	}
}

func ids(ss []core.Session) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.ID
	}
	return out
}
