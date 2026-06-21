"use strict";
const { test } = require("node:test");
const assert = require("node:assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const hooks = require("../src/hooks");

function tmpSettings(initial) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "throttle-hooks-"));
  const p = path.join(dir, "settings.json");
  if (initial !== undefined) fs.writeFileSync(p, JSON.stringify(initial, null, 2));
  return p;
}

const CMD = `"C:\\Users\\me\\.throttle\\bin\\throttle-hook.exe" --tool claude`;

test("install adds all three events", () => {
  const p = tmpSettings({});
  hooks.install(p, CMD);
  const s = JSON.parse(fs.readFileSync(p, "utf8"));
  assert.ok(s.hooks.PreToolUse, "PreToolUse present");
  assert.ok(s.hooks.UserPromptSubmit, "UserPromptSubmit present");
  assert.ok(s.hooks.SessionStart.some((g) => g.matcher === "compact"), "SessionStart compact present");
  assert.ok(JSON.stringify(s.hooks).includes("throttle-hook"));
});

test("install preserves the user's existing hooks", () => {
  const p = tmpSettings({
    hooks: {
      PreToolUse: [{ matcher: "Bash", hooks: [{ type: "command", command: "my-linter" }] }],
    },
    permissions: { allow: ["Read"] },
  });
  hooks.install(p, CMD);
  const s = JSON.parse(fs.readFileSync(p, "utf8"));
  const cmds = s.hooks.PreToolUse.flatMap((g) => g.hooks.map((h) => h.command));
  assert.ok(cmds.includes("my-linter"), "user hook kept");
  assert.ok(cmds.some((c) => c.includes("throttle-hook")), "throttle hook added");
  assert.deepStrictEqual(s.permissions, { allow: ["Read"] }, "other settings untouched");
});

test("install is idempotent (no duplicate throttle groups)", () => {
  const p = tmpSettings({});
  hooks.install(p, CMD);
  hooks.install(p, CMD);
  const s = JSON.parse(fs.readFileSync(p, "utf8"));
  const throttleGroups = s.hooks.PreToolUse.filter(hooks.groupIsThrottle);
  assert.strictEqual(throttleGroups.length, 1, "exactly one throttle PreToolUse group");
});

test("uninstall removes only throttle hooks", () => {
  const p = tmpSettings({
    hooks: {
      PreToolUse: [{ matcher: "Bash", hooks: [{ type: "command", command: "my-linter" }] }],
    },
  });
  hooks.install(p, CMD);
  const changed = hooks.uninstall(p);
  assert.ok(changed, "reported a change");
  const s = JSON.parse(fs.readFileSync(p, "utf8"));
  const cmds = (s.hooks?.PreToolUse || []).flatMap((g) => g.hooks.map((h) => h.command));
  assert.ok(cmds.includes("my-linter"), "user hook survives uninstall");
  assert.ok(!cmds.some((c) => c.includes("throttle-hook")), "throttle hook gone");
  assert.ok(!s.hooks.UserPromptSubmit, "empty throttle-only event removed");
});

test("uninstall on a file with no throttle hooks is a no-op", () => {
  const p = tmpSettings({ hooks: { PreToolUse: [{ matcher: "Bash", hooks: [{ type: "command", command: "x" }] }] } });
  const changed = hooks.uninstall(p);
  assert.strictEqual(changed, false);
});

test("install refuses to clobber an unparseable settings.json (CRITICAL safety)", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "throttle-hooks-"));
  const p = path.join(dir, "settings.json");
  // A real-world hand-edit mistake: a trailing comma → invalid JSON.
  const original = '{\n  "permissions": { "allow": ["Read"] },\n}';
  fs.writeFileSync(p, original);
  assert.throws(() => hooks.install(p, CMD), /not valid JSON/, "install must abort, not overwrite");
  assert.strictEqual(fs.readFileSync(p, "utf8"), original, "the user's original settings are untouched");
  assert.ok(fs.existsSync(p + ".throttle-bak"), "a backup copy was written");
});
