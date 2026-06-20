package core

// Adapter is the contract every per-tool log parser implements. The daemon
// owns discovery (fsnotify) and pricing; an adapter only knows how to find its
// log files and turn raw bytes into normalized UsageEvents.
//
// Adapters MUST:
//   - read incrementally from a byte offset (never re-parse a whole file),
//   - tolerate a truncated final line (return what parsed; resume next time),
//   - skip unparseable / old-format lines instead of erroring the whole file,
//   - emit normalized core.Tokens (see the package doc on token buckets).
type Adapter interface {
	// Tool identifies the tool this adapter handles.
	Tool() ToolKind

	// Roots returns the directories to watch recursively for this tool's logs.
	// They may not exist yet (the tool may be installed later); callers tolerate that.
	Roots() []string

	// SessionFileID reports whether path is a top-level session log this adapter
	// owns and, if so, the session ID derived from it. Subagent/auxiliary files
	// are still owned (so they can be folded into a parent) but the adapter marks
	// them via SessionMeta.IsSubagent during Parse.
	SessionFileID(path string) (id string, ok bool)

	// Parse reads path from fromOffset to EOF and returns the new events, any
	// session metadata seen, and the new byte offset to resume from.
	Parse(path string, fromOffset int64) (ParseResult, error)

	// DetectMode returns the current billing mode for this tool, read from its
	// auth file (not the session log). May change between sessions.
	DetectMode() Mode

	// Capabilities reports, honestly, what Throttle can do for this tool — so
	// the dashboard never implies a capability a tool can't back.
	Capabilities() Capabilities
}

// Capabilities is the honest per-tool capability gradient (THROTTLE-RESEARCH §1).
type Capabilities struct {
	Monitor                bool   `json:"monitor"`                  // live spend tracking
	HardCap                bool   `json:"hard_cap"`                 // block at a boundary via a hook
	LiveInject             bool   `json:"live_inject"`              // push context into a running session
	RulesSurviveCompaction bool   `json:"rules_survive_compaction"` // rules persist past compaction
	StopMechanism          string `json:"stop_mechanism"`           // "hook" | "process-kill"
	MonitorConfidence      string `json:"monitor_confidence"`       // "exact" | "best-effort"
	Note                   string `json:"note,omitempty"`           // honesty caveat for the UI
}
