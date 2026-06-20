# Throttle — Build Plan

## 0. Read first
This file + **`THROTTLE-RESEARCH.md`** (same folder) are the complete spec.
- `THROTTLE-RESEARCH.md` = the **verified mechanisms**: exact log paths/schemas, hook contracts, accounting traps, the per-tool capability matrix. It is ground truth — re-read the relevant section before building each tool adapter. Where it says **[verify]**, confirm against the tool's docs or the open-source **ccusage** / **TokenTracker** parsers (they already parse these logs) before coding.
- This file = **what to build, how it's wired, in what order, and how it's tested.**

## 1. What Throttle is
A **local, no-proxy control layer** for AI coding agents. One resident app + a live dashboard that:
- discovers **every** agent session across the machine in real time,
- shows **live token spend** ($ for API users, quota-remaining for subscription users),
- enforces **hard budget / token caps** that actually stop the run,
- lets you **stop** any session,
- injects **persistent rules that survive context compaction**.

Tools (first ship): **Claude Code, Codex, Gemini, Aider.** Fast-follow: OpenCode, Cline, Goose.
**Not Cursor** (no readable local token log). Keys never leave the machine; no proxy.

## 2. Locked decisions
1. **Daemon down ⇒ FAIL-OPEN.** The hook must let the agent run if it can't reach the daemon. Throttle never bricks the user's work.
2. **Caps:** per-session + per-day + per-tool, in **dollars** (API) AND **tokens** (subscription). Per-project = nice-to-have.
3. **Stop:** graceful hook-deny at the next boundary where a tool has hooks (Claude, Codex); **process-kill** fallback where it doesn't (Aider).
4. **Rules-control layer is IN**, with the honest per-tool gradient (guaranteed Claude, good Gemini, partial Codex, file-based rest).
5. **Subscription = full quota view** (rolling-window tracking), not just raw token counts.
6. **First tool set:** Claude + Codex + Gemini + Aider; the rest fast-follow.

## 3. Stack
- **Daemon + hook binary: Go** — single static binaries, cross-platform, fast. Pure-Go deps (no cgo): `fsnotify`, `modernc.org/sqlite`, a WebSocket lib (`nhooyr.io/websocket` or gorilla).
- **Dashboard:** a lean static web UI served by the daemon, live via WebSocket. No heavy framework.
- **Installer/distribution:** an npm package (`npx throttle`) shipping the prebuilt Go binaries per-OS; does `init` / `uninstall`. Binaries cross-compiled in CI for darwin/linux/windows × amd64/arm64.
- **Build + test on Windows first** (the dev machine); keep ALL paths/logic cross-platform.

## 4. Architecture
- **Daemon (`throttled`, resident):** fsnotify-watches each tool's log root → discovers sessions instantly; tails appends → parses only new bytes → dedupes + excludes subagents → attributes per-model → prices → updates live tally. Holds caps + rules + state (SQLite). Serves: a **local HTTP API** for the hooks and a **WebSocket** for the dashboard.
- **Hook binary (`throttle-hook`, thin):** installed into each tool's hook config. On a hook event it POSTs the daemon and obeys the reply (allow / deny / inject text). **Fail-open** if the daemon is unreachable.
- **Dashboard (web UI):** live session table + controls; talks to the daemon over WebSocket/REST.
- **Installer (npm):** detect tools → write hooks → fetch price table → install+start the daemon service → open dashboard; `uninstall` reverses it.

Wiring: `fsnotify → daemon` (discovery/tracking), `hook ↔ daemon` (localhost HTTP, enforcement+injection), `daemon ↔ dashboard` (WebSocket, live UI + controls).

## 5. Repo layout
```
throttle/
  cmd/throttled/         daemon main
  cmd/throttle-hook/     thin hook binary
  internal/
    watch/               fsnotify watchers per tool root
    adapters/            per-tool: claude/ codex/ gemini/ aider/ (log parse + hook-config writer + capability flags)
    tally/               byte-offset reader, dedup, subagent-exclude, model attribution, pricing
    prices/              fetch + cache LiteLLM price table
    store/               sqlite: live state + history
    api/                 http (hooks) + websocket (dashboard)
    enforce/             caps + stop + rules logic
    config/
  web/                   dashboard static assets
  installer/             npm package (node): detect, write hooks, service install, open dashboard, uninstall
  testdata/              REAL captured log fixtures for parser tests
  PROGRESS.md            running build log (you maintain this)
  HOW-TO-TEST.md         you write this at the end for the user
```

## 6. Contracts
- **Hook → daemon (localhost HTTP):** `POST /v1/check {tool, session_id, event, transcript_path}` → `{decision: "allow"|"deny", reason, inject?: "<text to add to context>"}`. The hook translates `decision`/`inject` into the tool's native form (Claude: `permissionDecision:"deny"` or exit 2, `additionalContext`; Codex: exit 2 / Stop `continue:false`).
- **Daemon → dashboard (WebSocket):** `{type: "session_new"|"session_update"|"session_end"|"alert", payload: <session>}`. **Dashboard → daemon:** set-cap, set-rule, stop, get-history (REST or WS).
- **Session model:** `{id, tool, project_path, model, mode: api|subscription, tokens:{in,out,cache_read,cache_creation,reasoning}, cost_usd, quota_used, quota_remaining, caps:{session_usd,session_tokens,day_usd,...}, rules:[], status: active|idle|stopped, stop_flag, byte_offset, started_at, last_seen, is_subagent, parent_id}`.

## 7. Runtime flow (condensed; full detail in THROTTLE-RESEARCH.md §4–6)
Install wires hooks + starts daemon + opens dashboard → daemon watches log roots → **new log file = new session row instantly** (read `session_meta`: tool, path, model, api/subscription via auth file, subagent?) → **on each append**: seek to stored byte-offset, read new bytes, track current model, dedupe, price the delta, update tally, push to dashboard → **before each tool call** the hook asks the daemon → over cap ⇒ deny ⇒ run halts at that boundary (warn at 80%; fail-open if daemon down) → **rules** injected every turn via `UserPromptSubmit` and re-injected after compaction via `SessionStart`-`compact` (Claude) → **stop** = deny-next-boundary or process-kill → idle session (no writes in N s) = ended, kept in history.

## 8. Correctness must-haves (NON-NEGOTIABLE — test each)
- **MACHINE-WIDE discovery + path display (core requirement):** the daemon watches each tool's **central per-user log root** (`~/.claude/projects/`, `~/.codex/sessions/`, `~/.gemini/…`, etc.) — honoring overrides like `CLAUDE_CONFIG_DIR` / `CODEX_HOME`. Because the tools centralize ALL sessions there (the launch directory is only encoded into the path/metadata), this captures **every session from every directory on the machine automatically** — no full-filesystem scan, and independent of where Throttle was installed or started. The dashboard MUST show each session's **real project path** as a column, extracted from session content/metadata (Codex `session_meta.cwd`, Claude transcript `cwd`), NOT by reverse-decoding Claude's lossy encoded folder name (non-alphanumerics → `-`, which mangles paths with `_`/spaces). One daemon per OS user covers that user's whole machine; other OS users are out of scope.
- **Incremental byte-offset reads** (never re-parse; survives 2GB logs; enables ms updates).
- **Codex dedup** (~47% duplicate events — key on `requestId` / `(timestamp+last_token_usage)`).
- **Subagent exclusion** (`session_meta.source.subagent` present ⇒ fold into parent, never a top-level row; avoids the 91× overcount).
- **Per-turn model attribution** (most-recent `turn_context.model` / per-message model; correct across mid-session switches).
- **Subscription vs API detection** (read auth files, not the session log; show $ vs quota).
- **Fail-open everywhere.**
- **Tolerate** truncated final lines, unknown/old log formats (skip + flag estimate, never crash), and tag compaction token-spikes.
- **Cross-platform paths** (`~/.claude`, `%USERPROFILE%\.codex`, etc.).
- Reasoning tokens already inside output_tokens — don't double-count; cache tokens priced separately.

## 9. Build order (construction sequence — the goal is the COMPLETE product; this is just the order)
- **M1 — Live monitoring spine:** daemon + Claude adapter (parse `~/.claude/projects/**.jsonl`) + fsnotify watcher + tally + pricing + WebSocket + minimal live dashboard. Proves ms-accurate discovery + live cost.
- **M2 — The kill-switch:** Claude `PreToolUse` hook + cap enforcement (session/day/tool, $/tokens) + warn threshold + fail-open.
- **M3 — Rules/control layer:** rule store + `UserPromptSubmit` injection + `SessionStart`-`compact` re-injection + dashboard rule controls + live one-off message.
- **M4 — Codex adapter:** parse `~/.codex/sessions/**` (with dedup + subagent exclude + per-turn model) + Codex hooks (cap + rules) + auth-mode detection.
- **M5 — Gemini + Aider adapters:** monitor + rules via memory files; enforcement where the tool allows, process-kill fallback for Aider.
- **M6 — Installer + packaging:** `npx throttle init/uninstall`, tool detection, hook writing, service install, price fetch, open dashboard; cross-OS binary builds.
- **M7 — Polish:** subscription quota view, history views (day/week/project/model), settings, then OpenCode/Cline/Goose fast-follow.
Each milestone builds + passes tests before the next.

## 10. Testing
- **Unit:** each adapter against **REAL** captured log fixtures in `testdata/` — assert token totals, cost, dedup, subagent exclusion, mid-session model switch, subscription-vs-API.
- **Integration:** daemon end-to-end with simulated appends → assert dashboard messages + cap firing + injection.
- **On-machine E2E — SANDBOXED ONLY:** run against a **throwaway config** (separate `CLAUDE_CONFIG_DIR` / temp project / test settings file). **Never touch the user's real `~/.claude/settings.json` or interfere with live sessions.** Verify: new session shows instantly; live cost tracks; a $ cap actually stops a Claude run; a rule survives `/compact`; killing the daemon mid-session leaves the agent working (fail-open).
- Write **`HOW-TO-TEST.md`** so the user can run the sandbox E2E themselves before any launch.

## 11. Definition of done
Full product builds on Windows and cross-compiles; all adapters pass tests against real fixtures; the dashboard shows live sessions ms-accurately; caps stop real runs; rules survive compaction on Claude; installer wires + unwires cleanly; fail-open verified; and the whole thing has been E2E-tested in a sandbox on this machine **without ever touching the user's real agent config.**
