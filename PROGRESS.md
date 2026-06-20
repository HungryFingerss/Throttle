# Throttle — Build Progress

Running build log. Newest entries on top.

## Status board
| Milestone | State |
|---|---|
| Setup: Go toolchain | ✅ done (Go 1.26.4, user-local `C:\Users\jagan\go-sdk`) |
| Setup: repo + layout | ✅ done |
| M1 — Live monitoring spine | ✅ done — unit + integration + sandbox smoke all green |
| M2 — Kill-switch | ✅ done — cap enforce + real hook binary, sandbox E2E (deny/allow/fail-open) green |
| M3 — Rules layer | ✅ done — rules inject every turn + survive compaction; sandbox E2E green |
| M4 — Codex adapter | ✅ done — dedup + subagent-exclude + per-turn model + subscription detect; real-log validated; sandbox E2E green |
| M5 — Gemini + Aider | ✅ done — Aider cost-from-file, Gemini OTel multi-session demux; honest capability flags; sandbox E2E green |
| M6 — Installer | ✅ done — npx throttle init/uninstall, hook wiring (surgical), daemon service, 6-target cross-build; node tests + sandbox E2E green |
| M7 — Polish + HOW-TO-TEST | ✅ done — subscription quota (Codex rate_limits), /api/summary, HOW-TO-TEST.md, README, one-command test-all; **ALL GREEN** |

**PRODUCT COMPLETE** — every Definition-of-Done item met (see bottom). `scripts\test-all.ps1` → ALL GREEN (Go + Node + 6 sandboxed milestone smokes).

---

## Verified schemas (against REAL logs on this machine, 2026-06-20)

### Claude Code — `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`
Verified against `~/.claude/projects/C--Users-jagan-dog-ai/781515e8-….jsonl` (191,062 lines).
- One JSON object per line; top-level `type` ∈ {assistant, user, system, attachment, progress, file-history-snapshot, …}.
- Assistant line top-level keys: `parentUuid, isSidechain, message, requestId, type, uuid, timestamp, userType, cwd, sessionId, version, gitBranch`.
- **`isSidechain: true` ⇒ subagent/sidechain line — EXCLUDE from top-level accounting.** Subagent transcripts also live in nested `…/<session>/subagents/…` dirs.
- **`cwd`** = real project path (use this for the dashboard path column; do NOT reverse-decode the lossy folder name).
- **`message.model`** per-message (e.g. `claude-sonnet-4-6`) → per-message model attribution.
- **Dedup key** = `message.id` + `requestId` (ccusage approach).
- **`message.usage`** = `{input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens, cache_creation:{ephemeral_5m_input_tokens, ephemeral_1h_input_tokens}, service_tier}`.
  - Cost = in·in_price + out·out_price + cache_read·cache_read_price + cache_creation·cache_creation_price, priced by that message's model. (5m vs 1h cache-write split available for later refinement.)

### Codex CLI — `~/.codex/sessions/YYYY/MM/DD/rollout-<id>.jsonl`
Verified against 7 real rollouts + `~/.codex/auth.json`.
- Every line `{timestamp, type, payload}`.
- Top-level `type` ∈ {session_meta, turn_context, event_msg, response_item, compacted, …}.
- `session_meta.payload`: `{id, timestamp, cwd, originator, cli_version, source, model_provider, base_instructions, git}`.
  - **`source`** observed as the string `"cli"` on ALL 7 real files → no real subagent session present on this machine. Per research, a SUBAGENT session has `source` as an **object** carrying `subagent.thread_spawn.parent_thread_id`. Adapter handles `source` as string OR object; subagent ⇒ object with `subagent`. Synthetic subagent fixture used for the 91× exclusion test (documented, no real one available here).
  - **`cwd`** = real project path.
- `turn_context.payload.model` (e.g. `gpt-5.5`) — per-turn; track most recent for attribution.
- `event_msg` w/ `payload.type=="token_count"`: `payload.info` =
  `{total_token_usage{input_tokens, cached_input_tokens, output_tokens, reasoning_output_tokens, total_tokens}, last_token_usage{…}, model_context_window}`.
  - **Verified**: `input_tokens` INCLUDES `cached_input_tokens` (12109 ⊇ 10624); `total = input + output` (12115 = 12109+6). `reasoning_output_tokens` ⊆ `output_tokens`.
  - Pricing: uncached_input = input−cached priced at input rate; cached priced at cache_read rate; output at output rate (reasoning already inside).
  - `info` can be `null` (rate-limit-only token_count events) — skip those.
  - Accounting: sum dedup'd `last_token_usage` deltas attributed to current `turn_context.model`; cross-check vs final `total_token_usage`.
  - **Dedup**: composite `(timestamp + last_token_usage)` (research: ~47% dup rate).
- **Subscription vs API**: `~/.codex/auth.json` → `auth_mode=="chatgpt"` ⇒ subscription (this machine IS subscription); `OPENAI_API_KEY` set / `auth_mode!="chatgpt"` ⇒ API. Can change between sessions.

---

## Decisions / deviations log
- **Fixtures**: committed fixtures in `testdata/` are schema-faithful with synthetic/redacted text content (real logs contain the user's private prompts/code — not committed). Parser correctness verified against the REAL files listed above; real raw slices kept only in gitignored `testdata/real-captures/`. This honors "parse the real schema, don't invent it" while protecting private content.
- **No real Codex subagent log** exists on this machine (all `source:"cli"`). The 91× subagent-exclusion test uses a synthetic fixture built to the documented schema.

---

## M7 — Polish + HOW-TO-TEST — DONE (2026-06-21)
- **Subscription quota view**: Codex `rate_limits.primary` (`used_percent`, `window_minutes`, `resets_at`) — VERIFIED from a real rollout — surfaced as `QuotaUsed`/`QuotaRemaining`; dashboard shows "N% quota" for subscription sessions. Parsed even on rate-limit-only (`info:null`) token_count events.
- **`/api/summary`**: rolls live sessions up by tool and by model (cost + tokens + count) — the lightweight "where's the spend" view.
- **Deterministic-pricing flag** (`THROTTLE_NO_PRICE_REFRESH`): pins the daemon to the embedded fallback table so sandbox tests assert exact dollars; real installs still fetch live LiteLLM. (This was the cause of the only smoke flake — live opus-4-8 priced differently than the fallback; not a product bug.)
- **`HOW-TO-TEST.md`**: step-by-step sandbox verification of every capability, never touching real config. **`README.md`**: overview + honest capability matrix.
- **`scripts/test-all.ps1`**: one command runs Go tests + Node installer tests + all 6 sandboxed smokes, each in an isolated child shell. → **ALL GREEN.**
- Removed transient debug scripts; kept schema-verification tools (`inspect-*`, `scan-*`) as provenance.

### Definition of Done — all met
- ✅ Builds on Windows + cross-compiles (win/mac/linux × amd64/arm64, 12 binaries).
- ✅ Adapters pass tests against real fixtures (Claude+Codex real-schema; Codex validated against a real on-machine log; Gemini/Aider doc-derived, labeled).
- ✅ Dashboard shows live sessions ms-accurately (fsnotify→tracker→WebSocket E2E).
- ✅ Caps stop real runs (PreToolUse deny; sandbox E2E).
- ✅ Rules survive compaction on Claude (SessionStart:compact re-injection; sandbox E2E).
- ✅ Installer wires + unwires cleanly (surgical hooks; sandbox E2E).
- ✅ Fail-open verified (daemon down → hook exits 0 silent).
- ✅ Whole thing E2E-tested in a sandbox **without ever touching the user's real agent config**.
- ✅ `HOW-TO-TEST.md` written.

## M6 — Installer + packaging — DONE (2026-06-20)
`npx throttle` CLI (Node) that wires everything and reverses cleanly. Writes to real configs, so it is fully sandbox-aware and was tested ONLY against temp dirs.
- **`installer/`**: `bin/throttle.js` + `src/{paths,detect,hooks,service,cli}.js`. Commands: `init`, `uninstall`, `start`, `stop`, `status`, `doctor`.
- **Hook wiring is surgical**: Claude hooks merged into `settings.json` (PreToolUse `*`, UserPromptSubmit, SessionStart `startup|resume|compact`) WITHOUT touching the user's existing hooks; Throttle's entries are tagged by the `throttle-hook` command so `uninstall` removes exactly ours and drops emptied arrays. Idempotent.
- **Daemon service**: detached background process + pid file (no elevation, trivially reversible). Codex/Gemini/Aider: detected and the exact wiring/notes are printed (Codex TOML auto-edit deferred — safer to instruct than to mangle config we can't fully verify).
- **Cross-build** (`scripts/build-binaries.ps1`): `throttled` + `throttle-hook` for win32/darwin/linux × x64/arm64 (Node-convention dir names matching the installer's `dist/<platform>-<arch>` lookup), CGO disabled. All 6 targets built clean.
- **Tests**: 7 Node tests (`installer/test`) — hooks merge/preserve/idempotent/uninstall + CLI init/uninstall/dry-run against a sandbox home using real binaries.
- **Sandbox E2E** (`scripts/smoke-m6.ps1`): real `node throttle.js init` (using the cross-built win32-x64 binaries) detected claude+codex, installed binaries, wired hooks, started the daemon (dashboard reachable); `uninstall` removed hooks + stopped the daemon; verified down + clean. PASS. Never touched real config.

## M5 — Gemini + Aider adapters — DONE (2026-06-20)
Honest "monitor + best-effort" per the research's capability gradient. Formats VERIFIED from official docs + the gemini-cli usage-analyzer (no real Gemini/Aider token logs exist on this machine — Aider absent, Gemini telemetry off-by-default — so fixtures are documentation-derived, clearly labeled; not invented).
- **`internal/adapters/aider`**: parses `.aider.chat.history.md` lines (`Tokens: 2.8k sent, 27 received. Cost: $0.0029 message …`). Takes per-message tokens AND the dollar cost directly from the file (new `UsageEvent.CostOverride` — no pricing guesswork). No central log → projects opt in via `THROTTLE_AIDER_DIRS`. No hooks → process-kill; rules via CONVENTIONS.md.
- **`internal/adapters/gemini`**: streams the OTel `telemetry.log` (concatenated JSON, parsed with `json.Decoder`; truncated-tail safe). One file holds MANY sessions → events carry `SessionID` and the tracker demuxes. Strong capability is rules via GEMINI.md (re-sent every prompt → survives compaction). Monitoring needs telemetry enabled (off by default).
- **Architecture**: tracker now tracks byte offsets **per file** and routes events to sessions by `UsageEvent.SessionID` (empty → file's primary session). Multi-session files never spawn an empty placeholder row. Single-session behavior (Claude/Codex/Aider) unchanged — all prior tests still green.
- **Capabilities**: every adapter reports `core.Capabilities` (monitor confidence, hard-cap, live-inject, rules-survive-compaction, stop mechanism, honesty note). New `/api/capabilities` endpoint; dashboard shows a per-tool tooltip + a `*` on best-effort tools. Never promises a capability a tool can't back.
- **Tests**: aider (parse, cost-from-file, full accounting, SessionFileID), gemini (parse, multi-session demux, truncated-tail, no-placeholder).
- **Sandbox E2E** (`scripts/smoke-m5.ps1`, full sandbox via USERPROFILE redirect): `/api/capabilities` lists all 4 tools with correct gradient; Gemini telemetry demuxed into sess-A ($0.0066875) + sess-B ($0.0031) with no placeholder; Aider history priced $0.0129. PASS.

## M4 — Codex adapter — DONE (2026-06-20)
The hardest accounting, all traps handled and tested.
- **`internal/adapters/codex`**: incremental rollout parser. Normalizes Codex token semantics (input_tokens INCLUDES cached → Input = input−cached, CacheRead = cached; reasoning_output_tokens recorded as Reasoning, already inside Output). Per-turn model from `turn_context.model` (most recent), with the tracker's last-known-model fallback covering incremental/restart gaps. Dedup key = timestamp + last_token_usage. `token_count.info==null` skipped. `compacted` event tags the next token_count.
- **Subagent exclusion (91× trap)**: `session_meta.source` as an OBJECT (vs string `"cli"`) marks a subagent → excluded from totals AND rows. Test proves a ~2M-token subagent replay leaks nothing.
- **Subscription detection**: `~/.codex/auth.json` → `auth_mode=="chatgpt"` ⇒ subscription (this machine), OPENAI_API_KEY/apikey ⇒ API.
- **Hook**: `cmd/throttle-hook` now tool-aware — Codex deny → exit code 2; inject → `additionalContext` JSON (Codex inject schema [verify]; exit-2 cap is reliable).
- **Real-log validation**: env-gated `TestRealCodexLogSanity` parsed an actual rollout on this machine (56 events, 5.88M tokens, no crash, non-negative). Real logs are NOT committed (privacy).
- **Tests**: adapter (parse, dedup keys, normalization, compaction tag, subagent meta, SessionFileID, DetectMode), full accounting + subagent exclusion via the tracker, hook render matrix (Claude + Codex).
- **Sandbox E2E** (`scripts/smoke-m4.ps1`): daemon discovered a Codex rollout in a sandbox `CODEX_HOME`, mode=subscription, model=gpt-5-mini, cost=$0.00322125; subagent dropped in → still 1 row, no token leak. PASS.

## M3 — Rules/control layer — DONE (2026-06-20)
Persistent rules injected every turn and re-injected after compaction (Claude's guaranteed channel), plus live one-off operator messages.
- **`internal/rules`**: rule store (global ▷ tool ▷ session merge) + per-session one-off message queue + `InjectText` renderer (numbered, clearly delimited block).
- **Enforcer** now also injects: `UserPromptSubmit` → rules + drained one-offs; `SessionStart[:compact]` → rules (re-injection that survives compaction). Injection works even for sessions the tracker hasn't seen yet (uses tool+session from the request). Tool-call events still run caps.
- **API**: `/api/rules` (GET/POST global|tool|session) + `/api/message` (enqueue one-off).
- **Dashboard**: global-rules textarea (one per line) + per-row "Msg" button (one-off send).
- **Tests**: rules (merge order, isolation, one-off drain, inject format/empty); enforce (inject on prompt, **survive compaction via SessionStart:compact**, one-off delivered once, rule events never block on caps).
- **Sandbox E2E** (`scripts/smoke-m3.ps1`): real hook — rules injected on `UserPromptSubmit`, re-injected on `SessionStart:compact`, one-off delivered exactly once. PASS.

## M2 — Kill-switch — DONE (2026-06-20)
Hard caps that stop a run at the next tool boundary, plus stop/resume, warn threshold, fail-open.
- **`internal/enforce`**: cap evaluator (the `api.Checker`). Resolves effective caps per session (per-session ▷ per-tool ▷ global, field-by-field), checks session $/tokens + daily $/tokens, denies at/over cap, warns at ≥80%, honors manual stop flag. **Unknown session → allow (fail-open by construction).**
- **`cmd/throttle-hook`**: thin hook binary. Reads tool hook JSON from stdin, POSTs `/v1/check` (1.5s timeout), translates to Claude's native output: deny → `permissionDecision:"deny"`, warn → stderr note, inject → `additionalContext`. **Any daemon trouble → exit 0 silent (fail-open).** Translation logic is a pure `render()` func, unit-tested across the matrix.
- **API**: added `/api/caps` (GET/POST global|tool|session) and `/api/stop`; enforcer wired as the daemon's Checker + Controls.
- **Dashboard**: global daily-$ cap input + per-row Stop/Resume buttons.
- **Tests**: enforce (deny on session/token/daily cap, warn band, quiet allow, stop-flag deny, unknown-session fail-open, per-session override beats global, daily aggregation); hook render matrix.
- **Sandbox E2E** (`scripts/smoke-m2.ps1`): real hook ↔ real daemon — over-cap emits deny JSON, raised cap → silent allow, daemon killed → fail-open (silent, exit 0). All PASS.
- **Test-harness note**: PowerShell's string-pipe to a native exe's stdin does NOT deliver; feed hooks via `cmd /c "hook < file"` (Claude Code uses a real stdin pipe, equivalent to the redirect). Documented in `scripts/smoke-m2.ps1`.

## M1 — Live monitoring spine — DONE (2026-06-20)
Daemon discovers sessions via OS events, tracks live spend ms-accurately, serves a live dashboard.
- **Packages**: `internal/core` (domain types + Adapter contract + token normalization), `internal/prices` (LiteLLM-shaped table, embedded offline fallback + live overlay), `internal/adapters/claude` (incremental JSONL parser), `internal/tally` (dedup + per-model pricing + subagent fold + idle), `internal/watch` (fsnotify, per-dir recursive, no polling), `internal/store` (atomic JSON state for offset resume), `internal/api` (HTTP `/v1/check` + REST + WebSocket hub), `web` (vanilla dashboard, embedded), `cmd/throttled`.
- **Added a `core` package** beyond PLAN §5 layout to hold shared domain types and the Adapter interface (avoids import cycles between adapters/tally/api). Deviation noted.
- **Token normalization** (in `core` doc): Input/CacheRead/CacheCreation/Output are disjoint & additive; Reasoning is informational (inside Output), never priced. Each adapter absorbs its tool's quirks before emitting `core.Tokens`.
- **Dedup lives in `tally`** (not the adapter) so it works across incremental passes; adapter is a pure stateless parser emitting events with a DedupKey.
- **Subagent decision**: subagent sessions are excluded from totals AND rows (the safe reading of "fold into parent, avoid 91× overcount"); Claude `isSidechain` lines ARE counted toward their parent session (no replay problem on Claude).
- **Tests**: prices (exact + fuzzy + overlay), claude adapter (dedup, sidechain, mid-session model switch, truncated-tail tolerance, incremental offset, malformed lines, SessionFileID excludes nested subagents), tally (full accounting: exact tokens+cost across model switch, idempotent re-read, idle sweep), api (live spine E2E: fsnotify→tracker→WebSocket, REST snapshot).
- **Sandbox smoke** (`scripts/smoke-m1.ps1`): real `throttled.exe` in a throwaway `CLAUDE_CONFIG_DIR`+`THROTTLE_DIR` discovered a session, priced 0.018→0.108 with opus attribution on append; live LiteLLM fetch loaded **1397 models**. Never touched real config.
- Daemon binds `127.0.0.1` only, reads logs read-only, writes only to `THROTTLE_DIR`, fails open.

## Setup — DONE
- Go 1.26.4 installed user-local at `C:\Users\jagan\go-sdk` (no admin; winget unavailable). User PATH + `GOTOOLCHAIN=local` set.
- Repo layout per PLAN.md §5 created.
