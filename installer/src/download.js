// Fetches the prebuilt throttled + throttle-hook binaries for the current OS from
// the GitHub Release matching this package's version. Binaries are NOT bundled in
// the npm package (per-platform + large) — they're downloaded on first `init`.
// Use `--bin-src <dir>` to point at locally-built binaries instead (dev).
"use strict";
const fs = require("fs");
const path = require("path");
const https = require("https");
const P = require("./paths");

const REPO = "HungryFingerss/Throttle";

// Node platform/arch → the Go GOOS/GOARCH used in the release asset names.
function target() {
  const goos = { win32: "windows", darwin: "darwin", linux: "linux" }[process.platform];
  const goarch = { x64: "amd64", arm64: "arm64" }[process.arch];
  return { goos, goarch, ext: process.platform === "win32" ? ".exe" : "" };
}

function assetName(bin, t) {
  return `${bin}-${t.goos}-${t.goarch}${t.ext}`;
}

// download follows redirects (GitHub release assets 302 to a CDN) and writes to dest.
function download(url, dest, redirects = 0) {
  return new Promise((resolve, reject) => {
    if (redirects > 5) return reject(new Error("too many redirects"));
    https
      .get(url, { headers: { "User-Agent": "throttle-installer", Accept: "application/octet-stream" } }, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          res.resume();
          return resolve(download(res.headers.location, dest, redirects + 1));
        }
        if (res.statusCode !== 200) {
          res.resume();
          return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
        }
        const file = fs.createWriteStream(dest);
        res.pipe(file);
        file.on("finish", () => file.close(() => resolve()));
        file.on("error", (e) => { try { fs.rmSync(dest, { force: true }); } catch (_) {} reject(e); });
      })
      .on("error", reject);
  });
}

// ensureBinaries makes sure both binaries for this OS/arch exist under
// dist/<platform>-<arch>/, downloading them from the matching GitHub Release if
// missing. Returns that directory — a valid --bin-src for copyBinaries().
async function ensureBinaries(version, log = () => {}) {
  const t = target();
  if (!t.goos || !t.goarch) {
    throw new Error(`unsupported platform ${process.platform}/${process.arch} — build from source`);
  }
  const dir = path.join(__dirname, "..", "dist", `${process.platform}-${process.arch}`);
  const have = ["throttled", "throttle-hook"].every((b) => fs.existsSync(path.join(dir, P.exe(b))));
  if (have) return dir;

  fs.mkdirSync(dir, { recursive: true });
  for (const bin of ["throttled", "throttle-hook"]) {
    const url = `https://github.com/${REPO}/releases/download/v${version}/${assetName(bin, t)}`;
    const dest = path.join(dir, P.exe(bin));
    log(`  downloading ${bin} for ${t.goos}/${t.goarch} …`);
    await download(url, dest);
    if (process.platform !== "win32") fs.chmodSync(dest, 0o755);
  }
  return dir;
}

module.exports = { ensureBinaries, assetName, target, REPO };
