# Throttle — Build Progress

Running build log. Newest entries on top.

## Status board
| Milestone | State |
|---|---|
| Setup: Go toolchain | ✅ done (Go 1.26.4, user-local `C:\Users\jagan\go-sdk`) |
| Setup: repo + layout | ✅ done |
| M1 — Live monitoring spine | ✅ done — unit + integration + sandbox smoke all green |
| M2 — Kill-switch | ⬜ |
| M3 — Rules layer | ⬜ |
| M4 — Codex adapter | ⬜ |
| M5 — Gemini + Aider | ⬜ |
| M6 — Installer | ⬜ |
| M7 — Polish + HOW-TO-TEST | ⬜ |

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
