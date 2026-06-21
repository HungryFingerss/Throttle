// Throttle dashboard: subscribe to /ws, render one row per live session.
const rows = new Map(); // sessionId -> session
let caps = {};          // tool -> capabilities
const expanded = new Set(); // session ids whose subagent panel is open

// Public Web3Forms access key — routes dashboard feedback to the maker's inbox.
// It's a FORM key meant to live in client code, not a secret. Get one free at
// web3forms.com (the email you sign up with is where feedback lands).
const WEB3FORMS_KEY = "5033f398-abfc-44bb-bdca-307e8eb173e9";

function capTitle(tool) {
  const c = caps[tool];
  if (!c) return "";
  const bits = [
    `monitor: ${c.monitor_confidence || (c.monitor ? "yes" : "no")}`,
    `hard cap: ${c.hard_cap ? "yes (hook)" : "no (process-kill)"}`,
    `rules survive compaction: ${c.rules_survive_compaction ? "yes" : "no"}`,
  ];
  if (c.note) bits.push(c.note);
  return bits.join(" · ");
}

function capMark(tool) {
  const c = caps[tool];
  return c && c.monitor_confidence === "best-effort" ? "*" : "";
}

const $rows = document.getElementById("rows");
const $empty = document.getElementById("empty");
const $dot = document.getElementById("conn-dot");
const $totalCost = document.getElementById("total-cost");
const $activeCount = document.getElementById("active-count");

function fmtTokens(n) {
  if (n == null) return "0";
  if (n >= 1e9) return (n / 1e9).toFixed(2) + "B";
  if (n >= 1e6) return (n / 1e6).toFixed(2) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
  return String(n);
}

function fmtCost(s) {
  if (s.mode === "subscription") {
    // Subscription: API-equivalent $ + rolling-window quota % when known.
    const q = s.quota_used > 0
      ? ` · <span class="model">${s.quota_used.toFixed(0)}% quota</span>`
      : ` <span class="model">(plan)</span>`;
    return `<span class="cost">~$${s.cost_usd.toFixed(2)}</span>${q}`;
  }
  const cls = s.estimated ? "cost est" : "cost";
  const tilde = s.estimated ? "~" : "";
  return `<span class="${cls}">${tilde}$${s.cost_usd.toFixed(4)}</span>`;
}

function shortPath(p) {
  if (!p) return "—";
  return p.replace(/\\/g, "/").split("/").slice(-2).join("/");
}

function tokTotal(t) {
  t = t || {};
  return (t.in || 0) + (t.out || 0) + (t.cache_read || 0) + (t.cache_creation || 0);
}

// subagentPanel renders the (hidden until expanded) per-day subagent breakdown.
// These tokens/cost are already in the row's headline total; this just itemizes
// them, e.g. "2026-06-21 → sub-a5c1f0e4 · haiku · 1.2k tok · $0.0040".
function subagentPanel(s) {
  const subs = s.subagents || [];
  if (!subs.length || !expanded.has(s.id)) return "";
  const byDay = {};
  for (const x of subs) (byDay[x.day || "—"] ||= []).push(x);
  const sub = s.mode === "subscription";
  const body = Object.keys(byDay).sort().reverse().map((day) => {
    const items = byDay[day]
      .slice().sort((a, b) => (b.cost_usd || 0) - (a.cost_usd || 0))
      .map((x) => {
        const kind = x.compact ? "compact" : "sub";
        const id = (x.id || "").replace(/^acompact-/, "").slice(0, 8);
        const cost = `${sub ? "~" : ""}$${(x.cost_usd || 0).toFixed(4)}`;
        return `<div class="subitem">
          <span class="subid">${kind}-${id}</span>
          <span class="submodel">${x.model || "—"}</span>
          <span class="subtok">${fmtTokens(tokTotal(x.tokens))} tok</span>
          <span class="subcost">${cost}</span>
        </div>`;
      }).join("");
    return `<div class="subday"><div class="subday-h">${day}</div>${items}</div>`;
  }).join("");
  return `<tr class="subrow"><td colspan="11"><div class="subpanel">${body}</div></td></tr>`;
}

function render() {
  $empty.style.display = rows.size ? "none" : "block";

  const list = [...rows.values()].sort((a, b) =>
    (b.last_seen || "").localeCompare(a.last_seen || ""));

  $rows.innerHTML = list.map((s) => {
    const t = s.tokens || {};
    const total = (t.in || 0) + (t.out || 0) + (t.cache_read || 0) + (t.cache_creation || 0);
    const cache = (t.cache_read || 0) + (t.cache_creation || 0);
    const nsub = (s.subagents || []).length;
    const toggle = nsub
      ? `<button class="subtoggle" data-id="${s.id}">${expanded.has(s.id) ? "▾" : "▸"} ${nsub} sub${nsub === 1 ? "" : "s"}</button>`
      : "";
    return `<tr>
      <td><span class="badge tool-${s.tool}" title="${capTitle(s.tool)}">${s.tool}${capMark(s.tool)}</span></td>
      <td class="path" title="${s.project_path || ""}">${shortPath(s.project_path)} ${toggle}</td>
      <td class="model">${s.model || "—"}</td>
      <td>${s.mode || "—"}</td>
      <td class="r">${fmtTokens(t.in)}</td>
      <td class="r">${fmtTokens(t.out)}</td>
      <td class="r">${fmtTokens(cache)}</td>
      <td class="r">${fmtTokens(total)}</td>
      <td class="r">${fmtCost(s)}</td>
      <td class="status-${s.status}">${s.status}</td>
      <td class="actions">
        <button class="btn msg" data-id="${s.id}">Msg</button>
        ${s.stop_flag
          ? `<button class="btn resume" data-id="${s.id}" data-stop="0">Resume</button>`
          : `<button class="btn stop" data-id="${s.id}" data-stop="1">Stop</button>`}
      </td>
    </tr>${subagentPanel(s)}`;
  }).join("");

  for (const b of $rows.querySelectorAll(".subtoggle")) {
    b.addEventListener("click", () => {
      const id = b.dataset.id;
      expanded.has(id) ? expanded.delete(id) : expanded.add(id);
      render();
    });
  }

  for (const b of $rows.querySelectorAll(".btn.stop, .btn.resume")) {
    b.addEventListener("click", () => stopSession(b.dataset.id, b.dataset.stop === "1"));
  }
  for (const b of $rows.querySelectorAll(".btn.msg")) {
    b.addEventListener("click", () => {
      const m = prompt("Send a one-off message to this session (delivered on its next prompt):");
      if (m) sendMessage(b.dataset.id, m);
    });
  }

  let cost = 0, active = 0;
  for (const s of rows.values()) {
    if (s.mode !== "subscription") cost += s.cost_usd || 0;
    if (s.status === "active") active++;
  }
  $totalCost.textContent = "$" + cost.toFixed(2);
  $activeCount.textContent = String(active);
}

function applyUpdate(u) {
  const s = u.payload;
  if (!s || !s.id) return;
  if (u.type === "session_end") rows.delete(s.id);
  else rows.set(s.id, s);
  render();
}

async function stopSession(id, stop) {
  try {
    await fetch("/api/stop", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session_id: id, stop }),
    });
  } catch (_) {}
}

async function sendMessage(id, message) {
  try {
    await fetch("/api/message", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session_id: id, message }),
    });
  } catch (_) {}
}

async function loadRules() {
  try {
    const r = await fetch("/api/rules");
    const v = await r.json();
    if (v && Array.isArray(v.global)) {
      document.getElementById("rules-text").value = v.global.join("\n");
    }
  } catch (_) {}
}

async function saveRules() {
  const lines = document.getElementById("rules-text").value
    .split("\n").map((s) => s.trim()).filter(Boolean);
  try {
    await fetch("/api/rules", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ scope: "global", rules: lines }),
    });
    const st = document.getElementById("rules-status");
    st.textContent = `${lines.length} rule${lines.length === 1 ? "" : "s"} saved`;
    setTimeout(() => (st.textContent = ""), 2500);
  } catch (_) {}
}

document.getElementById("save-rules").addEventListener("click", saveRules);

async function setDayCap() {
  const v = parseFloat(document.getElementById("day-cap").value);
  const caps = { day_usd: isNaN(v) ? 0 : v };
  try {
    await fetch("/api/caps", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ scope: "global", caps }),
    });
    const st = document.getElementById("cap-status");
    st.textContent = caps.day_usd ? `cap $${caps.day_usd.toFixed(2)}/day` : "no cap";
    setTimeout(() => (st.textContent = ""), 2500);
  } catch (_) {}
}

document.getElementById("set-day-cap").addEventListener("click", setDayCap);

function connect() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/ws`);

  ws.onopen = () => $dot.classList.add("live");
  ws.onclose = () => {
    $dot.classList.remove("live");
    setTimeout(connect, 1500); // auto-reconnect
  };
  ws.onmessage = (ev) => {
    try { applyUpdate(JSON.parse(ev.data)); } catch (_) {}
  };
}

async function loadCaps() {
  try {
    caps = await (await fetch("/api/capabilities")).json();
  } catch (_) {}
}

function initFeedback() {
  const toggle = document.getElementById("fb-toggle");
  const panel = document.getElementById("fb-panel");
  const send = document.getElementById("fb-send");
  const st = document.getElementById("fb-status");
  if (!toggle || !panel || !send) return;
  toggle.addEventListener("click", () => { panel.hidden = !panel.hidden; });
  send.addEventListener("click", async () => {
    const msg = document.getElementById("fb-text").value.trim();
    if (!msg) { st.textContent = "write something first"; return; }
    if (WEB3FORMS_KEY.startsWith("REPLACE_")) { st.textContent = "feedback key not configured"; return; }
    const email = document.getElementById("fb-email").value.trim();
    st.textContent = "sending…";
    try {
      const r = await fetch("https://api.web3forms.com/submit", {
        method: "POST",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify({
          access_key: WEB3FORMS_KEY,
          subject: "Throttle feedback",
          from_name: "Throttle dashboard",
          replyto: email || undefined,
          message: msg + (email ? `\n\n— from: ${email}` : ""),
        }),
      });
      const j = await r.json().catch(() => ({}));
      if (j.success) {
        st.textContent = "thanks — sent ✓";
        document.getElementById("fb-text").value = "";
        setTimeout(() => { st.textContent = ""; panel.hidden = true; }, 2500);
      } else {
        st.textContent = "couldn't send" + (j.message ? ` (${j.message})` : "");
      }
    } catch (_) {
      st.textContent = "couldn't send — check your connection";
    }
  });
}

loadCaps();
loadRules();
initFeedback();
connect();
