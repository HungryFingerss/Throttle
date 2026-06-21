// Package tally is Throttle's live accounting core. It owns the in-memory set
// of sessions and, for each file change, reads new bytes via the owning adapter,
// dedupes events, prices them per-model, attributes the current model, folds out
// subagent sessions, and tracks idle/active status. It is the single source of
// truth the dashboard renders and the enforcer reads.
package tally

import (
	"sync"
	"time"

	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/prices"
)

// UpdateKind labels a broadcast event.
type UpdateKind string

const (
	SessionNew    UpdateKind = "session_new"
	SessionUpdate UpdateKind = "session_update"
	SessionEnd    UpdateKind = "session_end"
)

// Update is pushed to subscribers (the dashboard WebSocket hub) on any change.
type Update struct {
	Kind    UpdateKind   `json:"type"`
	Session core.Session `json:"payload"`
}

// Tracker is the thread-safe live session model.
type Tracker struct {
	mu       sync.Mutex
	prices   *prices.Table
	adapters []core.Adapter
	sessions map[string]*core.Session
	seen     map[string]map[string]struct{} // sessionID -> dedup keys
	offsets  map[string]int64               // file path -> byte offset (per FILE)
	modeByTool map[core.ToolKind]core.Mode

	idleAfter time.Duration
	now       func() time.Time
	sink      func(Update)
}

// New builds a Tracker over the given adapters and price table.
func New(p *prices.Table, adapters []core.Adapter) *Tracker {
	t := &Tracker{
		prices:     p,
		adapters:   adapters,
		sessions:   map[string]*core.Session{},
		seen:       map[string]map[string]struct{}{},
		offsets:    map[string]int64{},
		modeByTool: map[core.ToolKind]core.Mode{},
		idleAfter:  90 * time.Second,
		now:        time.Now,
	}
	return t
}

// SetClock overrides the clock (tests).
func (t *Tracker) SetClock(f func() time.Time) { t.now = f }

// SetIdleAfter overrides the idle threshold.
func (t *Tracker) SetIdleAfter(d time.Duration) { t.idleAfter = d }

// SetSink registers the broadcast callback. It is invoked WITHOUT the lock held.
func (t *Tracker) SetSink(f func(Update)) { t.sink = f }

// match finds the adapter that owns a path and the session id it maps to.
func (t *Tracker) match(path string) (core.Adapter, string, bool) {
	for _, a := range t.adapters {
		if id, ok := a.SessionFileID(path); ok {
			return a, id, true
		}
	}
	return nil, "", false
}

// modeFor returns the (cached) billing mode for an adapter's tool.
func (t *Tracker) modeFor(a core.Adapter) core.Mode {
	tool := a.Tool()
	if m, ok := t.modeByTool[tool]; ok && m != core.ModeUnknown {
		return m
	}
	m := a.DetectMode()
	t.modeByTool[tool] = m
	return m
}

// HandlePath processes one file change. It is fail-soft: any parse/IO error
// leaves existing state intact and simply returns. Byte offsets are tracked per
// FILE; events are routed to a session by their SessionID (or the file's
// primary id for one-file-per-session tools).
func (t *Tracker) HandlePath(path string) {
	a, fileID, ok := t.match(path)
	if !ok {
		return
	}

	t.mu.Lock()

	res, err := a.Parse(path, t.offsets[path])
	if err != nil {
		t.mu.Unlock()
		return // fail-soft: don't corrupt offset/state on a bad read
	}
	t.offsets[path] = res.NewOffset

	touched := map[string]bool{} // id -> isNew
	ensure := func(id string) *core.Session {
		s, exists := t.sessions[id]
		if !exists {
			s = &core.Session{
				ID: id, Tool: a.Tool(), FilePath: path,
				Status: core.StatusActive, StartedAt: t.now(), LastSeen: t.now(),
			}
			t.sessions[id] = s
			t.seen[id] = map[string]struct{}{}
			touched[id] = true
		} else if _, ok := touched[id]; !ok {
			touched[id] = false
		}
		return s
	}

	// File-level metadata applies to the primary (file) session — but only for
	// one-file-per-session tools that actually report it. Multi-session files
	// (Gemini) report no Meta; their sessions are created from event SessionIDs
	// so the file never spawns an empty placeholder row.
	var primary *core.Session
	if res.Meta.Found {
		primary = ensure(fileID)
		primary.ByteOffset = res.NewOffset
		if res.Meta.ProjectPath != "" {
			primary.ProjectPath = res.Meta.ProjectPath
		}
		if res.Meta.IsSubagent {
			primary.IsSubagent = true
			primary.ParentID = res.Meta.ParentID
		}
		if !res.Meta.StartedAt.IsZero() {
			primary.StartedAt = res.Meta.StartedAt
		}
		if primary.Mode == "" || primary.Mode == core.ModeUnknown {
			primary.Mode = t.modeFor(a)
		}
		if res.Quota != nil {
			primary.QuotaUsed = res.Quota.UsedPercent
			primary.QuotaRemaining = 100 - res.Quota.UsedPercent
		}
	}

	for _, ev := range res.Events {
		id := ev.SessionID
		if id == "" {
			id = fileID
		}
		s := ensure(id)
		// Byte offsets are tracked per file in t.offsets; only the main
		// transcript drives the session's headline FilePath/ByteOffset (a
		// session spans the main file PLUS its subagent files).
		if ev.SubagentID == "" {
			s.ByteOffset = res.NewOffset
			s.FilePath = path
		}
		if s.Mode == "" || s.Mode == core.ModeUnknown {
			s.Mode = t.modeFor(a)
		}
		if ev.ProjectPath != "" && ev.SubagentID == "" {
			s.ProjectPath = ev.ProjectPath
		}

		// Codex-style replay subagent SESSIONS are excluded entirely (the 91×
		// trap). Claude subagents are NOT this — they arrive as SubagentID-tagged
		// events and ARE counted (folded into the parent below).
		if s.IsSubagent {
			continue
		}
		if ev.DedupKey != "" {
			if _, dup := t.seen[id][ev.DedupKey]; dup {
				continue
			}
			t.seen[id][ev.DedupKey] = struct{}{}
		}

		// Model attribution: prefer the event's model; fall back to the
		// session's last-known model across incremental passes / restarts.
		model := ev.Model
		if model == "" {
			model = s.Model
		}
		var addCost float64
		if ev.HasCostOverride {
			addCost = ev.CostOverride // adapter supplied cost (e.g. Aider)
		} else {
			c, est := t.prices.Cost(ev.Tokens, model)
			addCost = c
			if est {
				s.Estimated = true
			}
		}
		s.Tokens = s.Tokens.Add(ev.Tokens)
		s.CostUSD += addCost
		if ev.SubagentID != "" {
			// Counted into the parent total above; also itemize per (subagent,
			// day) for the dashboard breakdown. Subagents do NOT change the
			// session's displayed (main-agent) model.
			recordSubagent(s, ev, addCost)
		} else if model != "" {
			s.Model = model
		}
		if !ev.Timestamp.IsZero() {
			s.LastSeen = ev.Timestamp
		}
		if s.Status != core.StatusStopped {
			s.Status = core.StatusActive
		}
	}

	// If the file changed but produced no usage line, still bump the primary's
	// liveness so it reads as active (single-session tools only).
	if primary != nil && res.LastEvent.IsZero() && !primary.IsSubagent {
		primary.LastSeen = t.now()
		if primary.Status != core.StatusStopped {
			primary.Status = core.StatusActive
		}
	}

	// Snapshot touched, non-subagent sessions while still holding the lock.
	type out struct {
		s     core.Session
		isNew bool
	}
	var outs []out
	for id, isNew := range touched {
		s := t.sessions[id]
		if s == nil || s.IsSubagent {
			continue
		}
		outs = append(outs, out{*s, isNew})
	}
	t.mu.Unlock()

	for _, o := range outs {
		kind := SessionUpdate
		if o.isNew {
			kind = SessionNew
		}
		t.emit(Update{Kind: kind, Session: o.s})
	}
}

// SweepIdle marks active sessions idle when they have not been written within
// the idle window. Returns the sessions that changed.
func (t *Tracker) SweepIdle() {
	t.mu.Lock()
	var changed []core.Session
	cutoff := t.now().Add(-t.idleAfter)
	for _, s := range t.sessions {
		if s.IsSubagent {
			continue
		}
		if s.Status == core.StatusActive && s.LastSeen.Before(cutoff) {
			s.Status = core.StatusIdle
			changed = append(changed, *s)
		}
	}
	t.mu.Unlock()
	for _, s := range changed {
		t.emit(Update{Kind: SessionUpdate, Session: s})
	}
}

func (t *Tracker) emit(u Update) {
	if t.sink != nil {
		t.sink(u)
	}
}

// Snapshot returns a copy of all non-subagent sessions (for the dashboard's
// initial state). Order is unspecified.
func (t *Tracker) Snapshot() []core.Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]core.Session, 0, len(t.sessions))
	for _, s := range t.sessions {
		if s.IsSubagent {
			continue
		}
		out = append(out, *s)
	}
	return out
}

// ExportAll returns a copy of EVERY session (including subagents) so byte
// offsets persist across restarts and files are not re-parsed.
func (t *Tracker) ExportAll() []core.Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]core.Session, 0, len(t.sessions))
	for _, s := range t.sessions {
		out = append(out, *s)
	}
	return out
}

// Import restores sessions (with their byte offsets) from persisted state. Dedup
// sets start empty: resumed reads only see new bytes, so old lines are not
// re-counted. Existing sessions are left untouched.
func (t *Tracker) Import(sessions []core.Session) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range sessions {
		s := sessions[i]
		if _, exists := t.sessions[s.ID]; exists {
			continue
		}
		cp := s
		t.sessions[s.ID] = &cp
		t.seen[s.ID] = map[string]struct{}{}
		// Restore the per-file offset so resumed reads start where we left off.
		if s.FilePath != "" && s.ByteOffset > t.offsets[s.FilePath] {
			t.offsets[s.FilePath] = s.ByteOffset
		}
	}
}

// ExportOffsets returns a copy of every file's byte offset, for persistence.
// A session spans multiple files (main transcript + subagent files), so these
// must be saved in full or a restart would re-parse subagent files and
// double-count their spend.
func (t *Tracker) ExportOffsets() map[string]int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]int64, len(t.offsets))
	for k, v := range t.offsets {
		out[k] = v
	}
	return out
}

// ImportOffsets restores persisted per-file byte offsets (max-wins so we never
// rewind past already-counted bytes).
func (t *Tracker) ImportOffsets(m map[string]int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, v := range m {
		if v > t.offsets[k] {
			t.offsets[k] = v
		}
	}
}

// recordSubagent itemizes a subagent's usage on its day for the dashboard
// breakdown. The same tokens/cost are already folded into the parent total.
func recordSubagent(s *core.Session, ev core.UsageEvent, cost float64) {
	day := ""
	if !ev.Timestamp.IsZero() {
		day = ev.Timestamp.Format("2006-01-02")
	}
	for i := range s.Subagents {
		e := &s.Subagents[i]
		if e.ID == ev.SubagentID && e.Day == day {
			e.Tokens = e.Tokens.Add(ev.Tokens)
			e.CostUSD += cost
			if ev.Model != "" {
				e.Model = ev.Model
			}
			return
		}
	}
	s.Subagents = append(s.Subagents, core.SubagentUsage{
		ID:      ev.SubagentID,
		Day:     day,
		Model:   ev.Model,
		Tokens:  ev.Tokens,
		CostUSD: cost,
		Compact: ev.SubagentCompact,
	})
}

// SetStop sets (or clears) a session's manual stop flag. When stopped, the
// enforcer denies the session's next tool call (graceful stop at the boundary).
// Returns the updated session and whether it existed.
func (t *Tracker) SetStop(id string, stop bool) (core.Session, bool) {
	t.mu.Lock()
	s, ok := t.sessions[id]
	if !ok || s.IsSubagent {
		t.mu.Unlock()
		return core.Session{}, false
	}
	s.StopFlag = stop
	if stop {
		s.Status = core.StatusStopped
	} else if s.Status == core.StatusStopped {
		s.Status = core.StatusActive
	}
	snap := *s
	t.mu.Unlock()
	t.emit(Update{Kind: SessionUpdate, Session: snap})
	return snap, true
}

// SetSessionCaps writes resolved caps onto a live session so the dashboard can
// render progress toward them. No effect if the session is unknown.
func (t *Tracker) SetSessionCaps(id string, caps core.Caps) {
	t.mu.Lock()
	s, ok := t.sessions[id]
	if !ok {
		t.mu.Unlock()
		return
	}
	s.Caps = caps
	snap := *s
	t.mu.Unlock()
	t.emit(Update{Kind: SessionUpdate, Session: snap})
}

// SetSessionRules writes the resolved rule list onto a live session for
// dashboard display. No effect if the session is unknown.
func (t *Tracker) SetSessionRules(id string, rules []string) {
	t.mu.Lock()
	s, ok := t.sessions[id]
	if !ok {
		t.mu.Unlock()
		return
	}
	s.Rules = rules
	snap := *s
	t.mu.Unlock()
	t.emit(Update{Kind: SessionUpdate, Session: snap})
}

// Get returns a copy of one session by id.
func (t *Tracker) Get(id string) (core.Session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[id]
	if !ok || s.IsSubagent {
		return core.Session{}, false
	}
	return *s, true
}
