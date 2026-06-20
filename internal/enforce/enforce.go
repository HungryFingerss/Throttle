// Package enforce implements Throttle's cap/kill-switch logic. It is the
// Checker the hook calls before each tool boundary: it looks up the live
// session, compares spend against the applicable caps (per-session, per-tool,
// per-day, in dollars and tokens), and returns allow / deny / warn.
//
// Fail-open is structural: an unknown session or any uncertainty yields allow.
// The daemon (not this package) guarantees the hook also fails open when the
// daemon itself is unreachable.
package enforce

import (
	"fmt"
	"sync"
	"time"

	"github.com/jagannivas/throttle/internal/api"
	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/tally"
)

// Limits holds the configured caps at each scope. Zero fields mean "no cap".
type Limits struct {
	Global     core.Caps                   `json:"global"`
	PerTool    map[core.ToolKind]core.Caps `json:"per_tool"`
	PerSession map[string]core.Caps        `json:"per_session"`
}

// Enforcer is the live cap evaluator. Safe for concurrent use.
type Enforcer struct {
	mu           sync.RWMutex
	tracker      *tally.Tracker
	limits       Limits
	warnFraction float64
	now          func() time.Time
}

// New builds an Enforcer over a tracker. warnFraction (e.g. 0.8) is the
// approaching-cap threshold.
func New(tracker *tally.Tracker) *Enforcer {
	return &Enforcer{
		tracker: tracker,
		limits: Limits{
			PerTool:    map[core.ToolKind]core.Caps{},
			PerSession: map[string]core.Caps{},
		},
		warnFraction: 0.8,
		now:          time.Now,
	}
}

// SetClock overrides the clock (tests).
func (e *Enforcer) SetClock(f func() time.Time) { e.now = f }

// SetWarnFraction overrides the warn threshold.
func (e *Enforcer) SetWarnFraction(f float64) { e.warnFraction = f }

// --- cap configuration (driven by the dashboard via the API) ---

// SetGlobalCaps sets the caps that apply to every session and the day aggregate.
func (e *Enforcer) SetGlobalCaps(c core.Caps) {
	e.mu.Lock()
	e.limits.Global = c
	e.mu.Unlock()
}

// SetToolCaps sets caps for one tool.
func (e *Enforcer) SetToolCaps(tool core.ToolKind, c core.Caps) {
	e.mu.Lock()
	e.limits.PerTool[tool] = c
	e.mu.Unlock()
}

// SetSessionCaps sets caps for one session and reflects them onto the live
// session for dashboard display.
func (e *Enforcer) SetSessionCaps(id string, c core.Caps) {
	e.mu.Lock()
	e.limits.PerSession[id] = c
	e.mu.Unlock()
	e.tracker.SetSessionCaps(id, c)
}

// Limits returns a copy of the current limits (for GET /api/caps).
func (e *Enforcer) Limits() Limits {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := Limits{
		Global:     e.limits.Global,
		PerTool:    map[core.ToolKind]core.Caps{},
		PerSession: map[string]core.Caps{},
	}
	for k, v := range e.limits.PerTool {
		out.PerTool[k] = v
	}
	for k, v := range e.limits.PerSession {
		out.PerSession[k] = v
	}
	return out
}

// LimitsView returns the limits as an opaque value for the API (satisfies
// api.Controls without creating an import cycle on the concrete type).
func (e *Enforcer) LimitsView() any { return e.Limits() }

// sessionCaps resolves the effective per-session caps: per-session override wins
// over per-tool, which wins over global, field by field (a non-zero value at a
// more specific scope overrides a broader one).
func (e *Enforcer) sessionCaps(s core.Session) core.Caps {
	e.mu.RLock()
	defer e.mu.RUnlock()
	c := e.limits.Global
	if tc, ok := e.limits.PerTool[s.Tool]; ok {
		c = mergeCaps(c, tc)
	}
	if sc, ok := e.limits.PerSession[s.ID]; ok {
		c = mergeCaps(c, sc)
	}
	return c
}

func mergeCaps(base, over core.Caps) core.Caps {
	if over.SessionUSD != 0 {
		base.SessionUSD = over.SessionUSD
	}
	if over.SessionTokens != 0 {
		base.SessionTokens = over.SessionTokens
	}
	if over.DayUSD != 0 {
		base.DayUSD = over.DayUSD
	}
	if over.DayTokens != 0 {
		base.DayTokens = over.DayTokens
	}
	return base
}

// Check implements api.Checker. Order: manual stop → over-cap deny → warn → allow.
func (e *Enforcer) Check(req api.CheckRequest) api.CheckResponse {
	s, ok := e.tracker.Get(req.SessionID)
	if !ok {
		// Unknown session: we have no spend basis → fail-open.
		return api.CheckResponse{Decision: api.DecisionAllow}
	}

	if s.StopFlag {
		return api.CheckResponse{Decision: api.DecisionDeny, Reason: "session stopped from Throttle dashboard"}
	}

	caps := e.sessionCaps(s)
	if caps.IsZero() {
		return api.CheckResponse{Decision: api.DecisionAllow}
	}

	daySpend, dayTokens := e.dayTotals(s.Tool)

	type check struct {
		name      string
		value     float64
		cap       float64
	}
	checks := []check{
		{"session $", s.CostUSD, caps.SessionUSD},
		{"session tokens", float64(s.Tokens.Total()), float64(caps.SessionTokens)},
		{"daily $", daySpend, caps.DayUSD},
		{"daily tokens", dayTokens, float64(caps.DayTokens)},
	}

	// Deny if any cap is met or exceeded.
	for _, c := range checks {
		if c.cap > 0 && c.value >= c.cap {
			return api.CheckResponse{
				Decision: api.DecisionDeny,
				Reason:   fmt.Sprintf("%s cap reached: %s of %s", c.name, num(c.value), num(c.cap)),
			}
		}
	}

	// Otherwise warn if approaching any cap.
	for _, c := range checks {
		if c.cap > 0 && c.value >= c.cap*e.warnFraction {
			pct := int(c.value / c.cap * 100)
			return api.CheckResponse{
				Decision: api.DecisionAllow,
				Reason:   fmt.Sprintf("approaching %s cap: %d%% (%s of %s)", c.name, pct, num(c.value), num(c.cap)),
			}
		}
	}

	return api.CheckResponse{Decision: api.DecisionAllow}
}

// dayTotals sums spend/tokens for the current local day, overall (for the day
// caps) — filtered to the tool when a per-tool day view is needed. Here we sum
// across ALL sessions active today for the daily caps, since the daily caps are
// global; tool scoping is handled by SetToolCaps applying to that tool's
// sessions only.
func (e *Enforcer) dayTotals(_ core.ToolKind) (usd float64, tokens float64) {
	now := e.now()
	y, m, d := now.Date()
	for _, s := range e.tracker.Snapshot() {
		ly, lm, ld := s.LastSeen.Date()
		if ly == y && lm == m && ld == d {
			usd += s.CostUSD
			tokens += float64(s.Tokens.Total())
		}
	}
	return usd, tokens
}

func num(v float64) string {
	if v >= 1000 || v == float64(int64(v)) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.4f", v)
}
