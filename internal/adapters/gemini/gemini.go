// Package gemini is the Gemini CLI log adapter. Gemini writes token usage to a
// single OpenTelemetry log file (~/.gemini/telemetry.log) that holds events for
// MANY sessions — so this adapter emits per-event SessionIDs and the tracker
// demuxes them. Monitoring is honest "best-effort":
//   - telemetry is OFF by default (the user/installer must enable it: settings
//     target=local, outfile=.gemini/telemetry.log);
//   - the file is concatenated (pretty-printed) JSON objects, parsed with a
//     streaming json.Decoder (verified field names from the Gemini telemetry
//     docs + the gemini-cli usage-analyzer; no real log was available on the
//     build machine to capture, so this is schema-derived).
//
// Rules/persistence is the STRONG Gemini capability: GEMINI.md is re-sent with
// every prompt, so rules survive compaction (handled via the memory file, not a
// hook).
package gemini

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jagannivas/throttle/internal/config"
	"github.com/jagannivas/throttle/internal/core"
)

const telemetryFile = "telemetry.log"

// Adapter implements core.Adapter for Gemini CLI.
type Adapter struct {
	root string // ~/.gemini
}

func New() *Adapter                  { return &Adapter{root: config.GeminiRoot()} }
func NewWithRoot(root string) *Adapter { return &Adapter{root: root} }

func (a *Adapter) Tool() core.ToolKind { return core.ToolGemini }
func (a *Adapter) Roots() []string     { return []string{a.root} }

func (a *Adapter) Capabilities() core.Capabilities {
	return core.Capabilities{
		Monitor:                true,
		HardCap:                false,
		LiveInject:             true,
		RulesSurviveCompaction: true, // GEMINI.md re-sent with every prompt
		StopMechanism:          "process-kill",
		MonitorConfidence:      "best-effort",
		Note:                   "monitoring needs Gemini telemetry enabled (target=local); rules via GEMINI.md (re-sent every prompt)",
	}
}

// SessionFileID matches the telemetry log; the returned id is a file-level
// placeholder — real sessions come from each event's session.id.
func (a *Adapter) SessionFileID(path string) (string, bool) {
	if filepath.Base(path) != telemetryFile {
		return "", false
	}
	return "gemini-telemetry", true
}

type record struct {
	Timestamp  string `json:"timestamp"`
	EventName  string `json:"event.name"`
	Attributes attrs  `json:"attributes"`
}

type attrs struct {
	EventName string `json:"event.name"`
	Model     string `json:"model"`
	SessionID string `json:"session.id"`
	Input     int64  `json:"input_token_count"`
	Output    int64  `json:"output_token_count"`
	Cached    int64  `json:"cached_content_token_count"`
	Thoughts  int64  `json:"thoughts_token_count"`
	Total     int64  `json:"total_token_count"`
}

// Parse streams concatenated JSON records from fromOffset. A truncated trailing
// object (still being written) is left unread: NewOffset stays at the end of
// the last fully-decoded record.
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

	dec := json.NewDecoder(f)
	var lastGood int64
	for {
		var rec record
		if err := dec.Decode(&rec); err != nil {
			break // EOF or a partial trailing object → stop at lastGood
		}
		lastGood = dec.InputOffset()
		a.handleRecord(rec, &res)
	}
	res.NewOffset = fromOffset + lastGood
	return res, nil
}

func (a *Adapter) handleRecord(rec record, res *core.ParseResult) {
	at := rec.Attributes
	if at.Input == 0 && at.Output == 0 {
		return // not a usage (api_response) record
	}
	ev := core.UsageEvent{
		Model:     at.Model,
		SessionID: at.SessionID,
		Tokens: core.Tokens{
			Input:     maxZero(at.Input - at.Cached),
			CacheRead: at.Cached,
			Output:    at.Output,
			Reasoning: at.Thoughts,
		},
	}
	if ev.Tokens.IsZero() {
		return
	}
	if ts, ok := parseTime(rec.Timestamp); ok {
		ev.Timestamp = ts
		if ts.After(res.LastEvent) {
			res.LastEvent = ts
		}
	}
	res.Events = append(res.Events, ev)
}

// DetectMode: a Gemini API key ⇒ API billing; otherwise unknown (Code Assist /
// subscription detection is not reliably available locally).
func (a *Adapter) DetectMode() core.Mode {
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
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
