package enforce_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jagannivas/throttle/internal/adapters/claude"
	"github.com/jagannivas/throttle/internal/api"
	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/enforce"
	"github.com/jagannivas/throttle/internal/prices"
	"github.com/jagannivas/throttle/internal/tally"
)

// sonnetLine: 1000 in + 1000 out on claude-sonnet-4-6 = $0.018 each.
func sonnetLine(req string) string {
	return `{"type":"assistant","cwd":"C:\\proj","sessionId":"S","requestId":"` + req +
		`","message":{"model":"claude-sonnet-4-6","id":"` + req +
		`","usage":{"input_tokens":1000,"output_tokens":1000}}}` + "\n"
}

// makeSession stages a claude session of `lines` assistant turns ($0.018 each)
// and returns a tracker holding it plus the session id.
func makeSession(t *testing.T, id string, lines int) (*tally.Tracker, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "enc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		sb.WriteString(sonnetLine(id + "-" + itoa(i)))
	}
	file := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(file, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := tally.New(prices.Fallback(), []core.Adapter{claude.NewWithRoot(root)})
	tr.HandlePath(file)
	return tr, id
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

func check(e *enforce.Enforcer, id string) api.CheckResponse {
	return e.Check(api.CheckRequest{Tool: core.ToolClaude, SessionID: id, Event: "PreToolUse"})
}

func TestNoCapsAllows(t *testing.T) {
	tr, id := makeSession(t, "S", 1)
	e := enforce.New(tr)
	if r := check(e, id); r.Decision != api.DecisionAllow {
		t.Fatalf("decision = %q, want allow", r.Decision)
	}
}

func TestSessionDollarCapDenies(t *testing.T) {
	tr, id := makeSession(t, "S", 1) // $0.018
	e := enforce.New(tr)
	e.SetGlobalCaps(core.Caps{SessionUSD: 0.015})
	r := check(e, id)
	if r.Decision != api.DecisionDeny {
		t.Fatalf("decision = %q, want deny", r.Decision)
	}
	if !strings.Contains(r.Reason, "session $") {
		t.Fatalf("reason = %q", r.Reason)
	}
}

func TestSessionTokenCapDenies(t *testing.T) {
	tr, id := makeSession(t, "S", 1) // 2000 tokens
	e := enforce.New(tr)
	e.SetGlobalCaps(core.Caps{SessionTokens: 1500})
	if r := check(e, id); r.Decision != api.DecisionDeny {
		t.Fatalf("decision = %q, want deny", r.Decision)
	}
}

func TestWarnThreshold(t *testing.T) {
	tr, id := makeSession(t, "S", 1) // $0.018
	e := enforce.New(tr)
	e.SetGlobalCaps(core.Caps{SessionUSD: 0.02}) // 90% → warn, not deny
	r := check(e, id)
	if r.Decision != api.DecisionAllow {
		t.Fatalf("decision = %q, want allow (warn)", r.Decision)
	}
	if !strings.Contains(r.Reason, "approaching") {
		t.Fatalf("expected warn reason, got %q", r.Reason)
	}
}

func TestBelowWarnNoReason(t *testing.T) {
	tr, id := makeSession(t, "S", 1) // $0.018
	e := enforce.New(tr)
	e.SetGlobalCaps(core.Caps{SessionUSD: 0.03}) // 60% → quiet allow
	r := check(e, id)
	if r.Decision != api.DecisionAllow || r.Reason != "" {
		t.Fatalf("want quiet allow, got %q / %q", r.Decision, r.Reason)
	}
}

func TestStopFlagDenies(t *testing.T) {
	tr, id := makeSession(t, "S", 1)
	tr.SetStop(id, true)
	e := enforce.New(tr)
	r := check(e, id)
	if r.Decision != api.DecisionDeny || !strings.Contains(r.Reason, "stopped") {
		t.Fatalf("want deny/stopped, got %q / %q", r.Decision, r.Reason)
	}
}

func TestUnknownSessionFailsOpen(t *testing.T) {
	tr, _ := makeSession(t, "S", 1)
	e := enforce.New(tr)
	e.SetGlobalCaps(core.Caps{SessionUSD: 0.0001}) // tiny cap, but...
	if r := check(e, "does-not-exist"); r.Decision != api.DecisionAllow {
		t.Fatalf("unknown session must fail-open, got %q", r.Decision)
	}
}

func TestPerSessionOverrideBeatsGlobal(t *testing.T) {
	tr, id := makeSession(t, "S", 1) // $0.018
	e := enforce.New(tr)
	e.SetGlobalCaps(core.Caps{SessionUSD: 0.01}) // global would deny
	e.SetSessionCaps(id, core.Caps{SessionUSD: 1.0}) // override allows
	if r := check(e, id); r.Decision != api.DecisionAllow {
		t.Fatalf("per-session override should allow, got %q / %q", r.Decision, r.Reason)
	}
}

func TestRulesInjectedOnPrompt(t *testing.T) {
	tr, id := makeSession(t, "S", 1)
	e := enforce.New(tr)
	e.SetGlobalRules([]string{"never force-push"})

	r := e.Check(api.CheckRequest{Tool: core.ToolClaude, SessionID: id, Event: "UserPromptSubmit"})
	if r.Decision != api.DecisionAllow {
		t.Fatalf("prompt injection must allow, got %q", r.Decision)
	}
	if !strings.Contains(r.Inject, "never force-push") {
		t.Fatalf("rules not injected: %q", r.Inject)
	}
}

// The headline M3 claim: rules are re-injected after auto-compaction via the
// SessionStart:compact hook (CLAUDE.md is not re-read after compaction).
func TestRulesSurviveCompaction(t *testing.T) {
	tr, id := makeSession(t, "S", 1)
	e := enforce.New(tr)
	e.SetGlobalRules([]string{"keep the public API stable"})

	r := e.Check(api.CheckRequest{Tool: core.ToolClaude, SessionID: id, Event: "SessionStart:compact"})
	if r.Decision != api.DecisionAllow || !strings.Contains(r.Inject, "keep the public API stable") {
		t.Fatalf("rules must survive compaction: %q / %q", r.Decision, r.Inject)
	}
}

func TestOneOffMessageDeliveredOnce(t *testing.T) {
	tr, id := makeSession(t, "S", 1)
	e := enforce.New(tr)
	e.EnqueueMessage(id, "switch to the other branch")

	r1 := e.Check(api.CheckRequest{Tool: core.ToolClaude, SessionID: id, Event: "UserPromptSubmit"})
	if !strings.Contains(r1.Inject, "switch to the other branch") {
		t.Fatalf("one-off not delivered: %q", r1.Inject)
	}
	r2 := e.Check(api.CheckRequest{Tool: core.ToolClaude, SessionID: id, Event: "UserPromptSubmit"})
	if strings.Contains(r2.Inject, "switch to the other branch") {
		t.Fatalf("one-off delivered twice: %q", r2.Inject)
	}
}

func TestRuleEventsDoNotEnforceCaps(t *testing.T) {
	// Even wildly over a cap, a prompt/session-start event injects and allows
	// (caps fire at the tool-call boundary, not on prompts).
	tr, id := makeSession(t, "S", 5) // $0.09
	e := enforce.New(tr)
	e.SetGlobalCaps(core.Caps{SessionUSD: 0.01})
	r := e.Check(api.CheckRequest{Tool: core.ToolClaude, SessionID: id, Event: "UserPromptSubmit"})
	if r.Decision != api.DecisionAllow {
		t.Fatalf("prompt event must allow regardless of caps, got %q", r.Decision)
	}
}

func TestDailyCapAggregates(t *testing.T) {
	// One tracker, two sessions, $0.018 each = $0.036 today.
	root := t.TempDir()
	dir := filepath.Join(root, "enc")
	os.MkdirAll(dir, 0o755)
	tr := tally.New(prices.Fallback(), []core.Adapter{claude.NewWithRoot(root)})
	for _, id := range []string{"A", "B"} {
		f := filepath.Join(dir, id+".jsonl")
		os.WriteFile(f, []byte(strings.ReplaceAll(sonnetLine("r"), `"S"`, `"`+id+`"`)), 0o644)
		tr.HandlePath(f)
	}
	e := enforce.New(tr)
	e.SetGlobalCaps(core.Caps{DayUSD: 0.03}) // 0.036 total > 0.03

	r := e.Check(api.CheckRequest{Tool: core.ToolClaude, SessionID: "A", Event: "PreToolUse"})
	if r.Decision != api.DecisionDeny || !strings.Contains(r.Reason, "daily $") {
		t.Fatalf("want daily-cap deny, got %q / %q", r.Decision, r.Reason)
	}
}
