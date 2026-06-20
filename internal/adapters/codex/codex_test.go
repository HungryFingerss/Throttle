package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jagannivas/throttle/internal/core"
)

const (
	fixture  = "../../../testdata/codex_session.jsonl"
	subagent = "../../../testdata/codex_subagent.jsonl"
)

func TestParseCodexFixture(t *testing.T) {
	a := NewWithPaths(`C:\fake\sessions`, "")
	res, err := a.Parse(fixture, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !res.Meta.Found || res.Meta.ProjectPath != `C:\proj\codex` {
		t.Fatalf("meta = %+v", res.Meta)
	}
	if res.Meta.IsSubagent {
		t.Fatal("normal session wrongly flagged subagent")
	}

	// A, A-dup, B, C — info:null and malformed lines skipped.
	if len(res.Events) != 4 {
		t.Fatalf("got %d events, want 4: %+v", len(res.Events), res.Events)
	}

	// A: gpt-5.5, input includes cache → Input=400, CacheRead=600, Output=200, Reasoning=50.
	a0 := res.Events[0]
	if a0.Model != "gpt-5.5" {
		t.Fatalf("A model = %q", a0.Model)
	}
	wantA := core.Tokens{Input: 400, CacheRead: 600, Output: 200, Reasoning: 50}
	if a0.Tokens != wantA {
		t.Fatalf("A tokens = %+v, want %+v", a0.Tokens, wantA)
	}

	// A and its duplicate share a dedup key.
	if res.Events[0].DedupKey != res.Events[1].DedupKey {
		t.Fatal("duplicate token_count should share a dedup key")
	}

	// B: gpt-5-mini after the model switch.
	if res.Events[2].Model != "gpt-5-mini" || res.Events[2].Tokens.Output != 300 {
		t.Fatalf("B event wrong: %+v", res.Events[2])
	}

	// C: compaction-tagged, Input = 50-50 = 0, CacheRead 50.
	c := res.Events[3]
	if !c.IsCompaction {
		t.Fatal("C should be tagged as a compaction spike")
	}
	if (c.Tokens != core.Tokens{Input: 0, CacheRead: 50, Output: 10}) {
		t.Fatalf("C tokens = %+v", c.Tokens)
	}

	fi, _ := os.Stat(fixture)
	if res.NewOffset != fi.Size() {
		t.Fatalf("offset = %d, want file size %d (last line malformed but newline-terminated)", res.NewOffset, fi.Size())
	}
}

func TestParseSubagentMeta(t *testing.T) {
	a := NewWithPaths(`C:\fake\sessions`, "")
	res, err := a.Parse(subagent, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Meta.IsSubagent {
		t.Fatal("subagent session not detected")
	}
	if res.Meta.ParentID != "019e-codex-id" {
		t.Fatalf("parent id = %q", res.Meta.ParentID)
	}
}

func TestSessionFileID(t *testing.T) {
	root := `C:\Users\jagan\.codex\sessions`
	a := NewWithPaths(root, "")
	p := filepath.Join(root, "2026", "06", "20", "rollout-2026-06-20T10-00-00-019e20ed-3930-7643-9a72-c5665bfc447d.jsonl")
	id, ok := a.SessionFileID(p)
	if !ok || id != "019e20ed-3930-7643-9a72-c5665bfc447d" {
		t.Fatalf("id=%q ok=%v", id, ok)
	}

	// Non-rollout file ignored.
	if _, ok := a.SessionFileID(filepath.Join(root, "2026", "history.jsonl")); ok {
		t.Fatal("non-rollout file should be ignored")
	}
}

// TestRealCodexLogSanity parses an actual rollout from this machine when
// THROTTLE_REAL_CODEX_LOG points to one. It never commits or prints private
// content — it only asserts the parser doesn't crash and produces sane totals.
// Skipped by default so CI / other machines stay green.
func TestRealCodexLogSanity(t *testing.T) {
	p := os.Getenv("THROTTLE_REAL_CODEX_LOG")
	if p == "" {
		t.Skip("set THROTTLE_REAL_CODEX_LOG to a real rollout to run this")
	}
	a := NewWithPaths(`C:\fake`, "")
	res, err := a.Parse(p, 0)
	if err != nil {
		t.Fatalf("parse real log: %v", err)
	}
	if !res.Meta.Found {
		t.Fatal("no session_meta found in real log")
	}
	var total int64
	for _, e := range res.Events {
		if e.Tokens.Input < 0 || e.Tokens.Output < 0 || e.Tokens.CacheRead < 0 {
			t.Fatalf("negative tokens in event: %+v", e.Tokens)
		}
		total += e.Tokens.Total()
	}
	t.Logf("real log: %d usage events, %d total tokens, subagent=%v",
		len(res.Events), total, res.Meta.IsSubagent)
}

func TestDetectMode(t *testing.T) {
	dir := t.TempDir()

	sub := filepath.Join(dir, "chatgpt.json")
	os.WriteFile(sub, []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"x"}}`), 0o644)
	if m := NewWithPaths(dir, sub).DetectMode(); m != core.ModeSubscription {
		t.Fatalf("chatgpt mode = %q, want subscription", m)
	}

	api := filepath.Join(dir, "api.json")
	os.WriteFile(api, []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"sk-test"}`), 0o644)
	if m := NewWithPaths(dir, api).DetectMode(); m != core.ModeAPI {
		t.Fatalf("apikey mode = %q, want api", m)
	}

	if m := NewWithPaths(dir, filepath.Join(dir, "missing.json")).DetectMode(); m != core.ModeUnknown {
		t.Fatalf("missing auth mode = %q, want unknown", m)
	}
}
