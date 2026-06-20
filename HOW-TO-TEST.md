# Throttle — How to test it safely

This guide verifies Throttle end‑to‑end **without ever touching your real agent
config**. Everything below runs against a throwaway sandbox (a temp `HOME`, a
temp `CLAUDE_CONFIG_DIR`/`CODEX_HOME`, a temp `THROTTLE_DIR`). Your real
`~/.claude/settings.json`, `~/.codex/config.toml`, and live sessions are never
read‑modified or hooked.

> Safety contract: the daemon only **reads** tool logs and only **writes** to
> its own state dir. The installer is the only thing that writes hooks, and in
> every step below it is pointed at a sandbox. Throttle also **fails open** — if
> the daemon is down, your agent runs normally.

---

## 0. Prerequisites

- **Go 1.26+** (to build) and **Node 18+** (for the installer).
- Windows, macOS, or Linux. Examples use PowerShell (the repo's dev shell); the
  logic is identical on bash.

Build the binaries once:

```powershell
# from the repo root
& scripts\build-binaries.ps1     # cross-builds all 6 OS/arch targets into installer/dist/
# or just the current OS:
& scripts\go.ps1 build -o bin/throttled.exe ./cmd/throttled
& scripts\go.ps1 build -o bin/throttle-hook.exe ./cmd/throttle-hook
```

---

## 1. Run the whole automated suite (fastest confidence)

```powershell
& scripts\test-all.ps1
```

This runs: all Go unit/integration tests, the Node installer tests, and every
sandboxed milestone smoke (M1 live monitoring, M2 caps/kill‑switch + fail‑open,
M3 rules + compaction, M4 Codex, M5 Gemini/Aider, M6 install/uninstall). Each
smoke spins up the real daemon in a temp sandbox and tears it down.

If that all passes, the rest of this doc is how to verify each capability by
hand.

---

## 2. Live monitoring (M1) — a session appears instantly with live cost

```powershell
$sb = Join-Path $env:TEMP ("throttle-try-" + (Get-Random))
$env:CLAUDE_CONFIG_DIR = Join-Path $sb ".claude"
$env:THROTTLE_DIR      = Join-Path $sb ".throttle"
New-Item -ItemType Directory -Force -Path (Join-Path $env:CLAUDE_CONFIG_DIR "projects\C--demo") | Out-Null

bin\throttled.exe --addr 127.0.0.1:7878
```

Open <http://127.0.0.1:7878>. In another shell, simulate an assistant turn:

```powershell
$f = Join-Path $env:CLAUDE_CONFIG_DIR "projects\C--demo\s1.jsonl"
Set-Content $f -Encoding ascii -Value '{"type":"assistant","cwd":"C:\\demo","sessionId":"s1","requestId":"r1","message":{"model":"claude-sonnet-4-6","id":"r1","usage":{"input_tokens":1000,"output_tokens":1000}}}'
```

A row appears within ~ms showing project `C:\demo`, model, tokens, and **$0.018**.
Append another line and the cost updates live. (This is exactly what a real
Claude Code session writes — Throttle just reads it.)

---

## 3. The kill‑switch (M2) — a $ cap actually stops the run

With the daemon from step 2 running and the `s1` session present:

```powershell
# set a cap below the current spend
Invoke-RestMethod "http://127.0.0.1:7878/api/caps" -Method Post -ContentType application/json `
  -Body '{"scope":"global","caps":{"session_usd":0.01}}'

# simulate Claude's PreToolUse hook firing (Claude feeds the hook via stdin):
'{"session_id":"s1","hook_event_name":"PreToolUse","tool_name":"Bash"}' > $env:TEMP\h.json
cmd /c "bin\throttle-hook.exe --tool claude --addr 127.0.0.1:7878 < $env:TEMP\h.json"
```

You'll see a **deny** decision:
`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny",...}}`
— in a real session Claude blocks the next tool call. Raise the cap
(`session_usd: 100`) and the same hook call returns nothing (allow).

**Fail‑open:** stop the daemon (Ctrl‑C) and run the hook again — it prints
nothing and exits 0, so your agent keeps working even though Throttle is down.

---

## 4. Rules survive compaction (M3) — the headline capability

```powershell
Invoke-RestMethod "http://127.0.0.1:7878/api/rules" -Method Post -ContentType application/json `
  -Body '{"scope":"global","rules":["Never force-push to main"]}'

# UserPromptSubmit — rules injected every turn:
'{"session_id":"s1","hook_event_name":"UserPromptSubmit"}' > $env:TEMP\p.json
cmd /c "bin\throttle-hook.exe --tool claude --addr 127.0.0.1:7878 < $env:TEMP\p.json"

# SessionStart:compact — re-injected AFTER auto-compaction (CLAUDE.md is not):
'{"session_id":"s1","hook_event_name":"SessionStart","source":"compact"}' > $env:TEMP\c.json
cmd /c "bin\throttle-hook.exe --tool claude --addr 127.0.0.1:7878 < $env:TEMP\c.json"
```

Both print `additionalContext` containing your rule. The second proves the rule
is re‑injected on compaction — the guarantee CLAUDE.md cannot make.

Send a one‑off message from the dashboard ("Msg" button) or:
`POST /api/message {"session_id":"s1","message":"switch to staging"}` — it is
delivered on the session's next prompt, exactly once.

---

## 5. Codex (M4) — dedup, subagent exclusion, subscription quota

```powershell
$env:CODEX_HOME = Join-Path $sb ".codex"
$root = Join-Path $env:CODEX_HOME "sessions\2026\06\20"
New-Item -ItemType Directory -Force -Path $root | Out-Null
Set-Content (Join-Path $env:CODEX_HOME "auth.json") -Encoding ascii -Value '{"auth_mode":"chatgpt"}'
Copy-Item testdata\codex_session.jsonl  (Join-Path $root "rollout-2026-06-20T10-00-00-00000000-0000-0000-0000-000000000001.jsonl")
Copy-Item testdata\codex_subagent.jsonl (Join-Path $root "rollout-2026-06-20T11-00-00-00000000-0000-0000-0000-000000000002.jsonl")
```

Restart the daemon (so it picks up `CODEX_HOME`). The dashboard shows **one**
Codex row (mode **subscription**), priced **$0.00322125**, model `gpt-5-mini` —
and the ~2M‑token subagent rollout is correctly excluded (no extra row, no token
leak).

---

## 6. Gemini / Aider (M5) — monitor + best‑effort

These are the honest "monitor + best‑effort" tier (hover the tool badge for what
each can/can't do). Gemini needs telemetry enabled; Aider has no central log so
you opt projects in:

```powershell
# Gemini: drop a telemetry log under ~/.gemini (sandbox HOME)
Copy-Item testdata\gemini_telemetry.log (Join-Path $sb ".gemini\telemetry.log")
# Aider: point Throttle at a project that has .aider.chat.history.md
$env:THROTTLE_AIDER_DIRS = "C:\path\to\your\aider\project"
```

Gemini's single telemetry file demuxes into one row per `session.id`; Aider's
cost comes straight from its history file.

---

## 7. Install / uninstall against a sandbox (M6)

```powershell
$env:USERPROFILE = $sb; $env:HOME = $sb
node installer\bin\throttle.js init --no-open --bin-src installer\dist\win32-x64 --addr 127.0.0.1:7878
node installer\bin\throttle.js status      # daemon running, hooks installed
node installer\bin\throttle.js uninstall   # removes ONLY Throttle's hooks, stops daemon
```

`init` merges Throttle's hooks into the sandbox `settings.json` without touching
any other hooks; `uninstall` removes exactly Throttle's entries.

> To wire your **real** Claude later, run `npx throttle init` with no sandbox env
> vars set. It is reversible at any time with `npx throttle uninstall`.

---

## 8. Cleanup

```powershell
Remove-Item Env:CLAUDE_CONFIG_DIR, Env:CODEX_HOME, Env:THROTTLE_DIR, Env:USERPROFILE, Env:HOME, Env:THROTTLE_AIDER_DIRS -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force $sb
```

Nothing in your real config was ever touched.
