# Throttle

```text
████████╗██╗  ██╗██████╗  ██████╗ ████████╗████████╗██╗     ███████╗
╚══██╔══╝██║  ██║██╔══██╗██╔═══██╗╚══██╔══╝╚══██╔══╝██║     ██╔════╝
   ██║   ███████║██████╔╝██║   ██║   ██║      ██║   ██║     █████╗
   ██║   ██╔══██║██╔══██╗██║   ██║   ██║      ██║   ██║     ██╔══╝
   ██║   ██║  ██║██║  ██║╚██████╔╝   ██║      ██║   ███████╗███████╗
   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝    ╚═╝      ╚═╝   ╚══════╝╚══════╝
```

A local control layer for AI coding agents. One resident daemon plus a live
dashboard that watches every agent session on your machine, shows what each one
is spending in real time, stops a run when it hits a budget, and injects rules
that survive context compaction. No proxy, no keys leaving the machine.

Works with **Claude Code, Codex, Gemini CLI, and Aider**. Not Cursor — it keeps
no readable local token log.

## Why

When you hand work to an agent, you lose the feedback a human dev has: what it's
spending, whether it's drifting from your instructions, when to stop it. Throttle
puts that back — read‑only, local, and out of the way until you need it.

## What it does

| | Claude Code | Codex | Gemini CLI | Aider |
|---|---|---|---|---|
| Live spend ($ / quota) | exact | exact | best‑effort¹ | best‑effort² |
| Hard cap that stops a run | ✅ hook | ✅ hook | process‑kill | process‑kill |
| Rules injected every turn | ✅ | ✅ | ✅ (GEMINI.md) | file (CONVENTIONS.md) |
| Rules survive compaction | ✅ guaranteed | partial | ✅ | re‑read |

¹ Gemini token usage comes from its OpenTelemetry log, which is off by default.
² Aider has no central log; point Throttle at projects with `THROTTLE_AIDER_DIRS`.

The dashboard never claims a capability a tool can't back — hover any tool badge.

## How it works

- **Daemon (`throttled`, Go).** `fsnotify`‑watches each tool's per‑user log root,
  so it discovers every session from every directory automatically — no
  filesystem scan. It tails appends, reads only the new bytes from a stored
  byte offset (survives multi‑GB logs), dedupes, excludes subagent replays,
  attributes each turn to its model, prices it from a maintained table
  (LiteLLM, cached, weekly refresh), and pushes updates to the dashboard over
  WebSocket. It serves a localhost HTTP endpoint for the hooks.
- **Hook (`throttle-hook`, Go).** Installed into each tool's hook config. Before
  a tool call it asks the daemon allow/deny; on a prompt or after compaction it
  asks for rule text to inject. **If the daemon is unreachable it allows the
  agent to proceed** — Throttle never blocks your work.
- **Installer (`npx throttle`, Node).** Detects your tools, wires the hooks
  (surgically — it never disturbs your existing hooks), starts the daemon, opens
  the dashboard. `npx throttle uninstall` reverses all of it.

## Accounting correctness

The hard parts, each tested against captured/real‑schema fixtures:
incremental byte‑offset reads, Codex duplicate‑event dedup, Codex replay‑subagent
exclusion, Claude subagent spend folded into the parent total and itemized
per‑day in the dashboard, per‑turn model attribution across
mid‑session switches, subscription‑vs‑API detection from auth files, separate
cache‑token pricing, reasoning tokens not double‑counted, and tolerance for
truncated/old‑format log lines.

## Install

```bash
npx @hungryfingerss/throttle init       # detect tools, wire hooks, start daemon, open dashboard
npx @hungryfingerss/throttle status
npx @hungryfingerss/throttle uninstall  # remove hooks, stop daemon
```

Or install the `throttle` command globally:

```bash
npm install -g @hungryfingerss/throttle
throttle init
```

## Privacy

Throttle reads your local logs and writes only to its own state dir — your
prompts, code, and keys never leave your machine. The only thing that ever
leaves is the dashboard's **Feedback** button, and only when you click Send.

## Build from source

```bash
# daemon + hook for this OS
go build -o bin/throttled    ./cmd/throttled
go build -o bin/throttle-hook ./cmd/throttle-hook
# all platforms (into installer/dist/<platform>-<arch>/)
pwsh scripts/build-binaries.ps1
```

## Verify it safely

See **[HOW-TO-TEST.md](HOW-TO-TEST.md)** — every check runs in a throwaway
sandbox and never touches your real agent config. `scripts/test-all.ps1` runs
the whole suite (Go + Node + sandboxed end‑to‑end smokes).

## Layout

```
cmd/throttled/      daemon          cmd/throttle-hook/  thin hook binary
internal/adapters/  per-tool parsers (claude, codex, gemini, aider)
internal/tally/     dedup + pricing + model attribution + subagent exclusion
internal/watch/     fsnotify        internal/enforce/   caps + rules + kill-switch
internal/api/       HTTP + WebSocket internal/prices/   LiteLLM price table
web/                dashboard       installer/          npm package (Node)
```

## Status

v0.1 — Claude + Codex are the lead tools (exact monitoring, blocking caps);
Gemini + Aider are monitor + best‑effort. OpenCode / Cline / Goose are
fast‑follow.
