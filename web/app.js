// Throttle dashboard: subscribe to /ws, render one row per live session.
const rows = new Map(); // sessionId -> session

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
    // Subscription: show API-equivalent $ (quota view lands in M7).
    return `<span class="cost">~$${s.cost_usd.toFixed(2)}</span> <span class="model">(plan)</span>`;
  }
  const cls = s.estimated ? "cost est" : "cost";
  const tilde = s.estimated ? "~" : "";
  return `<span class="${cls}">${tilde}$${s.cost_usd.toFixed(4)}</span>`;
}

function shortPath(p) {
  if (!p) return "—";
  return p.replace(/\\/g, "/").split("/").slice(-2).join("/");
}

function render() {
  $empty.style.display = rows.size ? "none" : "block";

  const list = [...rows.values()].sort((a, b) =>
    (b.last_seen || "").localeCompare(a.last_seen || ""));

  $rows.innerHTML = list.map((s) => {
    const t = s.tokens || {};
    const total = (t.in || 0) + (t.out || 0) + (t.cache_read || 0) + (t.cache_creation || 0);
    const cache = (t.cache_read || 0) + (t.cache_creation || 0);
    return `<tr>
      <td><span class="badge tool-${s.tool}">${s.tool}</span></td>
      <td class="path" title="${s.project_path || ""}">${shortPath(s.project_path)}</td>
      <td class="model">${s.model || "—"}</td>
      <td>${s.mode || "—"}</td>
      <td class="r">${fmtTokens(t.in)}</td>
      <td class="r">${fmtTokens(t.out)}</td>
      <td class="r">${fmtTokens(cache)}</td>
      <td class="r">${fmtTokens(total)}</td>
      <td class="r">${fmtCost(s)}</td>
      <td class="status-${s.status}">${s.status}</td>
    </tr>`;
  }).join("");

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

connect();
