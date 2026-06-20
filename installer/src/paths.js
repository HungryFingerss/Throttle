// Path resolution for the installer — mirrors the daemon's config package and
// honors the same env overrides so tests (and power users) can fully sandbox.
"use strict";
const os = require("os");
const path = require("path");

function home() {
  // THROTTLE_FAKE_HOME lets tests redirect every tool root at once.
  return (
    process.env.THROTTLE_FAKE_HOME ||
    process.env.USERPROFILE ||
    process.env.HOME ||
    os.homedir()
  );
}

function claudeConfigDir() {
  return process.env.CLAUDE_CONFIG_DIR || path.join(home(), ".claude");
}
function claudeSettingsPath() {
  return path.join(claudeConfigDir(), "settings.json");
}
function codexHome() {
  return process.env.CODEX_HOME || path.join(home(), ".codex");
}
function codexConfigPath() {
  return path.join(codexHome(), "config.toml");
}
function geminiDir() {
  return path.join(home(), ".gemini");
}
function throttleDir() {
  return process.env.THROTTLE_DIR || path.join(home(), ".throttle");
}
function binDir() {
  return path.join(throttleDir(), "bin");
}
function pidFile() {
  return path.join(throttleDir(), "daemon.pid");
}

// hookBinaryName / daemonBinaryName account for the .exe suffix on Windows.
function exe(name) {
  return process.platform === "win32" ? name + ".exe" : name;
}
function hookBinaryPath() {
  return path.join(binDir(), exe("throttle-hook"));
}
function daemonBinaryPath() {
  return path.join(binDir(), exe("throttled"));
}

module.exports = {
  home,
  claudeConfigDir,
  claudeSettingsPath,
  codexHome,
  codexConfigPath,
  geminiDir,
  throttleDir,
  binDir,
  pidFile,
  exe,
  hookBinaryPath,
  daemonBinaryPath,
};
