// Package aider is the Aider log adapter. Aider has NO central log root and NO
// hook system, so this is honest "monitor + best-effort":
//   - monitoring reads each project's .aider.chat.history.md, which prints
//     concrete lines like:
//       > Tokens: 2.8k sent, 27 received. Cost: $0.0029 message, $0.0029 session.
//     We take per-message sent/received tokens and the per-message dollar cost
//     directly from the file (CostOverride) — no pricing guesswork.
//   - because there is no central root, the user opts projects in via
//     THROTTLE_AIDER_DIRS (the installer can populate this).
//   - enforcement is process-kill only (no hooks); rules via CONVENTIONS.md.
package aider

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jagannivas/throttle/internal/core"
)

const historyName = ".aider.chat.history.md"

// Adapter implements core.Adapter for Aider.
type Adapter struct {
	dirs []string // watched project directories (opt-in)
}

// New reads watched dirs from THROTTLE_AIDER_DIRS (os-path-list separated).
func New() *Adapter {
	var dirs []string
	if v := os.Getenv("THROTTLE_AIDER_DIRS"); v != "" {
		for _, d := range strings.Split(v, string(os.PathListSeparator)) {
			if d = strings.TrimSpace(d); d != "" {
				dirs = append(dirs, d)
			}
		}
	}
	return &Adapter{dirs: dirs}
}

// NewWithDirs wires explicit project dirs (tests).
func NewWithDirs(dirs ...string) *Adapter { return &Adapter{dirs: dirs} }

func (a *Adapter) Tool() core.ToolKind { return core.ToolAider }
func (a *Adapter) Roots() []string     { return a.dirs }

func (a *Adapter) Capabilities() core.Capabilities {
	return core.Capabilities{
		Monitor:                true,
		HardCap:                false,
		LiveInject:             false,
		RulesSurviveCompaction: false,
		StopMechanism:          "process-kill",
		MonitorConfidence:      "best-effort",
		Note:                   "no central log: opt in projects via THROTTLE_AIDER_DIRS; no hooks (process-kill); rules via CONVENTIONS.md",
	}
}

// SessionFileID matches a project's .aider.chat.history.md; the session id is
// the project directory (one Aider session per project).
func (a *Adapter) SessionFileID(path string) (string, bool) {
	if filepath.Base(path) != historyName {
		return "", false
	}
	return filepath.Dir(path), true
}

var (
	reTokens = regexp.MustCompile(`Tokens:\s*([\d.]+[kKmM]?)\s*sent,\s*([\d.]+[kKmM]?)\s*received`)
	reCost   = regexp.MustCompile(`Cost:\s*\$([\d.]+)\s*message`)
	reModel  = regexp.MustCompile(`Model:\s*([^\s]+)`)
)

// Parse reads new complete lines and emits one event per "Tokens: … Cost: …"
// line. Cost comes straight from the file (CostOverride).
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

	if !res.Meta.Found {
		res.Meta = core.SessionMeta{
			ID:          filepath.Dir(path),
			Tool:        core.ToolAider,
			ProjectPath: filepath.Dir(path),
			Found:       true,
		}
	}

	r := bufio.NewReaderSize(f, 1<<20)
	offset := fromOffset
	model := ""

	for {
		chunk, rerr := r.ReadBytes('\n')
		if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			a.handleLine(string(chunk), &res, &model)
			offset += int64(len(chunk))
		}
		if rerr != nil {
			break
		}
	}
	res.NewOffset = offset
	return res, nil
}

func (a *Adapter) handleLine(line string, res *core.ParseResult, model *string) {
	if m := reModel.FindStringSubmatch(line); m != nil {
		*model = m[1]
	}
	tok := reTokens.FindStringSubmatch(line)
	if tok == nil {
		return
	}
	ev := core.UsageEvent{
		Model: *model,
		Tokens: core.Tokens{
			Input:  parseTokenNum(tok[1]),
			Output: parseTokenNum(tok[2]),
		},
	}
	if c := reCost.FindStringSubmatch(line); c != nil {
		if v, err := strconv.ParseFloat(c[1], 64); err == nil {
			ev.CostOverride = v
			ev.HasCostOverride = true
		}
	}
	if ev.Tokens.IsZero() && !ev.HasCostOverride {
		return
	}
	res.Events = append(res.Events, ev)
}

// parseTokenNum turns "2.8k" / "1.0M" / "27" into a token count.
func parseTokenNum(s string) int64 {
	s = strings.TrimSpace(s)
	mult := 1.0
	if len(s) > 0 {
		switch s[len(s)-1] {
		case 'k', 'K':
			mult = 1e3
			s = s[:len(s)-1]
		case 'm', 'M':
			mult = 1e6
			s = s[:len(s)-1]
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(v * mult)
}

// DetectMode: Aider always bills via the underlying provider API key.
func (a *Adapter) DetectMode() core.Mode { return core.ModeAPI }
