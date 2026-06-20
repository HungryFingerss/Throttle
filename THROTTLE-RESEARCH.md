# Throttle — technical ground truth (research, 2026-06-20)

The local **control layer** for AI coding agents: live spend dashboard + hard caps/kill-switch
+ compaction-proof rules, across Claude Code, Codex, Gemini, Aider, OpenCode, Cline, Goose
(NOT Cursor — it has no readable local token log). Local-first, no proxy, your keys never leave.

This file is the verified mechanism reference. Anything marked **[verify]** the builder should
confirm against the tool's repo/docs or the ccusage / TokenTracker source (both open-source and
already parse these logs — use them as the format ground truth).

---

## 1. Per-tool capability matrix

| Tool | Monitor live | Cap / hard-stop | Live inject (dashboard→session) | Rule survives compaction |
|---|---|---|---|---|
| **Claude Code** | ✅ JSONL, model per-message | ✅ PreToolUse hook → `deny` | ✅ UserPromptSubmit/PostToolUse `additionalContext` | ✅✅ **guaranteed** (SessionStart `compact` hook) |
| **Codex CLI** | ✅ JSONL, model per-turn (needs dedupe + subagent filter) | ✅ PreToolUse exit 2 / Stop `continue:false` | ~ Stop-hook continuation prompt (no clean push) | ~ AGENTS.md + PreCompact hook (not native) |
| **Gemini CLI** | ✅ logs **[verify path]**; GEMINI.md sent every prompt | ~ **[verify hooks]** else process-kill | ~ file-based (GEMINI.md re-sent each prompt) | ✅ GEMINI.md is re-sent with every prompt |
| **Aider** | ✅ analytics/history logs **[verify]** | ✗ no hooks → process-kill only | ✗ none | ~ CONVENTIONS.md / read-only files (re-read) |
| **OpenCode** | ✅ **[verify]** | ~ **[verify]** | ~ **[verify]** | ~ AGENTS.md **[verify]** |
| **Cline / Goose** | ✅ **[verify]** | ~ **[verify]** | ~ **[verify]** | ~ memory files **[verify]** |

**Honest gradient:** monitoring works everywhere; **cap-enforcement is clean on Claude + Codex**
(both have a blocking pre-tool hook); **rule-persistence is guaranteed only on Claude**, good on
Gemini, partial on Codex, file-based on the rest. Lead the product on Claude+Codex; the rest are
"monitor + best-effort." Never promise a capability a tool can't back.

---

## 2. Log schemas

### Claude Code
- Path: `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl` (`$CLAUDE_CONFIG_DIR/projects` if set). Encoded-cwd = working dir with non-alphanumerics → `-`.
- JSONL, one message/event per line. Assistant messages carry `usage`: `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`, and the **model per message** (so mid-session model switch is captured natively).
- Cost = Σ over messages of `(in×in_price)+(out×out_price)+(cache_read×cache_read_price)+(cache_creation×cache_creation_price)`, priced by each message's model.

### Codex CLI
- Path: `~/.codex/sessions/YYYY/MM/DD/rollout-<id>.jsonl` (`%USERPROFILE%\.codex\sessions\...`; override `CODEX_HOME`).
- JSONL, every line `{timestamp(ISO-8601 UTC), type, payload}`. Types:
  - `session_meta` (line 1): `{id, cwd, cli_version?, model_provider?, git?, source?}`. **`source.subagent.thread_spawn.parent_thread_id` present ⇒ this is a SUBAGENT session.**
  - `turn_context`: `{model: "gpt-5.x..."}` — emitted **per turn** (track most-recent for model attribution).
  - `event_msg` w/ `payload.type=="token_count"`: `payload.info` has `total_token_usage` (cumulative) and `last_token_usage` (per-call delta), each with `input_tokens, output_tokens, cached_input_tokens, reasoning_output_tokens`. **`reasoning_output_tokens` is already inside `output_tokens` — don't double-count.**
  - `response_item` (message / function_call / function_call_output / reasoning).
  - `compacted` event w/ `replacement_history` snapshot on compaction (same file continues).
- Subscription-vs-API is **NOT** in the JSONL → read `~/.codex/auth.json`: `auth_mode=="chatgpt"` (+ refresh_token) ⇒ subscription credits; else API billing. (Can change between sessions.)
- Three format generations; pre-Aug/Sep-2025 logs have **no** token/model data.

### Gemini CLI / Aider / OpenCode / Cline / Goose
- Memory/rule files confirmed: Gemini `GEMINI.md` (hierarchical, `~/.gemini/`, **re-sent with every prompt**; `GEMINI_SYSTEM_MD` full override), Codex `AGENTS.md`, Aider `CONVENTIONS.md`/read-only files, OpenCode `AGENTS.md`.
- Exact log paths/formats + whether each records model per-event: **[verify from ccusage/TokenTracker parsers]** before coding each adapter. TokenTracker supports ~22 tools and is the broadest reference.

---

## 3. Hook / injection / persistence mechanisms

### Claude Code (strongest)
- **Cap/kill:** `PreToolUse` hook (configured in `~/.claude/settings.json`) fires before every tool call, receives `{session_id, transcript_path, ...}`. Return `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"..."}}` or exit code 2 → halts.
- **Live inject:** `UserPromptSubmit` hook → `additionalContext` (injected every turn); `PostToolUse` → `additionalContext`. Hooks can be **HTTP hooks** → POST to the local Throttle daemon, daemon returns the JSON to inject. This is the dashboard→session channel.
- **Compaction-proof rule (the key):** `SessionStart` hook with matcher **`compact`** fires right after auto-compaction → write text to stdout → re-injected into context. CLAUDE.md is NOT re-read after compaction — do not rely on it. `PreCompact`/`PostCompact` exist but don't support `additionalContext` injection.
- **Limits:** UserPromptSubmit hook timeout ~30s, SessionStart ~10min → daemon must answer fast (sub-second, local); on timeout, hook falls back to cached rule. Hooks fire at turn/tool boundaries only (not mid-tool) — a running Bash command can't be interrupted; you block the NEXT tool call.

### Codex CLI
- Hooks exist: `SessionStart, SubagentStart/Stop, PreToolUse, PostToolUse, PermissionRequest, UserPromptSubmit, PreCompact, PostCompact, Stop`, returning `additionalContext`/`systemMessage`, exit 2 to block, `continue:false` to halt.
- **Cap:** PreToolUse hook reads the rollout JSONL running total, prices it, exit 2 over budget. **Stop hook** can return a continuation prompt (closest real-time inject).
- **Persistence:** `AGENTS.md` loaded once per session (32 KiB cap), subject to compaction; post-compaction reinjection is an **open gap** (GitHub issue #19061). Workaround: PreCompact/Stop hook re-injects. `compact_prompt` can embed rules into the compaction summary. `developer_instructions` (config.toml) static.
- No documented external IPC to push an instruction mid-session (app-server daemon exists but injection API undocumented).

### Others
- Aider: no hook system → "stop" = process-kill; rules via read-only/CONVENTIONS files. Gemini: GEMINI.md re-sent every prompt (good persistence); hooks/enforcement **[verify]**. OpenCode/Cline/Goose: **[verify]**.

---

## 4. Edge cases & accounting traps (CRITICAL — these make Throttle wrong if missed)

1. **Mid-session model switch** → attribute each usage event to the most-recently-seen model (Claude: per-message; Codex: most-recent `turn_context.model`), price per-model. Handled.
2. **Codex duplicate events (~47%)** → dedupe by `requestId` or composite `(timestamp + last_token_usage)` before summing.
3. **Codex subagent overcounting (up to 91×)** → subagent files **replay the full parent history**. Exclude sessions where `session_meta.source.subagent` is present, or parent-deduct. Use only `last_token_usage` deltas.
4. **Compaction** → cumulative totals continue across it (good), but the compaction call itself is a token spike — tag it, don't alarm. Rule-persistence handled via the hooks above (per-tool strength).
5. **Clear / resume** → `/clear` or resume may start a NEW file (Codex). No "end" event in the old file; a timestamp gap is the only signal. Treat a new file as a new session.
6. **Idle / killed session** → no end marker; last line may be partial JSON. Infer "active" = file written within last N seconds; tolerate truncated final line.
7. **Huge files (700MB–2GB)** → NEVER re-parse; tail incrementally from a stored byte-offset per session (the core perf rule). Keep a running tally in a state file keyed by session-id.
8. **Reasoning tokens** (Codex) included in output_tokens — don't double-count. Per-tool token semantics differ → mirror ccusage's parsers.
9. **Cache tokens** (Claude) priced separately (cache_read, cache_creation) — include with cache prices.
10. **Subscription vs API** → not in logs; read auth files (Codex `auth.json`; Claude config). Show **$** for API, **quota/limit-remaining** for subscription. Detect per-session; auth can change between runs.
11. **Old log formats** → pre-Sep-2025 Codex has no token/model data → fallback pricing + flag `estimate`. Don't crash on unparseable lines; skip + continue (formats are "experimental").
12. **Enforcement is boundary-gated** → the cap stops the agent at the NEXT tool call, not mid-command. For an instant hard stop, process-kill. Be honest in the UI.
13. **Daemon down** → the hook must FAIL-OPEN (let the agent run) by default so Throttle crashing never bricks the user's session — but then the cap has a gap. (Decision below.)
14. **Concurrency / subagents** → each session = its own file; watch dirs recursively; one dashboard row per real (non-subagent) session.
15. **Cross-platform paths** → resolve `~/.claude`, `~/.codex`, `~/.gemini` per-OS (Windows `%USERPROFILE%`).

---

## 5. Pricing
- Pull a **maintained** price table — LiteLLM `model_prices_and_context_window.json` (per-model input/output/cache prices) — on install + weekly refresh; cache locally; fallback for unknown models (flag `estimate`). Never hand-maintain prices.
- Subscription users aren't billed per token → show **API-equivalent $** (what it would cost) AND plan-quota remaining (Claude Max/Pro & Codex Plus/Pro use rolling ~5-hour windows; limits are vendor-side — track the rolling window / **[verify exact limits]**).

---

## 6. Architecture
- **Throttle daemon (Go, resident):** fsnotify-watches each tool's log root (OS events, not polling) → new file = new session instantly; tail appends → parse only new bytes (byte-offset per session) → dedupe + exclude subagents → per-model price → running tally + status. Holds caps + rules state. Serves the dashboard over WebSocket (ms updates) and a local HTTP endpoint for the hooks.
- **Throttle hook (Go, thin):** installed into each tool's hook config. On PreToolUse/UserPromptSubmit/SessionStart-compact it POSTs session-id to the daemon → daemon replies (allow/deny + any rule text to inject) → hook acts. Fail-open if daemon unreachable.
- **Dashboard (web UI the daemon serves):** all live sessions (tool, path, model, live $/quota, status) + per-session controls (budget cap, token cap, stop, history, rules).
- **Installer (`npx throttle init`):** drop daemon+hook binaries, wire hooks into each detected tool's config, fetch price table, start daemon, open dashboard.

---

## 7. Open decisions (lock before building)
1. **Daemon-down fail mode:** fail-OPEN (recommended — don't brick the agent) vs fail-CLOSED.
2. **Caps in v1:** per-session + per-day + per-tool, in $ (API) and tokens (subscription)? per-project too?
3. **Stop semantics:** graceful (hook deny at next boundary) where hooks exist + process-kill fallback elsewhere — confirm.
4. **Rules layer in v1:** yes, with the honest per-tool gradient (guaranteed Claude, good Gemini, partial Codex, file-based rest)?
5. **Subscription depth:** show quota %/remaining (needs rolling-window tracking) vs token counts only.
6. **Tool set for first ship:** Claude + Codex + Gemini + Aider, with OpenCode/Cline/Goose as fast-follow?
