// Package core holds the shared domain types for Throttle: tokens, sessions,
// usage events, and the Adapter contract every per-tool log parser implements.
//
// Token normalization (CRITICAL — every adapter must normalize to this):
// the four billable token buckets are DISJOINT and ADDITIVE, so they can be
// summed and priced without double-counting.
//
//	Input         — uncached, billable input tokens (input price)
//	CacheRead     — cache-read input tokens          (cache-read price)
//	CacheCreation — cache-write/creation tokens       (cache-creation price)
//	Output        — output tokens                     (output price)
//	Reasoning     — reasoning tokens; INFORMATIONAL only, already inside Output;
//	                never priced separately, never added to the total.
//
// Per-tool quirks the adapter must absorb before producing core.Tokens:
//   - Claude: input_tokens already excludes cache read/creation → map directly.
//   - Codex:  input_tokens INCLUDES cached_input_tokens → Input = input-cached,
//     CacheRead = cached; reasoning_output_tokens is inside output_tokens.
package core

import "time"

// ToolKind identifies which agent tool a session belongs to.
type ToolKind string

const (
	ToolClaude ToolKind = "claude"
	ToolCodex  ToolKind = "codex"
	ToolGemini ToolKind = "gemini"
	ToolAider  ToolKind = "aider"
)

// Mode is the billing mode of a session, detected from the tool's auth file.
type Mode string

const (
	ModeAPI          Mode = "api"          // billed per token in dollars
	ModeSubscription Mode = "subscription" // plan quota (Claude Max/Pro, Codex Plus/Pro)
	ModeUnknown      Mode = "unknown"
)

// Status is the lifecycle state of a session.
type Status string

const (
	StatusActive  Status = "active"  // written within the active window
	StatusIdle    Status = "idle"    // no writes within the active window
	StatusStopped Status = "stopped" // stopped by a Throttle cap or user action
	StatusEnded   Status = "ended"   // kept for history
)

// Tokens is a normalized, disjoint token count (see package doc).
type Tokens struct {
	Input         int64 `json:"in"`
	Output        int64 `json:"out"`
	CacheRead     int64 `json:"cache_read"`
	CacheCreation int64 `json:"cache_creation"`
	Reasoning     int64 `json:"reasoning"` // informational; already in Output
}

// Add returns the element-wise sum of two token counts.
func (t Tokens) Add(o Tokens) Tokens {
	return Tokens{
		Input:         t.Input + o.Input,
		Output:        t.Output + o.Output,
		CacheRead:     t.CacheRead + o.CacheRead,
		CacheCreation: t.CacheCreation + o.CacheCreation,
		Reasoning:     t.Reasoning + o.Reasoning,
	}
}

// Total is the sum of all billable buckets. Reasoning is NOT added (it is
// already counted inside Output) — adding it would double-count.
func (t Tokens) Total() int64 {
	return t.Input + t.Output + t.CacheRead + t.CacheCreation
}

// IsZero reports whether every bucket is zero.
func (t Tokens) IsZero() bool {
	return t.Input == 0 && t.Output == 0 && t.CacheRead == 0 &&
		t.CacheCreation == 0 && t.Reasoning == 0
}

// UsageEvent is one priced-attributable usage delta extracted from a log.
// Adapters emit a stream of these as they read new bytes.
type UsageEvent struct {
	Model        string    // model active for this event (for per-model pricing)
	Tokens       Tokens    // normalized delta for this event
	DedupKey     string    // stable key to drop duplicate log lines
	Timestamp    time.Time // event time (for idle detection / ordering)
	IsCompaction bool      // tag compaction spikes; do not alarm on these

	// SessionID/ProjectPath let a single log file carry events for MULTIPLE
	// sessions (e.g. Gemini's one telemetry.log). When SessionID is empty the
	// event belongs to the file's primary session (the one-file-per-session
	// case: Claude, Codex, Aider).
	SessionID   string
	ProjectPath string

	// CostOverride lets an adapter supply the dollar cost directly (e.g. Aider
	// prints "Cost: $X" in its history) instead of having the daemon price the
	// tokens. Used only when HasCostOverride is true.
	CostOverride    float64
	HasCostOverride bool
}

// SessionMeta is the per-session metadata an adapter discovers (usually from
// the first lines of a log file). Populated fields are merged into the live
// session; zero-value fields are ignored on incremental re-parses.
type SessionMeta struct {
	ID          string
	Tool        ToolKind
	ProjectPath string // the REAL cwd from log content, not a decoded folder name
	Mode        Mode
	IsSubagent  bool
	ParentID    string
	StartedAt   time.Time
	Found       bool // true once the adapter has seen the session_meta/first line
}

// ParseResult is what an adapter returns from one incremental read.
type ParseResult struct {
	Meta      SessionMeta  // Meta.Found == false when not (yet) seen
	Events    []UsageEvent // usage deltas read this pass (already deduped within-file)
	NewOffset int64        // byte offset to resume from next time
	LastEvent time.Time    // timestamp of the last line read (for last_seen)
}
