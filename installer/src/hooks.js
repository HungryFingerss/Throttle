// Claude Code hook wiring. We MERGE Throttle's hooks into the user's
// settings.json without disturbing their existing hooks, and we tag ours by the
// command (it contains "throttle-hook") so uninstall removes exactly ours.
"use strict";
const fs = require("fs");
const path = require("path");

const SENTINEL = "throttle-hook";

// Events we register and their matcher conventions:
//  - PreToolUse: tool matcher "*" (every tool call) → caps/kill-switch.
//  - UserPromptSubmit: no matcher → inject rules every turn.
//  - SessionStart: source matchers; "compact" is the compaction-proof channel,
//    "startup"/"resume" load rules at the top of a session too.
function desiredGroups(hookCmd) {
  const cmdGroup = [{ type: "command", command: hookCmd }];
  return {
    PreToolUse: [{ matcher: "*", hooks: cmdGroup }],
    UserPromptSubmit: [{ hooks: cmdGroup }],
    SessionStart: [
      { matcher: "startup", hooks: cmdGroup },
      { matcher: "resume", hooks: cmdGroup },
      { matcher: "compact", hooks: cmdGroup },
    ],
  };
}

function readJSON(file) {
  let text;
  try {
    text = fs.readFileSync(file, "utf8");
  } catch (e) {
    if (e.code === "ENOENT") return {}; // no file yet → fresh install
    throw e; // permission/IO error → surface it; never assume empty
  }
  try {
    return JSON.parse(text);
  } catch (e) {
    // The file EXISTS but isn't valid JSON. Overwriting it would wipe the user's
    // settings (permissions, env, plugins, their own hooks). Back it up and abort.
    const bak = file + ".throttle-bak";
    try { fs.copyFileSync(file, bak); } catch (_) {}
    const err = new Error(
      `Refusing to modify ${file}: it is not valid JSON (${e.message}). ` +
      `A backup was written to ${bak}. Fix the JSON, then re-run.`
    );
    err.code = "THROTTLE_UNPARSEABLE_SETTINGS";
    throw err;
  }
}

function writeJSON(file, obj) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, JSON.stringify(obj, null, 2) + "\n");
}

// groupIsThrottle reports whether a hook group belongs to Throttle.
function groupIsThrottle(group) {
  const hooks = (group && group.hooks) || [];
  return hooks.some((h) => typeof h.command === "string" && h.command.includes(SENTINEL));
}

// stripThrottle removes Throttle-owned groups from an event array, returning the
// cleaned array (may be empty).
function stripThrottle(arr) {
  if (!Array.isArray(arr)) return [];
  return arr.filter((g) => !groupIsThrottle(g));
}

// install merges Throttle hooks into the settings at settingsPath. Idempotent:
// re-running replaces Throttle's own groups, never duplicating, never touching
// the user's other hooks.
function install(settingsPath, hookCmd) {
  const settings = readJSON(settingsPath);
  if (!settings.hooks || typeof settings.hooks !== "object") settings.hooks = {};
  const want = desiredGroups(hookCmd);

  for (const [event, groups] of Object.entries(want)) {
    const existing = stripThrottle(settings.hooks[event]);
    settings.hooks[event] = existing.concat(groups);
  }
  writeJSON(settingsPath, settings);
  return settings;
}

// uninstall removes only Throttle's hook groups, dropping any event arrays that
// become empty, and removing the hooks object if it ends up empty.
function uninstall(settingsPath) {
  if (!fs.existsSync(settingsPath)) return false;
  const settings = readJSON(settingsPath);
  if (!settings.hooks) return false;

  let changed = false;
  for (const event of Object.keys(settings.hooks)) {
    const before = settings.hooks[event];
    const after = stripThrottle(before);
    if (after.length !== (Array.isArray(before) ? before.length : 0)) changed = true;
    if (after.length === 0) delete settings.hooks[event];
    else settings.hooks[event] = after;
  }
  if (Object.keys(settings.hooks).length === 0) delete settings.hooks;
  writeJSON(settingsPath, settings);
  return changed;
}

module.exports = { install, uninstall, desiredGroups, groupIsThrottle, SENTINEL };
