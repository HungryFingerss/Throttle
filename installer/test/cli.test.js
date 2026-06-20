"use strict";
const { test } = require("node:test");
const assert = require("node:assert");
const fs = require("fs");
const os = require("os");
const path = require("path");

const repoBin = path.join(__dirname, "..", "..", "bin");
const haveBinaries =
  fs.existsSync(path.join(repoBin, process.platform === "win32" ? "throttle-hook.exe" : "throttle-hook"));

function sandboxHome() {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "throttle-cli-"));
  fs.mkdirSync(path.join(home, ".claude"), { recursive: true }); // so detect() sees claude
  return home;
}

function withHome(home, fn) {
  const prev = process.env.THROTTLE_FAKE_HOME;
  process.env.THROTTLE_FAKE_HOME = home;
  // src modules read env at call time, but cache nothing — fresh require is fine.
  delete require.cache[require.resolve("../src/cli")];
  delete require.cache[require.resolve("../src/paths")];
  try {
    return fn(require("../src/cli"));
  } finally {
    if (prev === undefined) delete process.env.THROTTLE_FAKE_HOME;
    else process.env.THROTTLE_FAKE_HOME = prev;
  }
}

test("init wires hooks + installs binaries; uninstall reverses it", { skip: !haveBinaries ? "repo binaries not built" : false }, () => {
  const home = sandboxHome();
  const logs = [];
  const log = (m) => logs.push(m);

  withHome(home, (cli) => {
    const code = cli.main(["init", "--no-start", "--no-open", "--bin-src", repoBin], log);
    assert.strictEqual(code, 0);

    // binaries copied
    const hookBin = path.join(home, ".throttle", "bin", process.platform === "win32" ? "throttle-hook.exe" : "throttle-hook");
    assert.ok(fs.existsSync(hookBin), "hook binary installed");

    // claude hooks written
    const settings = JSON.parse(fs.readFileSync(path.join(home, ".claude", "settings.json"), "utf8"));
    assert.ok(JSON.stringify(settings.hooks).includes("throttle-hook"), "claude hooks wired");

    // uninstall removes hooks
    const code2 = cli.main(["uninstall"], log);
    assert.strictEqual(code2, 0);
    const after = JSON.parse(fs.readFileSync(path.join(home, ".claude", "settings.json"), "utf8"));
    assert.ok(!JSON.stringify(after.hooks || {}).includes("throttle-hook"), "hooks removed");
  });
});

test("dry-run writes nothing", () => {
  const home = sandboxHome();
  withHome(home, (cli) => {
    const code = cli.main(["init", "--dry-run", "--no-open", "--bin-src", repoBin], () => {});
    assert.strictEqual(code, 0);
    assert.ok(!fs.existsSync(path.join(home, ".throttle", "bin")), "dry-run created no bin dir");
    const settingsPath = path.join(home, ".claude", "settings.json");
    assert.ok(!fs.existsSync(settingsPath), "dry-run wrote no settings");
  });
});
