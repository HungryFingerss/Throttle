package api

import "github.com/jagannivas/throttle/internal/core"

// CheckRequest is the hook→daemon payload (PLAN §6).
type CheckRequest struct {
	Tool           core.ToolKind `json:"tool"`
	SessionID      string        `json:"session_id"`
	Event          string        `json:"event"` // PreToolUse | UserPromptSubmit | SessionStart | Stop ...
	TranscriptPath string        `json:"transcript_path"`
}

// CheckDecision is allow or deny.
type CheckDecision string

const (
	DecisionAllow CheckDecision = "allow"
	DecisionDeny  CheckDecision = "deny"
)

// CheckResponse is the daemon→hook reply. Inject carries rule/context text the
// hook should add to the session (M3).
type CheckResponse struct {
	Decision CheckDecision `json:"decision"`
	Reason   string        `json:"reason,omitempty"`
	Inject   string        `json:"inject,omitempty"`
}

// Checker decides whether a session may proceed and what to inject. The daemon
// supplies the real implementation (enforcement) in M2/M3; M1 uses AllowAll.
type Checker interface {
	Check(CheckRequest) CheckResponse
}

// AllowAll is the M1 no-op checker: it never blocks (fail-open by construction).
type AllowAll struct{}

func (AllowAll) Check(CheckRequest) CheckResponse {
	return CheckResponse{Decision: DecisionAllow}
}
