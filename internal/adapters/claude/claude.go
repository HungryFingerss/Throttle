// Package claude is the Claude Code log adapter. It reads
// ~/.claude/projects/<encoded-cwd>/<session>.jsonl incrementally and emits
// normalized usage events. See THROTTLE-RESEARCH.md §2 and the verified schema
// in PROGRESS.md.
//
// Claude specifics handled here:
//   - input_tokens already excludes cache read/creation → mapped directly.
//   - per-message model (message.model) → exact per-event pricing.
//   - isSidechain lines are subagent turns; they are folded into the parent
//     session (counted, not a separate row) — Claude has no Codex-style
//     full-history replay, so folding is correct, not an overcount.
//   - dedup key = message.id + requestId (deduped by the tally layer).
//   - nested .../subagents/... transcripts are ignored (no separate rows).
package claude

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jagannivas/throttle/internal/config"
	"github.com/jagannivas/throttle/internal/core"
)

// Adapter implements core.Adapter for Claude Code.
type Adapter struct {
	root string // ClaudeProjectsRoot, captured once
}

// New returns a Claude adapter watching the resolved projects root.
func New() *Adapter { return &Adapter{root: config.ClaudeProjectsRoot()} }

// NewWithRoot returns an adapter rooted at a specific dir (used by tests/sandbox).
func NewWithRoot(root string) *Adapter { return &Adapter{root: root} }

func (a *Adapter) Tool() core.ToolKind { return core.ToolClaude }

func (a *Adapter) Roots() []string { return []string{a.root} }

// Capabilities: Claude is the strongest tool — exact monitoring, blocking hook
// caps, live injection, and rules guaranteed to survive compaction.
func (a *Adapter) Capabilities() core.Capabilities {
	return core.Capabilities{
		Monitor:                true,
		HardCap:                true,
		LiveInject:             true,
		RulesSurviveCompaction: true,
		StopMechanism:          "hook",
		MonitorConfidence:      "exact",
	}
}

// SessionFileID accepts only top-level session files: <root>/<encoded>/<uuid>.jsonl.
// Anything deeper (e.g. .../<session>/subagents/...) is ignored.
func (a *Adapter) SessionFileID(path string) (string, bool) {
	rel, err := filepath.Rel(a.root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	if !strings.EqualFold(filepath.Ext(rel), ".jsonl") {
		return "", false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 {
		return "", false // not a direct <encoded>/<file>.jsonl
	}
	return strings.TrimSuffix(parts[1], ".jsonl"), true
}

// ---- line schema (only the fields we use) ----

type line struct {
	Type        string   `json:"type"`
	IsSidechain bool     `json:"isSidechain"`
	Cwd         string   `json:"cwd"`
	SessionID   string   `json:"sessionId"`
	RequestID   string   `json:"requestId"`
	UUID        string   `json:"uuid"`
	Timestamp   string   `json:"timestamp"`
	Message     *message `json:"message"`
}

type message struct {
	Model string `json:"model"`
	ID    string `json:"id"`
	Usage *usage `json:"usage"`
}

type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// Parse reads new complete lines from fromOffset and emits usage events.
// A truncated trailing line (no newline yet) is left unread: NewOffset points
// just before it so the next pass re-reads it once complete.
func (a *Adapter) Parse(path string, fromOffset int64) (core.ParseResult, error) {
	res := core.ParseResult{NewOffset: fromOffset}

	f, err := os.Open(path)
	if err != nil {
		return res, err
	}
	defer f.Close()

	if fromOffset > 0 {
		if _, err := f.Seek(fromOffset, io.SeekStart); err != nil {
			return res, err
		}
	}

	r := bufio.NewReaderSize(f, 1<<20)
	offset := fromOffset

	for {
		chunk, rerr := r.ReadBytes('\n')
		if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			a.handleLine(chunk, &res)
			offset += int64(len(chunk))
		}
		// A non-newline-terminated tail is a partial line: stop, don't advance.
		if rerr != nil {
			break
		}
	}

	res.NewOffset = offset
	return res, nil
}

func (a *Adapter) handleLine(raw []byte, res *core.ParseResult) {
	var ln line
	if err := json.Unmarshal(raw, &ln); err != nil {
		return // tolerate unparseable / old-format lines
	}

	// Capture session metadata the first time we see a real cwd.
	if !res.Meta.Found && ln.Cwd != "" {
		res.Meta = core.SessionMeta{
			ID:          ln.SessionID,
			Tool:        core.ToolClaude,
			ProjectPath: ln.Cwd,
			Found:       true,
		}
		if ts, ok := parseTime(ln.Timestamp); ok {
			res.Meta.StartedAt = ts
		}
	}

	if ln.Type != "assistant" || ln.Message == nil || ln.Message.Usage == nil {
		return
	}
	u := ln.Message.Usage
	ev := core.UsageEvent{
		Model: ln.Message.Model,
		Tokens: core.Tokens{
			Input:         u.InputTokens,
			Output:        u.OutputTokens,
			CacheRead:     u.CacheReadInputTokens,
			CacheCreation: u.CacheCreationInputTokens,
		},
		DedupKey: dedupKey(ln),
	}
	if ev.Tokens.IsZero() {
		return // nothing billable
	}
	if ts, ok := parseTime(ln.Timestamp); ok {
		ev.Timestamp = ts
		if ts.After(res.LastEvent) {
			res.LastEvent = ts
		}
	}
	res.Events = append(res.Events, ev)
}

// dedupKey is message.id+requestId (ccusage approach); falls back to the line
// uuid so a line is never silently dropped when ids are missing.
func dedupKey(ln line) string {
	if ln.Message != nil && (ln.Message.ID != "" || ln.RequestID != "") {
		return ln.Message.ID + ":" + ln.RequestID
	}
	return "uuid:" + ln.UUID
}

func parseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// DetectMode reads Claude auth state. ANTHROPIC_API_KEY ⇒ API billing; an
// OAuth credentials file (Max/Pro login) ⇒ subscription; else unknown.
// [verify] exact Claude credential layout per platform.
func (a *Adapter) DetectMode() core.Mode {
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return core.ModeAPI
	}
	for _, name := range []string{".credentials.json", "credentials.json"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(a.root), name)); err == nil {
			return core.ModeSubscription
		}
	}
	return core.ModeUnknown
}
