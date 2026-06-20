// Package store persists Throttle's live session state (including per-session
// byte offsets) to a JSON file, written atomically. This lets the daemon resume
// without re-parsing multi-gigabyte logs after a restart.
//
// History/analytics will move to SQLite in M7; this JSON store covers the
// resume-and-restore need for the live spine.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/jagannivas/throttle/internal/core"
)

type stateFile struct {
	Version  int            `json:"version"`
	Sessions []core.Session `json:"sessions"`
}

// Save atomically writes sessions to path (temp file + rename).
func Save(path string, sessions []core.Session) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(stateFile{Version: 1, Sessions: sessions}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads persisted sessions. A missing file yields an empty slice and no
// error (first run). A corrupt file yields an error; callers may choose to
// start fresh rather than fail.
func Load(path string) ([]core.Session, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sf stateFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return nil, err
	}
	return sf.Sessions, nil
}
