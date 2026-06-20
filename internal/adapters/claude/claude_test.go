package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jagannivas/throttle/internal/core"
)

const fixture = "../../../testdata/claude_session.jsonl"

func TestParseClaudeFixture(t *testing.T) {
	a := NewWithRoot(`C:\fake\projects`)
	res, err := a.Parse(fixture, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !res.Meta.Found {
		t.Fatal("meta not found")
	}
	if res.Meta.ProjectPath != `C:\Users\jagan\throttle-demo` {
		t.Fatalf("project path = %q", res.Meta.ProjectPath)
	}
	if res.Meta.ID != "sess-1" {
		t.Fatalf("session id = %q", res.Meta.ID)
	}

	// A, A-dup, S(sidechain), B — malformed and no-usage lines skipped.
	if len(res.Events) != 4 {
		t.Fatalf("got %d events, want 4: %+v", len(res.Events), res.Events)
	}

	// First event = A, sonnet, exact tokens.
	a0 := res.Events[0]
	if a0.Model != "claude-sonnet-4-6" {
		t.Fatalf("event0 model = %q", a0.Model)
	}
	want := core.Tokens{Input: 100, Output: 200, CacheRead: 300, CacheCreation: 400}
	if a0.Tokens != want {
		t.Fatalf("event0 tokens = %+v, want %+v", a0.Tokens, want)
	}

	// A and A-dup share a dedup key.
	if res.Events[0].DedupKey != res.Events[1].DedupKey {
		t.Fatalf("duplicate lines should share a dedup key: %q vs %q",
			res.Events[0].DedupKey, res.Events[1].DedupKey)
	}

	// Sidechain folded in (present as an event) with its model.
	if res.Events[2].Model != "claude-sonnet-4-6" || res.Events[2].Tokens.Input != 5 {
		t.Fatalf("sidechain event wrong: %+v", res.Events[2])
	}

	// Mid-session model switch captured on the last event.
	if res.Events[3].Model != "claude-opus-4-8" {
		t.Fatalf("event3 model = %q, want opus", res.Events[3].Model)
	}

	// Offset advanced to EOF (all lines newline-terminated).
	fi, _ := os.Stat(fixture)
	if res.NewOffset != fi.Size() {
		t.Fatalf("offset = %d, want file size %d", res.NewOffset, fi.Size())
	}
}

func TestIncrementalReads(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")

	line := func(req string) string {
		return `{"type":"assistant","cwd":"C:\\x","sessionId":"s","requestId":"` + req +
			`","message":{"model":"claude-sonnet-4-6","id":"` + req +
			`","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n"
	}

	if err := os.WriteFile(p, []byte(line("r1")+line("r2")), 0o644); err != nil {
		t.Fatal(err)
	}
	a := NewWithRoot(dir)

	r1, err := a.Parse(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Events) != 2 {
		t.Fatalf("first pass: got %d events, want 2", len(r1.Events))
	}

	// Append one more line; re-parse from the stored offset.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(line("r3"))
	f.Close()

	r2, err := a.Parse(p, r1.NewOffset)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Events) != 1 {
		t.Fatalf("incremental pass: got %d events, want 1 (only the new line)", len(r2.Events))
	}
	if r2.Events[0].DedupKey != "r3:r3" {
		t.Fatalf("incremental event key = %q", r2.Events[0].DedupKey)
	}
}

func TestTruncatedTailNotConsumed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")

	good := `{"type":"assistant","cwd":"C:\\x","sessionId":"s","requestId":"r1","message":{"model":"claude-sonnet-4-6","id":"m1","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n"
	partial := `{"type":"assistant","cwd":"C:\\x"` // no newline — file still being written
	if err := os.WriteFile(p, []byte(good+partial), 0o644); err != nil {
		t.Fatal(err)
	}
	a := NewWithRoot(dir)

	r, err := a.Parse(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 1 {
		t.Fatalf("got %d events, want 1 (partial line excluded)", len(r.Events))
	}
	if r.NewOffset != int64(len(good)) {
		t.Fatalf("offset = %d, want %d (just before the partial line)", r.NewOffset, len(good))
	}

	// Complete the partial line; the next pass should now read it.
	rest := `,"sessionId":"s","requestId":"r2","message":{"model":"claude-sonnet-4-6","id":"m2","usage":{"input_tokens":2,"output_tokens":2}}}` + "\n"
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(rest)
	f.Close()

	r2, err := a.Parse(p, r.NewOffset)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Events) != 1 || r2.Events[0].DedupKey != "m2:r2" {
		t.Fatalf("after completion got %+v", r2.Events)
	}
}

func TestSessionFileID(t *testing.T) {
	root := `C:\Users\jagan\.claude\projects`
	a := NewWithRoot(root)

	id, ok := a.SessionFileID(filepath.Join(root, "C--Users-jagan-demo", "abc-123.jsonl"))
	if !ok || id != "abc-123" {
		t.Fatalf("top-level file: id=%q ok=%v", id, ok)
	}

	// Nested subagent transcript → ignored.
	_, ok = a.SessionFileID(filepath.Join(root, "C--Users-jagan-demo", "abc-123", "subagents", "x.jsonl"))
	if ok {
		t.Fatal("nested subagent file should be ignored")
	}

	// Outside the root → ignored.
	_, ok = a.SessionFileID(`C:\somewhere\else\x.jsonl`)
	if ok {
		t.Fatal("file outside root should be ignored")
	}
}
