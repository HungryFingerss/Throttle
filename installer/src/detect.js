// Tool detection: which agent tools are present for this user.
"use strict";
const fs = require("fs");
const P = require("./paths");

function detect() {
  return {
    claude: fs.existsSync(P.claudeConfigDir()),
    codex: fs.existsSync(P.codexHome()),
    gemini: fs.existsSync(P.geminiDir()),
    // Aider has no central config dir; it is wired per-project via
    // THROTTLE_AIDER_DIRS, so we report it as opt-in rather than auto-detected.
    aider: false,
  };
}

module.exports = { detect };
