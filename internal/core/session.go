package core

import "time"

// Caps are the hard limits applied to a session. Zero means "no cap".
// Dollar caps apply to API-mode sessions; token caps apply to everyone
// (and are the primary lever for subscription sessions).
type Caps struct {
	SessionUSD    float64 `json:"session_usd"`
	SessionTokens int64   `json:"session_tokens"`
	DayUSD        float64 `json:"day_usd"`
	DayTokens     int64   `json:"day_tokens"`
}

// IsZero reports whether no caps are set.
func (c Caps) IsZero() bool {
	return c.SessionUSD == 0 && c.SessionTokens == 0 && c.DayUSD == 0 && c.DayTokens == 0
}

// Session is one live (or historical) agent session — one dashboard row.
// Subagent sessions are folded into their parent and never surface as a row.
type Session struct {
	ID          string   `json:"id"`
	Tool        ToolKind `json:"tool"`
	ProjectPath string   `json:"project_path"`
	Model       string   `json:"model"`
	Mode        Mode     `json:"mode"`

	Tokens  Tokens  `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`

	QuotaUsed      float64 `json:"quota_used"`
	QuotaRemaining float64 `json:"quota_remaining"`

	Caps     Caps     `json:"caps"`
	Rules    []string `json:"rules"`
	Status   Status   `json:"status"`
	StopFlag bool     `json:"stop_flag"`

	ByteOffset int64     `json:"byte_offset"`
	FilePath   string    `json:"file_path"`
	StartedAt  time.Time `json:"started_at"`
	LastSeen   time.Time `json:"last_seen"`

	IsSubagent bool   `json:"is_subagent"`
	ParentID   string `json:"parent_id"`

	// Estimated is true when at least one event was priced with a fallback
	// (unknown model / missing price) — the UI should flag the cost as approximate.
	Estimated bool `json:"estimated"`
}
