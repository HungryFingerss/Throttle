package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderDenyBlocksPreToolUse(t *testing.T) {
	out := render("claude", "PreToolUse", checkResponse{Decision: "deny", Reason: "session $ cap reached: 5 of 5"})
	if out.stdout == "" {
		t.Fatal("deny should produce stdout JSON")
	}
	var parsed struct {
		Hook struct {
			Event  string `json:"hookEventName"`
			Dec    string `json:"permissionDecision"`
			Reason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out.stdout), &parsed); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out.stdout)
	}
	if parsed.Hook.Event != "PreToolUse" || parsed.Hook.Dec != "deny" {
		t.Fatalf("wrong decision payload: %+v", parsed.Hook)
	}
	if !strings.Contains(parsed.Hook.Reason, "cap reached") {
		t.Fatalf("reason = %q", parsed.Hook.Reason)
	}
}

func TestRenderAllowIsSilent(t *testing.T) {
	out := render("claude", "PreToolUse", checkResponse{Decision: "allow"})
	if out.stdout != "" || out.stderr != "" || out.exitCode != 0 {
		t.Fatalf("allow must be silent exit-0, got %+v", out)
	}
}

func TestRenderWarnGoesToStderr(t *testing.T) {
	out := render("claude", "PreToolUse", checkResponse{Decision: "allow", Reason: "approaching session $ cap: 90%"})
	if out.stdout != "" {
		t.Fatalf("warn must not block (no stdout), got %q", out.stdout)
	}
	if !strings.Contains(out.stderr, "approaching") {
		t.Fatalf("warn stderr = %q", out.stderr)
	}
}

func TestRenderInjectOnPrompt(t *testing.T) {
	out := render("claude", "UserPromptSubmit", checkResponse{Decision: "allow", Inject: "Remember: no force-push."})
	if !strings.Contains(out.stdout, "additionalContext") || !strings.Contains(out.stdout, "force-push") {
		t.Fatalf("inject output = %q", out.stdout)
	}
}

func TestRenderInjectOnSessionStart(t *testing.T) {
	out := render("claude", "SessionStart", checkResponse{Decision: "allow", Inject: "RULES SURVIVE COMPACTION"})
	if !strings.Contains(out.stdout, "SessionStart") || !strings.Contains(out.stdout, "SURVIVE") {
		t.Fatalf("session-start inject = %q", out.stdout)
	}
}

func TestRenderCodexDenyExits2(t *testing.T) {
	out := render("codex", "PreToolUse", checkResponse{Decision: "deny", Reason: "daily $ cap reached"})
	if out.exitCode != 2 {
		t.Fatalf("codex deny must exit 2, got %d", out.exitCode)
	}
	if !strings.Contains(out.stderr, "cap reached") {
		t.Fatalf("codex deny stderr = %q", out.stderr)
	}
	if out.stdout != "" {
		t.Fatalf("codex deny should not print stdout, got %q", out.stdout)
	}
}

func TestRenderCodexInject(t *testing.T) {
	out := render("codex", "UserPromptSubmit", checkResponse{Decision: "allow", Inject: "no force-push"})
	if !strings.Contains(out.stdout, "additionalContext") || !strings.Contains(out.stdout, "force-push") {
		t.Fatalf("codex inject = %q", out.stdout)
	}
	if out.exitCode != 0 {
		t.Fatalf("codex inject must exit 0, got %d", out.exitCode)
	}
}

func TestRenderCodexAllowSilent(t *testing.T) {
	out := render("codex", "PreToolUse", checkResponse{Decision: "allow"})
	if out.stdout != "" || out.stderr != "" || out.exitCode != 0 {
		t.Fatalf("codex allow must be silent exit-0, got %+v", out)
	}
}
