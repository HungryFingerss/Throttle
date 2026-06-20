// Package config resolves per-OS, per-tool log roots and Throttle's own state
// directory, honoring environment overrides (CLAUDE_CONFIG_DIR, CODEX_HOME).
//
// MACHINE-WIDE discovery rests on these roots: each tool centralizes EVERY
// session under one per-user root regardless of the launch directory, so
// watching the root captures all sessions from all directories automatically.
package config

import (
	"os"
	"path/filepath"
)

// Home returns the current user's home directory ("" only if undiscoverable).
func Home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	// Last-ditch fallbacks for odd Windows environments.
	if h := os.Getenv("USERPROFILE"); h != "" {
		return h
	}
	return os.Getenv("HOME")
}

// claudeConfigDir is $CLAUDE_CONFIG_DIR if set, else ~/.claude.
func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(Home(), ".claude")
}

// codexHome is $CODEX_HOME if set, else ~/.codex.
func codexHome() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	return filepath.Join(Home(), ".codex")
}

// ClaudeProjectsRoot is the root holding every Claude Code session transcript.
func ClaudeProjectsRoot() string { return filepath.Join(claudeConfigDir(), "projects") }

// ClaudeSettingsPath is the user settings file where hooks are configured.
func ClaudeSettingsPath() string { return filepath.Join(claudeConfigDir(), "settings.json") }

// CodexSessionsRoot is the root holding every Codex rollout file (YYYY/MM/DD/...).
func CodexSessionsRoot() string { return filepath.Join(codexHome(), "sessions") }

// CodexAuthPath is the Codex auth file used to detect subscription vs API.
func CodexAuthPath() string { return filepath.Join(codexHome(), "auth.json") }

// GeminiRoot is the Gemini CLI config/home root.
func GeminiRoot() string { return filepath.Join(Home(), ".gemini") }

// ThrottleDir is where Throttle stores its own state (offsets, cache, db).
// Override with THROTTLE_DIR (used by the sandboxed E2E so tests never touch
// real state).
func ThrottleDir() string {
	if d := os.Getenv("THROTTLE_DIR"); d != "" {
		return d
	}
	return filepath.Join(Home(), ".throttle")
}

// StatePath / PriceCachePath are files under ThrottleDir.
func StatePath() string      { return filepath.Join(ThrottleDir(), "state.json") }
func PriceCachePath() string { return filepath.Join(ThrottleDir(), "prices.json") }
