// Package codex is the Codex CLI log adapter. It reads
// ~/.codex/sessions/YYYY/MM/DD/rollout-<id>.jsonl incrementally and emits
// normalized usage events. See THROTTLE-RESEARCH.md §2/§4 and the verified
// schema in PROGRESS.md.
//
// Codex specifics handled here (the accounting traps):
//   - input_tokens INCLUDES cached_input_tokens → Input = input-cached, CacheRead = cached.
//   - reasoning_output_tokens is INSIDE output_tokens → recorded as Reasoning, not added.
//   - per-turn model from turn_context.model (most recent wins; the tracker
//     also falls back to the session's last-known model across passes).
//   - dedup key = timestamp + last_token_usage signature (~47% dup rate);
//     the tally layer drops repeats.
//   - subagent sessions: session_meta.source is an OBJECT carrying a subagent
//     marker (vs the string "cli" for normal sessions) → excluded entirely to
//     avoid the 91× full-history-replay overcount.
//   - token_count.info may be null (rate-limit-only events) → skipped.
package codex

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

// Adapter implements core.Adapter for Codex CLI.
type Adapter struct {
	root     string
	authPath string
}

// New returns a Codex adapter using the resolved sessions root + auth path.
func New() *Adapter {
	return &Adapter{root: config.CodexSessionsRoot(), authPath: config.CodexAuthPath()}
}

// NewWithPaths wires explicit paths (tests/sandbox).
func NewWithPaths(root, authPath string) *Adapter {
	return &Adapter{root: root, authPath: authPath}
}

func (a *Adapter) Tool() core.ToolKind { return core.ToolCodex }
func (a *Adapter) Roots() []string     { return []string{a.root} }

// Capabilities: Codex has exact monitoring and a blocking pre-tool hook (cap),
// but live injection and compaction-persistence are partial (PreCompact/Stop
// workarounds, not a native re-injection channel).
func (a *Adapter) Capabilities() core.Capabilities {
	return core.Capabilities{
		Monitor:                true,
		HardCap:                true,
		LiveInject:             false,
		RulesSurviveCompaction: false,
		StopMechanism:          "hook",
		MonitorConfidence:      "exact",
		Note:                   "rules via AGENTS.md; post-compaction re-injection is best-effort",
	}
}

// SessionFileID accepts rollout-*.jsonl files anywhere under the sessions root
// (the YYYY/MM/DD nesting is arbitrary depth). The id is the trailing UUID of
// the filename, which is stable per session.
func (a *Adapter) SessionFileID(path string) (string, bool) {
	rel, err := filepath.Rel(a.root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	name := filepath.Base(path)
	if !strings.HasPrefix(name, "rollout-") || !strings.EqualFold(filepath.Ext(name), ".jsonl") {
		return "", false
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return trailingUUID(base), true
}

// trailingUUID extracts the last UUID (8-4-4-4-12) from a rollout filename;
// falls back to the whole base if no UUID is found.
func trailingUUID(base string) string {
	parts := strings.Split(base, "-")
	if len(parts) >= 5 {
		uuid := strings.Join(parts[len(parts)-5:], "-")
		if len(uuid) == 36 {
			return uuid
		}
	}
	return base
}

// ---- line schemas (only the fields we use) ----

type outer struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMetaPayload struct {
	ID     string          `json:"id"`
	Cwd    string          `json:"cwd"`
	Source json.RawMessage `json:"source"`
}

type turnContextPayload struct {
	Model string `json:"model"`
}

type eventMsgPayload struct {
	Type       string      `json:"type"`
	Info       *infoBlock  `json:"info"`
	RateLimits *rateLimits `json:"rate_limits"`
}

type rateLimits struct {
	Primary *rlWindow `json:"primary"`
}

type rlWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

type infoBlock struct {
	Last *tokenUsage `json:"last_token_usage"`
}

type tokenUsage struct {
	InputTokens          int64 `json:"input_tokens"`
	CachedInputTokens    int64 `json:"cached_input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	TotalTokens          int64 `json:"total_tokens"`
}

// Parse reads new complete lines from fromOffset and emits usage events.
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
	currentModel := ""
	pendingCompaction := false

	for {
		chunk, rerr := r.ReadBytes('\n')
		if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			a.handleLine(chunk, &res, &currentModel, &pendingCompaction)
			offset += int64(len(chunk))
		}
		if rerr != nil {
			break
		}
	}

	res.NewOffset = offset
	return res, nil
}

func (a *Adapter) handleLine(raw []byte, res *core.ParseResult, currentModel *string, pendingCompaction *bool) {
	var o outer
	if err := json.Unmarshal(raw, &o); err != nil {
		return // tolerate malformed / old-format lines
	}

	switch o.Type {
	case "session_meta":
		var p sessionMetaPayload
		if err := json.Unmarshal(o.Payload, &p); err != nil {
			return
		}
		if !res.Meta.Found {
			res.Meta = core.SessionMeta{
				ID:          p.ID,
				Tool:        core.ToolCodex,
				ProjectPath: p.Cwd,
				Found:       true,
			}
			if parent, isSub := subagentParent(p.Source); isSub {
				res.Meta.IsSubagent = true
				res.Meta.ParentID = parent
			}
			if ts, ok := parseTime(o.Timestamp); ok {
				res.Meta.StartedAt = ts
			}
		}

	case "turn_context":
		var p turnContextPayload
		if err := json.Unmarshal(o.Payload, &p); err == nil && p.Model != "" {
			*currentModel = p.Model
		}

	case "compacted":
		*pendingCompaction = true // tag the NEXT token_count as a compaction spike

	case "event_msg":
		var p eventMsgPayload
		if err := json.Unmarshal(o.Payload, &p); err != nil {
			return
		}
		if p.Type != "token_count" {
			return
		}
		// Subscription quota lives in rate_limits and may appear even when info
		// is null (a rate-limit-only token_count event).
		if p.RateLimits != nil && p.RateLimits.Primary != nil {
			res.Quota = &core.QuotaInfo{
				UsedPercent:   p.RateLimits.Primary.UsedPercent,
				WindowMinutes: p.RateLimits.Primary.WindowMinutes,
				ResetsAt:      p.RateLimits.Primary.ResetsAt,
			}
		}
		if p.Info == nil || p.Info.Last == nil {
			return // rate-limit-only token_count (no usage delta)
		}
		u := p.Info.Last
		ev := core.UsageEvent{
			Model: *currentModel,
			Tokens: core.Tokens{
				Input:         maxZero(u.InputTokens - u.CachedInputTokens),
				CacheRead:     u.CachedInputTokens,
				CacheCreation: 0,
				Output:        u.OutputTokens,
				Reasoning:     u.ReasoningOutputTokens,
			},
			DedupKey:     dedupKey(o.Timestamp, u),
			IsCompaction: *pendingCompaction,
		}
		*pendingCompaction = false
		if ev.Tokens.IsZero() {
			return
		}
		if ts, ok := parseTime(o.Timestamp); ok {
			ev.Timestamp = ts
			if ts.After(res.LastEvent) {
				res.LastEvent = ts
			}
		}
		res.Events = append(res.Events, ev)
	}
}

// subagentParent reports whether session_meta.source marks a subagent and, if
// so, the parent thread id. A normal session has source == the string "cli";
// a subagent has source as an OBJECT containing a subagent marker.
func subagentParent(src json.RawMessage) (string, bool) {
	s := strings.TrimSpace(string(src))
	if s == "" || s[0] != '{' {
		return "", false // string source (e.g. "cli") → not a subagent
	}
	var obj struct {
		Subagent *struct {
			ThreadSpawn *struct {
				ParentThreadID string `json:"parent_thread_id"`
			} `json:"thread_spawn"`
		} `json:"subagent"`
	}
	if err := json.Unmarshal(src, &obj); err != nil || obj.Subagent == nil {
		return "", false
	}
	if obj.Subagent.ThreadSpawn != nil {
		return obj.Subagent.ThreadSpawn.ParentThreadID, true
	}
	return "", true
}

func dedupKey(ts string, u *tokenUsage) string {
	return ts + "|" + itoa(u.InputTokens) + "|" + itoa(u.CachedInputTokens) + "|" +
		itoa(u.OutputTokens) + "|" + itoa(u.ReasoningOutputTokens)
}

// DetectMode reads ~/.codex/auth.json: auth_mode=="chatgpt" ⇒ subscription;
// a present OPENAI_API_KEY ⇒ API; else unknown.
func (a *Adapter) DetectMode() core.Mode {
	b, err := os.ReadFile(a.authPath)
	if err != nil {
		return core.ModeUnknown
	}
	var auth struct {
		AuthMode     string `json:"auth_mode"`
		OpenAIAPIKey string `json:"OPENAI_API_KEY"`
	}
	if err := json.Unmarshal(b, &auth); err != nil {
		return core.ModeUnknown
	}
	if auth.AuthMode == "chatgpt" {
		return core.ModeSubscription
	}
	if auth.OpenAIAPIKey != "" || auth.AuthMode == "apikey" {
		return core.ModeAPI
	}
	return core.ModeUnknown
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

func maxZero(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
