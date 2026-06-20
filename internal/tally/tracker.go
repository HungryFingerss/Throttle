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
// leaves existing state intact and simply returns. Returns the resulting
// update kind (or empty string if nothing was emitted).
func (t *Tracker) HandlePath(path string) {
	a, id, ok := t.match(path)
	if !ok {
		return
	}

	t.mu.Lock()
	s, exists := t.sessions[id]
	isNew := false
	if !exists {
		s = &core.Session{
			ID:        id,
			Tool:      a.Tool(),
			FilePath:  path,
			Status:    core.StatusActive,
			StartedAt: t.now(),
			LastSeen:  t.now(),
		}
		t.sessions[id] = s
		t.seen[id] = map[string]struct{}{}
		isNew = true
	}

	res, err := a.Parse(path, s.ByteOffset)
	if err != nil {
		t.mu.Unlock()
		return // fail-soft: don't corrupt offset/state on a bad read
	}

	if res.Meta.Found {
		if res.Meta.ProjectPath != "" {
			s.ProjectPath = res.Meta.ProjectPath
		}
		if res.Meta.IsSubagent {
			s.IsSubagent = true
			s.ParentID = res.Meta.ParentID
		}
		if !res.Meta.StartedAt.IsZero() {
			s.StartedAt = res.Meta.StartedAt
		}
		if s.Mode == "" || s.Mode == core.ModeUnknown {
			s.Mode = t.modeFor(a)
		}
	}

	s.ByteOffset = res.NewOffset

	// Subagent sessions are a replay of parent history (the Codex 91× trap):
	// never count their tokens and never surface them as a row.
	if s.IsSubagent {
		t.mu.Unlock()
		return
	}

	seen := t.seen[id]
	for _, ev := range res.Events {
		if ev.DedupKey != "" {
			if _, dup := seen[ev.DedupKey]; dup {
				continue
			}
			seen[ev.DedupKey] = struct{}{}
		}
		s.Tokens = s.Tokens.Add(ev.Tokens)
		cost, est := t.prices.Cost(ev.Tokens, ev.Model)
		s.CostUSD += cost
		if est {
			s.Estimated = true
		}
		if ev.Model != "" {
			s.Model = ev.Model
		}
	}

	if !res.LastEvent.IsZero() {
		s.LastSeen = res.LastEvent
	} else {
		s.LastSeen = t.now()
	}
	if s.Status != core.StatusStopped {
		s.Status = core.StatusActive
	}

	snap := *s
	t.mu.Unlock()

	kind := SessionUpdate
	if isNew {
		kind = SessionNew
	}
	t.emit(Update{Kind: kind, Session: snap})
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
	}
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
