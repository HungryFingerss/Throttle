// Command throttle-hook is the thin hook binary installed into each tool's hook
// config. On a hook event it reads the tool's JSON from stdin, asks the daemon
// (/v1/check) for a decision, and translates the reply into the tool's native
// hook output.
//
// FAIL-OPEN IS ABSOLUTE: if the daemon is unreachable, slow, or returns
// anything unexpected, the hook allows the agent to proceed (exit 0, no
// blocking output). Throttle must never break the user's real work.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// hookInput is the subset of a tool's hook payload we read (Claude Code shape;
// Codex is close enough for the fields we use).
type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Source         string `json:"source"` // SessionStart: startup|resume|compact|clear
}

type checkRequest struct {
	Tool           string `json:"tool"`
	SessionID      string `json:"session_id"`
	Event          string `json:"event"`
	TranscriptPath string `json:"transcript_path"`
}

type checkResponse struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Inject   string `json:"inject"`
}

func main() {
	tool := flag.String("tool", "claude", "tool name (claude|codex|gemini|aider)")
	addr := flag.String("addr", "", "daemon address (default $THROTTLE_ADDR or 127.0.0.1:7878)")
	timeout := flag.Duration("timeout", 1500*time.Millisecond, "daemon call timeout (fail-open on expiry)")
	flag.Parse()

	daemonAddr := *addr
	if daemonAddr == "" {
		if e := os.Getenv("THROTTLE_ADDR"); e != "" {
			daemonAddr = e
		} else {
			daemonAddr = "127.0.0.1:7878"
		}
	}

	// Read stdin (the tool's hook payload). Any failure → fail-open.
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
	if err != nil {
		os.Exit(0)
	}
	var in hookInput
	_ = json.Unmarshal(raw, &in) // tolerate odd payloads; fields may stay empty

	event := in.HookEventName
	if event == "SessionStart" && in.Source != "" {
		event = "SessionStart:" + in.Source
	}

	resp, ok := callDaemon(daemonAddr, *timeout, checkRequest{
		Tool:           *tool,
		SessionID:      in.SessionID,
		Event:          event,
		TranscriptPath: in.TranscriptPath,
	})
	if !ok {
		os.Exit(0) // daemon unreachable → fail-open
	}

	out := render(*tool, in.HookEventName, resp)
	if out.stdout != "" {
		os.Stdout.WriteString(out.stdout)
	}
	if out.stderr != "" {
		fmt.Fprintln(os.Stderr, out.stderr)
	}
	os.Exit(out.exitCode)
}

func callDaemon(addr string, timeout time.Duration, req checkRequest) (checkResponse, bool) {
	body, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+addr+"/v1/check", bytes.NewReader(body))
	if err != nil {
		return checkResponse{}, false
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return checkResponse{}, false
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return checkResponse{}, false
	}
	var out checkResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&out); err != nil {
		return checkResponse{}, false
	}
	return out, true
}

type renderOutput struct {
	stdout   string
	stderr   string
	exitCode int
}

// render translates the daemon decision into the tool's native hook output as
// strings (no side effects), so the full matrix is unit-testable.
func render(tool, eventName string, resp checkResponse) renderOutput {
	if tool == "codex" {
		return renderCodex(eventName, resp)
	}
	return renderClaude(eventName, resp)
}

// renderClaude uses Claude Code's JSON hook output (deny via permissionDecision,
// inject via additionalContext); exit code stays 0 (the JSON drives behavior).
func renderClaude(eventName string, resp checkResponse) renderOutput {
	switch eventName {
	case "PreToolUse":
		if resp.Decision == "deny" {
			return renderOutput{stdout: claudeJSON("PreToolUse", map[string]any{
				"permissionDecision":       "deny",
				"permissionDecisionReason": orDefault(resp.Reason, "Throttle: cap reached"),
			})}
		}
		if resp.Reason != "" { // warn: visible without blocking
			return renderOutput{stderr: "Throttle: " + resp.Reason}
		}
	case "UserPromptSubmit":
		if resp.Inject != "" {
			return renderOutput{stdout: claudeJSON("UserPromptSubmit", map[string]any{"additionalContext": resp.Inject})}
		}
	case "SessionStart":
		if resp.Inject != "" {
			return renderOutput{stdout: claudeJSON("SessionStart", map[string]any{"additionalContext": resp.Inject})}
		}
	}
	return renderOutput{}
}

// renderCodex uses Codex's hook conventions: exit code 2 blocks a tool call;
// injection is returned as additionalContext JSON. (Codex's exact inject schema
// is [verify]; exit-2 blocking is the reliable cap mechanism.)
func renderCodex(eventName string, resp checkResponse) renderOutput {
	switch eventName {
	case "PreToolUse":
		if resp.Decision == "deny" {
			return renderOutput{stderr: "Throttle: " + orDefault(resp.Reason, "cap reached"), exitCode: 2}
		}
		if resp.Reason != "" {
			return renderOutput{stderr: "Throttle: " + resp.Reason}
		}
	case "UserPromptSubmit", "SessionStart":
		if resp.Inject != "" {
			b, _ := json.Marshal(map[string]any{"additionalContext": resp.Inject})
			return renderOutput{stdout: string(b)}
		}
	}
	return renderOutput{}
}

func claudeJSON(eventName string, specific map[string]any) string {
	specific["hookEventName"] = eventName
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": specific})
	return string(b)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
