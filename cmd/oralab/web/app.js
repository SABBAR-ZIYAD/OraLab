(() => {
  "use strict";

  const csrf = document.querySelector('meta[name="oralab-csrf"]').content;
  const $ = (id) => document.getElementById(id);
  const elements = {
    overall: $("overall-status"), notice: $("notice"), host: $("host-value"), docker: $("docker-value"),
    oracle: $("oracle-value"), database: $("database-value"), image: $("image-value"), service: $("service-value"), refresh: $("refresh-button"),
    start: $("start-button"), stop: $("stop-button"), restart: $("restart-button"),
    cpu: $("cpu-limit"), memory: $("memory-limit"), cpuUsage: $("cpu-usage"), memoryUsage: $("memory-usage"), storage: $("storage-list"),
    logs: $("logs-value"), logsButton: $("logs-button")
  };
  let currentState = null;
  let busy = false;

  const label = (value) => String(value || "unknown").replaceAll("-", " ");
  const formatBytes = (bytes) => bytes ? `${(bytes / 1073741824).toFixed(bytes >= 10737418240 ? 0 : 1)} GiB` : "Not configured";

  function setNotice(message, success = false) {
    elements.notice.hidden = !message;
    elements.notice.textContent = message || "";
    elements.notice.classList.toggle("success", success);
  }

  function updateButtons() {
    const manageable = Boolean(currentState?.docker?.reachable && currentState?.docker?.local_context && currentState?.oracle?.service);
    const running = Boolean(currentState?.oracle?.running);
    elements.start.disabled = busy || !manageable || running;
    elements.stop.disabled = busy || !manageable || !running;
    elements.restart.disabled = busy || !manageable || !running;
    elements.refresh.disabled = busy;
    elements.logsButton.disabled = busy || !running;
  }

  function render(state) {
    currentState = state;
    const status = elements.overall.querySelector("span:last-child");
    elements.overall.className = "status-chip neutral";
    if (!state.docker.reachable) { status.textContent = "Compose unavailable"; elements.overall.classList.add("bad"); }
    else if (state.oracle.readiness === "ready") { status.textContent = "Oracle ready"; elements.overall.classList.add("good"); }
    else if (state.oracle.running) { status.textContent = "Oracle initializing"; elements.overall.classList.add("warn"); }
    else { status.textContent = "Oracle stopped"; }
    elements.host.textContent = `${state.host.os} · ${state.host.architecture}`;
    elements.docker.textContent = state.docker.reachable ? (state.docker.context || "Docker Compose") : (state.docker.error || "Unavailable");
    elements.oracle.textContent = state.oracle.error || label(state.oracle.status);
    elements.database.textContent = label(state.oracle.readiness);
    elements.image.textContent = state.oracle.image || "Unavailable";
    elements.service.textContent = state.oracle.pdb || "ORCLPDB1";
    elements.cpu.textContent = state.resources.configured_cpus ? `${state.resources.configured_cpus} CPUs` : "Not configured";
    elements.memory.textContent = formatBytes(state.resources.configured_memory_bytes);
    elements.cpuUsage.textContent = state.resources.stats_available ? `${state.resources.cpu_percent.toFixed(1)}%` : "Unavailable";
    elements.memoryUsage.textContent = state.resources.stats_available ? formatBytes(state.resources.memory_usage_bytes) : "Unavailable";
    elements.storage.replaceChildren();
    const mounts = state.storage?.length ? state.storage : [{ destination: "No mount information available", type: "" }];
    mounts.forEach((mount) => {
      const item = document.createElement("li");
      item.textContent = mount.type ? `${mount.destination} · ${mount.type}${mount.name ? ` · ${mount.name}` : ""}` : mount.destination;
      elements.storage.append(item);
    });
    updateButtons();
  }

  async function readState() {
    try {
      const response = await fetch("/api/state", { cache: "no-store" });
      if (!response.ok) throw new Error(`Status request failed (${response.status})`);
      render(await response.json());
    } catch (error) { setNotice(error.message); }
  }

  async function readLogs() {
    elements.logs.textContent = "Loading…";
    try {
      const response = await fetch("/api/logs", { cache: "no-store" });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || `Log request failed (${response.status})`);
      elements.logs.textContent = result.logs || "No log lines returned.";
    } catch (error) { elements.logs.textContent = error.message; }
  }

  async function lifecycle(action) {
    if ((action === "stop" || action === "restart") && !window.confirm(`${action[0].toUpperCase()}${action.slice(1)} Oracle? Database files will be preserved.`)) return;
    busy = true; updateButtons(); setNotice(`${action[0].toUpperCase()}${action.slice(1)}ing Oracle…`);
    try {
      const response = await fetch("/api/lifecycle", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-OraLab-CSRF": csrf },
        body: JSON.stringify({ action })
      });
      const result = await response.json().catch(() => ({ message: `Request failed (${response.status})` }));
      if (result.state) render(result.state);
      setNotice(result.message, result.ok);
    } catch (error) { setNotice(error.message); }
    finally { busy = false; updateButtons(); }
  }

  elements.refresh.addEventListener("click", readState);
  elements.logsButton.addEventListener("click", readLogs);
  elements.start.addEventListener("click", () => lifecycle("start"));
  elements.stop.addEventListener("click", () => lifecycle("stop"));
  elements.restart.addEventListener("click", () => lifecycle("restart"));
  document.addEventListener("visibilitychange", () => { if (!document.hidden && !busy) readState(); });
  setInterval(() => { if (!document.hidden && !busy) readState(); }, 5000);
  readState();
})();
