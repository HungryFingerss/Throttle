// Package rules holds the persistent-rule and one-off-message state that the
// daemon injects into agent sessions. Rules are injected every turn via the
// UserPromptSubmit hook and — critically on Claude — re-injected after
// auto-compaction via the SessionStart:compact hook, so they survive context
// compaction (CLAUDE.md is NOT re-read after compaction; the hook is the
// guaranteed channel — THROTTLE-RESEARCH.md §3).
package rules

import (
	"strings"
	"sync"

	"github.com/jagannivas/throttle/internal/core"
)

// Store holds rules at three scopes plus a per-session one-off message queue.
// Safe for concurrent use.
type Store struct {
	mu         sync.RWMutex
	global     []string
	perTool    map[core.ToolKind][]string
	perSession map[string][]string
	oneOff     map[string][]string // session id -> queued one-shot messages
}

// New returns an empty store.
func New() *Store {
	return &Store{
		perTool:    map[core.ToolKind][]string{},
		perSession: map[string][]string{},
		oneOff:     map[string][]string{},
	}
}

// SetGlobal replaces the global rules.
func (s *Store) SetGlobal(r []string) { s.mu.Lock(); s.global = clean(r); s.mu.Unlock() }

// SetTool replaces the rules for one tool.
func (s *Store) SetTool(t core.ToolKind, r []string) {
	s.mu.Lock()
	s.perTool[t] = clean(r)
	s.mu.Unlock()
}

// SetSession replaces the rules for one session.
func (s *Store) SetSession(id string, r []string) {
	s.mu.Lock()
	s.perSession[id] = clean(r)
	s.mu.Unlock()
}

// Enqueue adds a one-off message to be injected on the session's next prompt.
func (s *Store) Enqueue(id, msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	s.mu.Lock()
	s.oneOff[id] = append(s.oneOff[id], msg)
	s.mu.Unlock()
}

// RulesFor returns the merged rules for a (tool, session): global, then tool,
// then session-specific.
func (s *Store) RulesFor(tool core.ToolKind, id string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	out = append(out, s.global...)
	out = append(out, s.perTool[tool]...)
	out = append(out, s.perSession[id]...)
	return out
}

// DrainOneOff returns and clears the queued one-off messages for a session.
func (s *Store) DrainOneOff(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.oneOff[id]
	delete(s.oneOff, id)
	return msgs
}

// View is the serializable snapshot for GET /api/rules.
type View struct {
	Global     []string                      `json:"global"`
	PerTool    map[core.ToolKind][]string    `json:"per_tool"`
	PerSession map[string][]string           `json:"per_session"`
}

// View returns a copy of the configured rules (excludes the one-off queue).
func (s *Store) View() View {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := View{Global: append([]string{}, s.global...), PerTool: map[core.ToolKind][]string{}, PerSession: map[string][]string{}}
	for k, r := range s.perTool {
		v.PerTool[k] = append([]string{}, r...)
	}
	for k, r := range s.perSession {
		v.PerSession[k] = append([]string{}, r...)
	}
	return v
}

// InjectText renders rules + one-off messages into the text the hook injects as
// additionalContext. Returns "" when there is nothing to inject. The block is
// clearly delimited so the agent (and the user) can see it is Throttle-managed.
func InjectText(rules, oneOff []string) string {
	rules = clean(rules)
	oneOff = clean(oneOff)
	if len(rules) == 0 && len(oneOff) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Throttle — persistent rules; always follow, even after compaction]\n")
	for i, r := range rules {
		b.WriteString(itoa(i+1) + ". " + r + "\n")
	}
	for _, m := range oneOff {
		b.WriteString("\n[Throttle — message from operator] " + m + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func clean(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
