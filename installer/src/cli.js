// Throttle installer CLI: init / uninstall / start / stop / status / doctor.
// Everything honors the env overrides in paths.js, so it can be fully sandboxed.
"use strict";
const fs = require("fs");
const path = require("path");
const { spawn } = require("child_process");
const P = require("./paths");
const hooks = require("./hooks");
const { detect } = require("./detect");
const service = require("./service");
const { ensureBinaries } = require("./download");

// ASCII banner shown on `init` and a bare `npx throttle` (figlet "ANSI Shadow").
function banner(url) {
  const cyan = (s) => `\x1b[36m${s}\x1b[0m`;
  const art = [
    "  ████████╗██╗  ██╗██████╗  ██████╗ ████████╗████████╗██╗     ███████╗",
    "  ╚══██╔══╝██║  ██║██╔══██╗██╔═══██╗╚══██╔══╝╚══██╔══╝██║     ██╔════╝",
    "     ██║   ███████║██████╔╝██║   ██║   ██║      ██║   ██║     █████╗  ",
    "     ██║   ██╔══██║██╔══██╗██║   ██║   ██║      ██║   ██║     ██╔══╝  ",
    "     ██║   ██║  ██║██║  ██║╚██████╔╝   ██║      ██║   ███████╗███████╗",
    "     ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝    ╚═╝      ╚═╝   ╚══════╝╚══════╝",
  ].join("\n");
  const gray = (s) => `\x1b[90m${s}\x1b[0m`;
  let out = "\n" + cyan(art) + "\n\n  \x1b[1mlocal control layer for your AI coding agents\x1b[0m\n";
  if (url) {
    out += "\n  Throttle is live at " + cyan(url) + "\n";
    const cmd = (sub, desc) =>
      "    npx @hungryfingerss/throttle " + sub.padEnd(10) + gray("— " + desc);
    out += "\n  manage it anytime:\n" +
      cmd("status", "is it running? what's installed?") + "\n" +
      cmd("stop", "pause it (agents keep working, unmonitored)") + "\n" +
      cmd("start", "resume after a stop") + "\n" +
      cmd("uninstall", "remove for good (unwire hooks + stop)") + "\n";
  }
  return out;
}

function parseArgs(argv) {
  const args = { _: [], flags: {}, aiderDirs: [] };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--dry-run" || a === "--no-open" || a === "--purge" || a === "--no-start") {
      args.flags[a.slice(2)] = true;
    } else if (a === "--bin-src" || a === "--addr") {
      args.flags[a.slice(2)] = argv[++i];
    } else if (a === "--aider-dir") {
      args.aiderDirs.push(argv[++i]);
    } else {
      args._.push(a);
    }
  }
  return args;
}

function defaultBinSrc() {
  const dist = path.join(__dirname, "..", "dist", `${process.platform}-${process.arch}`);
  if (fs.existsSync(dist)) return dist;
  const repoBin = path.join(__dirname, "..", "..", "bin"); // dev fallback
  if (fs.existsSync(repoBin)) return repoBin;
  return null;
}

function copyBinaries(src, destDir) {
  fs.mkdirSync(destDir, { recursive: true });
  const copied = [];
  for (const name of [P.exe("throttled"), P.exe("throttle-hook")]) {
    const from = path.join(src, name);
    const to = path.join(destDir, name);
    if (!fs.existsSync(from)) {
      throw new Error(`missing binary: ${from}`);
    }
    fs.copyFileSync(from, to);
    if (process.platform !== "win32") fs.chmodSync(to, 0o755);
    copied.push(to);
  }
  return copied;
}

function hookCommand() {
  return `"${P.hookBinaryPath()}" --tool claude`;
}

function openBrowser(url) {
  const cmd =
    process.platform === "win32" ? "cmd" : process.platform === "darwin" ? "open" : "xdg-open";
  const args = process.platform === "win32" ? ["/c", "start", "", url] : [url];
  try {
    spawn(cmd, args, { detached: true, stdio: "ignore" }).unref();
  } catch (_) {}
}

async function cmdInit(args, log) {
  const addr = args.flags.addr || "127.0.0.1:7878";
  const dry = !!args.flags["dry-run"];
  let binSrc = args.flags["bin-src"] || defaultBinSrc();
  const tools = detect();

  log(`Throttle init`);
  log(`  state dir: ${P.throttleDir()}`);
  log(`  detected:  ${Object.entries(tools).filter(([, v]) => v).map(([k]) => k).join(", ") || "(none)"}`);

  if (dry) {
    log(`  [dry-run] would use binaries from ${binSrc || "the GitHub release"} → ${P.binDir()}`);
    if (tools.claude) log(`  [dry-run] would wire Claude hooks → ${P.claudeSettingsPath()}`);
    log(`  [dry-run] would start daemon on ${addr} and open dashboard`);
    return 0;
  }

  if (!binSrc) {
    // No local binaries → fetch the ones for this OS from the GitHub release.
    binSrc = await ensureBinaries(log);
  }
  copyBinaries(binSrc, P.binDir());
  log(`  installed binaries → ${P.binDir()}`);

  if (tools.claude) {
    hooks.install(P.claudeSettingsPath(), hookCommand());
    log(`  wired Claude hooks (PreToolUse, UserPromptSubmit, SessionStart) → ${P.claudeSettingsPath()}`);
  }
  if (tools.codex) {
    log(`  Codex detected — add this to ~/.codex hooks to enable caps/rules:`);
    log(`      command: "${P.hookBinaryPath()}" --tool codex   (events: PreToolUse, UserPromptSubmit, SessionStart)`);
  }
  if (tools.gemini) {
    log(`  Gemini detected — rules via GEMINI.md; enable telemetry (target=local) for monitoring.`);
  }
  if (args.aiderDirs.length) {
    log(`  Aider projects to watch: set THROTTLE_AIDER_DIRS=${args.aiderDirs.join(path.delimiter)}`);
  }

  if (args.flags["no-start"]) {
    log(`  [--no-start] skipping daemon launch`);
    return 0;
  }
  const res = service.start(addr);
  log(res.started ? `  daemon started (pid ${res.pid})` : `  daemon: ${res.reason}`);

  const url = `http://${addr}`;
  if (!args.flags["no-open"]) openBrowser(url);
  log(banner(url));
  return 0;
}

function cmdUninstall(args, log) {
  const changed = fs.existsSync(P.claudeSettingsPath()) ? hooks.uninstall(P.claudeSettingsPath()) : false;
  log(changed ? `  removed Claude hooks` : `  no Claude hooks to remove`);
  const res = service.stop();
  log(res.stopped ? `  daemon stopped (pid ${res.pid})` : `  daemon: ${res.reason}`);
  if (args.flags.purge) {
    try { fs.rmSync(P.binDir(), { recursive: true, force: true }); log(`  removed ${P.binDir()}`); } catch (_) {}
  }
  return 0;
}

function claudeHooksInstalled() {
  try {
    const s = JSON.parse(fs.readFileSync(P.claudeSettingsPath(), "utf8"));
    return JSON.stringify(s.hooks || {}).includes(hooks.SENTINEL);
  } catch (_) {
    return false;
  }
}

function cmdStatus(_args, log) {
  const tools = detect();
  log(`daemon: ${service.isRunning() ? `running (pid ${service.readPid()})` : "stopped"}`);
  log(`tools:  ${Object.entries(tools).map(([k, v]) => `${k}=${v ? "yes" : "no"}`).join("  ")}`);
  log(`claude hooks: ${claudeHooksInstalled() ? "installed" : "not installed"}`);
  return 0;
}

function cmdDoctor(_args, log) {
  log(`Throttle doctor`);
  log(`  home:            ${P.home()}`);
  log(`  throttle dir:    ${P.throttleDir()}`);
  log(`  bin dir:         ${P.binDir()}`);
  log(`  daemon binary:   ${fs.existsSync(P.daemonBinaryPath()) ? "present" : "MISSING"}`);
  log(`  hook binary:     ${fs.existsSync(P.hookBinaryPath()) ? "present" : "MISSING"}`);
  log(`  claude settings: ${P.claudeSettingsPath()}`);
  log(`  daemon:          ${service.isRunning() ? "running" : "stopped"}`);
  return 0;
}

function main(argv, log = console.log) {
  const args = parseArgs(argv);
  const cmd = args._[0] || "help";
  switch (cmd) {
    case "init": return cmdInit(args, log);
    case "uninstall": return cmdUninstall(args, log);
    case "start": { const r = service.start(args.flags.addr); log(r.started ? `started (pid ${r.pid})` : r.reason); return 0; }
    case "stop": { const r = service.stop(); log(r.stopped ? `stopped (pid ${r.pid})` : r.reason); return 0; }
    case "status": return cmdStatus(args, log);
    case "doctor": return cmdDoctor(args, log);
    default:
      log(banner());
      log("  usage: throttle <init|uninstall|start|stop|status|doctor> [--dry-run] [--no-open] [--bin-src dir] [--addr host:port] [--aider-dir dir] [--purge]");
      return cmd === "help" ? 0 : 1;
  }
}

module.exports = { main, parseArgs, copyBinaries, hookCommand };
