package server

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <header class="topbar">
    <div>
      <h1>All Notify</h1>
      <p>聚合通知发送、配置和日志</p>
    </div>
    <nav class="tabs">
      <button data-tab="dashboard" class="active">概览</button>
      <button data-tab="routes">通知入口</button>
      <button data-tab="targets">发送目标</button>
      <button data-tab="logs">发送日志</button>
      <button data-tab="runtime">运行日志</button>
      <button data-tab="settings">设置</button>
    </nav>
  </header>

  <main>
    <section id="dashboard" class="tab active">
      <div class="metric-grid">
        <div class="metric"><span>通知入口</span><strong id="routeCount">0</strong></div>
        <div class="metric"><span>发送目标</span><strong id="targetCount">0</strong></div>
        <div class="metric"><span>最近成功</span><strong id="successCount">0</strong></div>
        <div class="metric"><span>最近失败</span><strong id="failedCount">0</strong></div>
      </div>
      <div class="panel">
        <div class="panel-title"><h2>最近发送</h2><button onclick="loadAll()">刷新</button></div>
        <div id="recentLogs"></div>
      </div>
    </section>

    <section id="routes" class="tab">
      <div class="grid two">
        <form id="routeForm" class="panel" onsubmit="saveRoute(event)">
          <div class="panel-title"><h2>通知入口</h2><button type="button" onclick="resetRouteForm()">新建</button></div>
          <input type="hidden" id="routeID">
          <label>Key<input id="routeKey" required placeholder="例如 server-alert"></label>
          <label>名称<input id="routeName" required placeholder="例如 服务器告警"></label>
          <label>默认标题<input id="routeDefaultTitle" placeholder="未传 title 时使用"></label>
          <label class="check"><input id="routeEnabled" type="checkbox" checked> 启用</label>
          <label>发送目标<select id="routeTargets" multiple size="8"></select></label>
          <button type="submit">保存入口</button>
        </form>
        <div class="panel">
          <div class="panel-title"><h2>入口列表</h2></div>
          <div id="routesTable"></div>
        </div>
      </div>
    </section>

    <section id="targets" class="tab">
      <div class="grid two">
        <form id="targetForm" class="panel" onsubmit="saveTarget(event)">
          <div class="panel-title"><h2>发送目标</h2><button type="button" onclick="resetTargetForm()">新建</button></div>
          <input type="hidden" id="targetID">
          <label>名称<input id="targetName" required placeholder="例如 我的 iPhone"></label>
          <label>类型
            <select id="targetType" onchange="fillTargetExample()">
              <option value="bark">bark</option>
              <option value="ntfy">ntfy</option>
              <option value="smtp">smtp</option>
            </select>
          </label>
          <label class="check"><input id="targetEnabled" type="checkbox" checked> 启用</label>
          <label>配置 JSON<textarea id="targetConfig" required rows="14"></textarea></label>
          <button type="submit">保存目标</button>
        </form>
        <div class="panel">
          <div class="panel-title"><h2>目标列表</h2></div>
          <div id="targetsTable"></div>
        </div>
      </div>
    </section>

    <section id="logs" class="tab">
      <div class="panel">
        <div class="toolbar">
          <label>入口 Key<input id="logRouteKey" placeholder="可选"></label>
          <label>状态
            <select id="logStatus">
              <option value="">全部</option>
              <option value="success">success</option>
              <option value="failed">failed</option>
            </select>
          </label>
          <label>数量<input id="logLimit" type="number" value="100" min="1" max="500"></label>
          <button onclick="loadLogs()">查询</button>
        </div>
        <div id="logsTable"></div>
      </div>
      <div class="panel">
        <div class="panel-title"><h2>日志详情</h2></div>
        <pre id="logDetail"></pre>
      </div>
    </section>

    <section id="runtime" class="tab">
      <div class="panel">
        <div class="panel-title"><h2>运行日志</h2><button onclick="loadRuntimeLogs()">刷新</button></div>
        <pre id="runtimeLog"></pre>
      </div>
    </section>

    <section id="settings" class="tab">
      <form class="panel narrow" onsubmit="saveSettings(event)">
        <div class="panel-title"><h2>日志裁剪</h2></div>
        <label>发送日志保留天数<input id="settingDays" type="number" min="1" required></label>
        <label>发送日志最大条数<input id="settingRows" type="number" min="1" required></label>
        <button type="submit">保存设置</button>
      </form>
    </section>
  </main>

  <div id="toast"></div>

<script>
let routes = [];
let targets = [];
let logs = [];

const examples = {
  bark: {
    server_url: "https://api.day.app",
    device_key: "your_bark_key",
    group: "all-notify",
    sound: "minuet"
  },
  ntfy: {
    server_url: "https://ntfy.sh",
    topic: "your_topic",
    priority: "default",
    tags: ["bell"]
  },
  smtp: {
    host: "smtp.example.com",
    port: 587,
    security: "starttls",
    username: "user@example.com",
    password: "password",
    from: "user@example.com",
    to: ["receiver@example.com"],
    subject_prefix: "[All Notify]"
  }
};

document.querySelectorAll(".tabs button").forEach(btn => {
  btn.addEventListener("click", () => {
    document.querySelectorAll(".tabs button,.tab").forEach(el => el.classList.remove("active"));
    btn.classList.add("active");
    document.getElementById(btn.dataset.tab).classList.add("active");
    if (btn.dataset.tab === "runtime") loadRuntimeLogs();
  });
});

async function api(path, options = {}) {
  const headers = options.headers || {};
  if (options.body && !headers["Content-Type"]) headers["Content-Type"] = "application/json";
  const res = await fetch(path, {...options, headers});
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch (_) { data = text; }
  if (!res.ok) throw new Error((data && data.error) || text || res.statusText);
  return data;
}

async function loadAll() {
  try {
    [routes, targets, logs] = await Promise.all([
      api("/api/routes"),
      api("/api/targets"),
      api("/api/logs?limit=20")
    ]);
    renderRoutes();
    renderTargets();
    renderRouteTargetOptions();
    renderLogs(logs, "recentLogs");
    renderMetrics();
    await loadSettings();
  } catch (err) {
    toast(err.message, true);
  }
}

function renderMetrics() {
  document.getElementById("routeCount").textContent = routes.length;
  document.getElementById("targetCount").textContent = targets.length;
  document.getElementById("successCount").textContent = logs.filter(l => l.status === "success").length;
  document.getElementById("failedCount").textContent = logs.filter(l => l.status === "failed").length;
}

function renderRoutes() {
  document.getElementById("routesTable").innerHTML = table(["ID", "Key", "名称", "启用", "目标", "操作"], routes.map(r => [
    r.id,
    esc(r.key),
    esc(r.name),
    r.enabled ? "是" : "否",
    (r.target_ids || []).join(", "),
    '<button onclick="testRoute(' + r.id + ')">测试</button> <button onclick="editRoute(' + r.id + ')">编辑</button> <button class="danger" onclick="deleteRoute(' + r.id + ')">删除</button>'
  ]));
}

function renderTargets() {
  document.getElementById("targetsTable").innerHTML = table(["ID", "名称", "类型", "启用", "操作"], targets.map(t => [
    t.id,
    esc(t.name),
    esc(t.type),
    t.enabled ? "是" : "否",
    '<button onclick="testTarget(' + t.id + ')">测试</button> <button onclick="editTarget(' + t.id + ')">编辑</button> <button class="danger" onclick="deleteTarget(' + t.id + ')">删除</button>'
  ]));
}

function renderRouteTargetOptions() {
  const select = document.getElementById("routeTargets");
  select.innerHTML = targets.map(t => '<option value="' + t.id + '">' + esc(t.name) + ' (' + esc(t.type) + ')</option>').join("");
}

function renderLogs(items, id) {
  document.getElementById(id).innerHTML = table(["时间", "ID", "入口", "状态", "成功/总数", "耗时", "操作"], items.map(l => [
    fmtTime(l.created_at),
    '<code>' + esc(l.id) + '</code>',
    esc(l.route_key),
    '<span class="status ' + esc(l.status) + '">' + esc(l.status) + '</span>',
    l.success_targets + '/' + l.total_targets,
    l.duration_ms + 'ms',
    '<button onclick="showLog(\'' + esc(l.id) + '\')">详情</button>'
  ]));
}

async function saveRoute(event) {
  event.preventDefault();
  const id = document.getElementById("routeID").value;
  const payload = {
    key: document.getElementById("routeKey").value.trim(),
    name: document.getElementById("routeName").value.trim(),
    default_title: document.getElementById("routeDefaultTitle").value.trim(),
    enabled: document.getElementById("routeEnabled").checked,
    target_ids: Array.from(document.getElementById("routeTargets").selectedOptions).map(o => Number(o.value))
  };
  await api(id ? "/api/routes/" + id : "/api/routes", {method: id ? "PUT" : "POST", body: JSON.stringify(payload)});
  resetRouteForm();
  await loadAll();
  toast("入口已保存");
}

function editRoute(id) {
  const route = routes.find(r => r.id === id);
  if (!route) return;
  document.getElementById("routeID").value = route.id;
  document.getElementById("routeKey").value = route.key;
  document.getElementById("routeName").value = route.name;
  document.getElementById("routeDefaultTitle").value = route.default_title || "";
  document.getElementById("routeEnabled").checked = route.enabled;
  Array.from(document.getElementById("routeTargets").options).forEach(o => {
    o.selected = (route.target_ids || []).includes(Number(o.value));
  });
}

async function deleteRoute(id) {
  if (!confirm("删除这个通知入口？")) return;
  await api("/api/routes/" + id, {method: "DELETE"});
  await loadAll();
}

async function testRoute(id) {
  const result = await apiAllowError("/api/routes/" + id + "/test", {method: "POST"});
  renderTestResult(result);
  await loadAll();
}

function resetRouteForm() {
  document.getElementById("routeForm").reset();
  document.getElementById("routeID").value = "";
  document.getElementById("routeEnabled").checked = true;
}

async function saveTarget(event) {
  event.preventDefault();
  const id = document.getElementById("targetID").value;
  let config;
  try { config = JSON.parse(document.getElementById("targetConfig").value); }
  catch (err) { toast("配置 JSON 无效", true); return; }
  const payload = {
    name: document.getElementById("targetName").value.trim(),
    type: document.getElementById("targetType").value,
    enabled: document.getElementById("targetEnabled").checked,
    config
  };
  await api(id ? "/api/targets/" + id : "/api/targets", {method: id ? "PUT" : "POST", body: JSON.stringify(payload)});
  resetTargetForm();
  await loadAll();
  toast("目标已保存");
}

function editTarget(id) {
  const target = targets.find(t => t.id === id);
  if (!target) return;
  document.getElementById("targetID").value = target.id;
  document.getElementById("targetName").value = target.name;
  document.getElementById("targetType").value = target.type;
  document.getElementById("targetEnabled").checked = target.enabled;
  try { document.getElementById("targetConfig").value = JSON.stringify(JSON.parse(target.config), null, 2); }
  catch (_) { document.getElementById("targetConfig").value = target.config; }
}

async function deleteTarget(id) {
  if (!confirm("删除这个发送目标？")) return;
  await api("/api/targets/" + id, {method: "DELETE"});
  await loadAll();
}

async function testTarget(id) {
  const result = await apiAllowError("/api/targets/" + id + "/test", {method: "POST"});
  renderTestResult(result);
  await loadAll();
}

function resetTargetForm() {
  document.getElementById("targetForm").reset();
  document.getElementById("targetID").value = "";
  document.getElementById("targetEnabled").checked = true;
  fillTargetExample();
}

function fillTargetExample() {
  const type = document.getElementById("targetType").value;
  if (!document.getElementById("targetID").value) {
    document.getElementById("targetConfig").value = JSON.stringify(examples[type], null, 2);
  }
}

async function loadLogs() {
  const params = new URLSearchParams();
  const routeKey = document.getElementById("logRouteKey").value.trim();
  const status = document.getElementById("logStatus").value;
  const limit = document.getElementById("logLimit").value;
  if (routeKey) params.set("route_key", routeKey);
  if (status) params.set("status", status);
  if (limit) params.set("limit", limit);
  const items = await api("/api/logs?" + params.toString());
  renderLogs(items, "logsTable");
}

async function showLog(id) {
  const detail = await api("/api/logs/" + id);
  document.getElementById("logDetail").textContent = JSON.stringify(detail, null, 2);
}

async function apiAllowError(path, options = {}) {
  const headers = options.headers || {};
  if (options.body && !headers["Content-Type"]) headers["Content-Type"] = "application/json";
  const res = await fetch(path, {...options, headers});
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch (_) { data = text; }
  return {ok: res.ok, status: res.status, data};
}

function renderTestResult(result) {
  document.querySelectorAll(".tabs button,.tab").forEach(el => el.classList.remove("active"));
  document.querySelector('[data-tab="logs"]').classList.add("active");
  document.getElementById("logs").classList.add("active");
  document.getElementById("logDetail").textContent = JSON.stringify(result.data, null, 2);
  const data = result.data || {};
  if (result.ok) {
    toast("测试成功: " + (data.success_targets || 0) + "/" + (data.total_targets || 0));
  } else {
    toast("测试失败: HTTP " + result.status, true);
  }
}

async function loadRuntimeLogs() {
  const data = await api("/api/runtime-logs?limit=500");
  document.getElementById("runtimeLog").textContent = (data.lines || []).join("\n");
}

async function loadSettings() {
  const settings = await api("/api/settings");
  document.getElementById("settingDays").value = settings.log_retention_days;
  document.getElementById("settingRows").value = settings.log_max_rows;
}

async function saveSettings(event) {
  event.preventDefault();
  await api("/api/settings", {
    method: "PUT",
    body: JSON.stringify({
      log_retention_days: Number(document.getElementById("settingDays").value),
      log_max_rows: Number(document.getElementById("settingRows").value)
    })
  });
  toast("设置已保存");
}

function table(headers, rows) {
  if (!rows.length) return '<div class="empty">暂无数据</div>';
  const head = headers.map(h => '<th>' + h + '</th>').join("");
  const body = rows.map(row => '<tr>' + row.map(cell => '<td>' + cell + '</td>').join("") + '</tr>').join("");
  return '<div class="table-wrap"><table><thead><tr>' + head + '</tr></thead><tbody>' + body + '</tbody></table></div>';
}

function esc(value) {
  return String(value ?? "").replace(/[&<>"']/g, ch => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[ch]));
}

function fmtTime(value) {
  if (!value) return "";
  return new Date(value).toLocaleString();
}

function toast(message, danger = false) {
  const el = document.getElementById("toast");
  el.textContent = message;
  el.className = danger ? "show danger" : "show";
  setTimeout(() => el.className = "", 2500);
}

fillTargetExample();
loadAll();
</script>
</body>
</html>`

const appCSS = `:root {
  color-scheme: light;
  font-family: Inter, "Segoe UI", Arial, sans-serif;
  color: #20252d;
  background: #f6f7f9;
}
* { box-sizing: border-box; }
body { margin: 0; min-width: 320px; }
.topbar {
  display: flex;
  justify-content: space-between;
  gap: 24px;
  align-items: center;
  padding: 18px 28px;
  background: #ffffff;
  border-bottom: 1px solid #d8dde6;
}
h1, h2, p { margin: 0; }
h1 { font-size: 22px; font-weight: 700; }
h2 { font-size: 16px; }
.topbar p { margin-top: 4px; color: #687280; font-size: 13px; }
.tabs { display: flex; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
button {
  border: 1px solid #b9c1cc;
  background: #ffffff;
  color: #20252d;
  border-radius: 6px;
  padding: 7px 11px;
  font-size: 13px;
  cursor: pointer;
}
button:hover { border-color: #61758d; }
button.active, button[type="submit"] {
  color: #ffffff;
  background: #235d8f;
  border-color: #235d8f;
}
button.danger { color: #a32626; border-color: #dfa9a9; }
main { padding: 24px 28px 40px; }
.tab { display: none; }
.tab.active { display: block; }
.grid { display: grid; gap: 18px; }
.grid.two { grid-template-columns: minmax(320px, 420px) minmax(0, 1fr); }
.metric-grid { display: grid; grid-template-columns: repeat(4, minmax(120px, 1fr)); gap: 14px; margin-bottom: 18px; }
.metric, .panel {
  background: #ffffff;
  border: 1px solid #d8dde6;
  border-radius: 8px;
}
.metric { padding: 18px; }
.metric span { display: block; color: #687280; font-size: 13px; }
.metric strong { display: block; margin-top: 8px; font-size: 28px; }
.panel { padding: 18px; }
.panel.narrow { max-width: 520px; }
.panel-title, .toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
}
.toolbar { justify-content: flex-start; flex-wrap: wrap; }
label { display: grid; gap: 6px; margin-bottom: 13px; font-size: 13px; color: #46515f; }
label.check { display: flex; align-items: center; gap: 8px; }
input, select, textarea {
  width: 100%;
  min-height: 34px;
  border: 1px solid #b9c1cc;
  border-radius: 6px;
  padding: 7px 9px;
  font: inherit;
  background: #ffffff;
  color: #20252d;
}
select[multiple] { min-height: 170px; }
textarea { font-family: "Cascadia Mono", Consolas, monospace; font-size: 12px; line-height: 1.45; }
.table-wrap { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { border-bottom: 1px solid #e4e8ee; padding: 9px 8px; text-align: left; vertical-align: top; }
th { color: #526071; font-weight: 600; background: #f8fafc; }
td code { font-size: 12px; }
.status { display: inline-block; min-width: 56px; padding: 2px 7px; border-radius: 999px; text-align: center; font-size: 12px; }
.status.success { color: #176b43; background: #e8f5ee; }
.status.failed { color: #a32626; background: #faeaea; }
.empty { color: #687280; padding: 18px 0; }
pre {
  min-height: 160px;
  max-height: 520px;
  overflow: auto;
  white-space: pre-wrap;
  background: #15191f;
  color: #eef3f8;
  border-radius: 8px;
  padding: 14px;
  font-size: 12px;
}
#toast {
  position: fixed;
  right: 18px;
  bottom: 18px;
  transform: translateY(16px);
  opacity: 0;
  background: #20252d;
  color: #ffffff;
  padding: 10px 14px;
  border-radius: 7px;
  transition: 0.18s ease;
  pointer-events: none;
}
#toast.show { opacity: 1; transform: translateY(0); }
#toast.danger { background: #9d2929; }
@media (max-width: 900px) {
  .topbar { align-items: flex-start; flex-direction: column; }
  .tabs { justify-content: flex-start; }
  .grid.two, .metric-grid { grid-template-columns: 1fr; }
  main { padding: 16px; }
}
`
