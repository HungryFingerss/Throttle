package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderDenyBlocksPreToolUse(t *testing.T) {
	out := render("PreToolUse", checkResponse{Decision: "deny", Reason: "session $ cap reached: 5 of 5"})
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
	out := render("PreToolUse", checkResponse{Decision: "allow"})
	if out.stdout != "" || out.stderr != "" {
		t.Fatalf("allow must be silent, got stdout=%q stderr=%q", out.stdout, out.stderr)
	}
}

func TestRenderWarnGoesToStderr(t *testing.T) {
	out := render("PreToolUse", checkResponse{Decision: "allow", Reason: "approaching session $ cap: 90%"})
	if out.stdout != "" {
		t.Fatalf("warn must not block (no stdout), got %q", out.stdout)
	}
	if !strings.Contains(out.stderr, "approaching") {
		t.Fatalf("warn stderr = %q", out.stderr)
	}
}

func TestRenderInjectOnPrompt(t *testing.T) {
	out := render("UserPromptSubmit", checkResponse{Decision: "allow", Inject: "Remember: no force-push."})
	if !strings.Contains(out.stdout, "additionalContext") || !strings.Contains(out.stdout, "force-push") {
		t.Fatalf("inject output = %q", out.stdout)
	}
}

func TestRenderInjectOnSessionStart(t *testing.T) {
	out := render("SessionStart", checkResponse{Decision: "allow", Inject: "RULES SURVIVE COMPACTION"})
	if !strings.Contains(out.stdout, "SessionStart") || !strings.Contains(out.stdout, "SURVIVE") {
		t.Fatalf("session-start inject = %q", out.stdout)
	}
}
