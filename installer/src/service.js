// Daemon lifecycle: start the resident throttled process detached, track its
// pid, and stop it cleanly. Kept deliberately simple (a detached background
// process + pid file) rather than an OS service manager, so install/uninstall
// never require elevation and are trivially reversible.
"use strict";
const fs = require("fs");
const path = require("path");
const { spawn } = require("child_process");
const P = require("./paths");

function isAlive(pid) {
  if (!pid) return false;
  try {
    process.kill(pid, 0); // signal 0 = existence check
    return true;
  } catch (e) {
    return e.code === "EPERM"; // exists but not ours
  }
}

function readPid() {
  try {
    const pid = parseInt(fs.readFileSync(P.pidFile(), "utf8").trim(), 10);
    return Number.isFinite(pid) ? pid : 0;
  } catch (_) {
    return 0;
  }
}

function isRunning() {
  return isAlive(readPid());
}

function start(addr) {
  if (isRunning()) return { started: false, pid: readPid(), reason: "already running" };

  const bin = P.daemonBinaryPath();
  if (!fs.existsSync(bin)) {
    throw new Error("daemon binary not found at " + bin + " (run with --bin-src or build first)");
  }
  fs.mkdirSync(P.throttleDir(), { recursive: true });
  const logPath = path.join(P.throttleDir(), "daemon.log");
  const out = fs.openSync(logPath, "a");

  const args = [];
  if (addr) args.push("--addr", addr);
  const child = spawn(bin, args, {
    detached: true,
    stdio: ["ignore", out, out],
  });
  child.unref();
  fs.writeFileSync(P.pidFile(), String(child.pid));
  return { started: true, pid: child.pid, log: logPath };
}

function stop() {
  const pid = readPid();
  if (!isAlive(pid)) {
    try { fs.unlinkSync(P.pidFile()); } catch (_) {}
    return { stopped: false, reason: "not running" };
  }
  try {
    process.kill(pid);
  } catch (_) {}
  try { fs.unlinkSync(P.pidFile()); } catch (_) {}
  return { stopped: true, pid };
}

module.exports = { start, stop, isRunning, readPid };
