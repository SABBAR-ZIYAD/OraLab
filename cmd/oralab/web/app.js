(() => {
  "use strict";

  const csrf = document.querySelector('meta[name="oralab-csrf"]').content;
  const $ = (id) => document.getElementById(id);
  const elements = {
    overall: $("overall-status"), notice: $("notice"), host: $("host-value"), docker: $("docker-value"),
    oracle: $("oracle-value"), database: $("database-value"), capacity: $("capacity-value"),
    cpuRange: $("cpu-input"), cpuNumber: $("cpu-number"), cpuOutput: $("cpu-output"),
    memoryRange: $("memory-input"), memoryNumber: $("memory-number"), memoryOutput: $("memory-output"),
    apply: $("apply-button"), form: $("resource-form"), refresh: $("refresh-button"),
    start: $("start-button"), stop: $("stop-button"), restart: $("restart-button"),
    cpuUsage: $("cpu-usage-value"), memoryUsage: $("memory-usage-value"), cpuMeter: $("cpu-meter"),
    memoryMeter: $("memory-meter"), statsTime: $("stats-time"), storage: $("storage-list"),
    dialog: $("restart-dialog"), restartSummary: $("restart-summary")
  };

  let currentState = null;
  let busy = false;
  let initializedInputs = false;

  const formatBytes = (bytes) => {
    if (!bytes) return "—";
    const gib = bytes / 1073741824;
    return `${gib >= 10 ? gib.toFixed(0) : gib.toFixed(1)} GiB`;
  };

  const label = (value) => String(value || "unknown").replaceAll("-", " ");

  function setNotice(message, success = false) {
    elements.notice.hidden = !message;
    elements.notice.textContent = message || "";
    elements.notice.classList.toggle("success", success);
  }

  function setBusy(value, message = "Working…") {
    busy = value;
    elements.refresh.disabled = value;
    elements.apply.disabled = value || !canEdit();
    updateLifecycleButtons();
    if (value) setNotice(message);
  }

  function canEdit() {
    return Boolean(currentState?.docker?.reachable && currentState?.docker?.local_context);
  }

  function updateOverall(state) {
    const span = elements.overall.querySelector("span:last-child");
    elements.overall.className = "status-chip neutral";
    if (!state.docker.reachable) {
      span.textContent = "Docker unavailable";
      elements.overall.classList.add("bad");
    } else if (!state.docker.local_context) {
      span.textContent = "Remote context blocked";
      elements.overall.classList.add("warn");
    } else if (state.oracle.readiness === "ready") {
      span.textContent = "Oracle ready";
      elements.overall.classList.add("good");
    } else if (state.oracle.running) {
      span.textContent = "Oracle starting";
      elements.overall.classList.add("warn");
    } else {
      span.textContent = "Oracle stopped";
    }
  }

  function render(state, preserveInputs = false) {
    currentState = state;
    updateOverall(state);
    elements.host.textContent = `${state.host.os} · ${state.host.architecture}`;
    elements.docker.textContent = state.docker.reachable
      ? `${state.docker.engine_os || "Docker Engine"} · ${state.docker.context || "default"}`
      : state.docker.error || "Unavailable";
    elements.oracle.textContent = state.oracle.error || label(state.oracle.status);
    elements.database.textContent = label(state.oracle.readiness);

    const maxCPUs = Math.max(1, state.docker.cpus || 1);
    const maxMemory = Math.max(1, Math.floor((state.docker.memory_bytes || 1073741824) / 1073741824));
    elements.capacity.textContent = state.docker.reachable
      ? `Docker reports ${state.docker.cpus} CPUs / ${formatBytes(state.docker.memory_bytes)}`
      : "Docker capacity unavailable";
    for (const input of [elements.cpuRange, elements.cpuNumber]) input.max = String(maxCPUs);
    for (const input of [elements.memoryRange, elements.memoryNumber]) input.max = String(maxMemory);

    const configuredCPU = state.resources.configured_cpus || Math.round(state.resources.applied_cpus || 0);
    const configuredMemory = Math.round((state.resources.configured_memory_bytes || state.resources.applied_memory_bytes || 0) / 1073741824);
    if ((!preserveInputs || !initializedInputs) && configuredCPU > 0) {
      elements.cpuRange.value = elements.cpuNumber.value = String(configuredCPU);
    }
    if ((!preserveInputs || !initializedInputs) && configuredMemory > 0) {
      elements.memoryRange.value = elements.memoryNumber.value = String(configuredMemory);
    }
    initializedInputs = true;
    updateInputLabels();

    const editable = canEdit();
    for (const input of [elements.cpuRange, elements.cpuNumber, elements.memoryRange, elements.memoryNumber]) input.disabled = !editable || busy;
    elements.apply.disabled = !editable || busy || !elements.cpuNumber.value || !elements.memoryNumber.value;

    if (state.resources.stats_available) {
      const cpuLimit = state.resources.applied_cpus || state.docker.cpus || 1;
      const cpuCapacityPercent = Math.min(100, state.resources.cpu_percent / cpuLimit);
      const memoryLimit = state.resources.applied_memory_bytes || 1;
      const memoryPercent = Math.min(100, state.resources.memory_usage_bytes / memoryLimit * 100);
      elements.cpuUsage.textContent = `${state.resources.cpu_percent.toFixed(1)}%`;
      elements.memoryUsage.textContent = `${formatBytes(state.resources.memory_usage_bytes)} / ${formatBytes(memoryLimit)}`;
      elements.cpuMeter.style.width = `${cpuCapacityPercent}%`;
      elements.memoryMeter.style.width = `${memoryPercent}%`;
      elements.statsTime.textContent = `Updated ${new Date(state.checked_at).toLocaleTimeString()}`;
    } else {
      elements.cpuUsage.textContent = elements.memoryUsage.textContent = "Unavailable";
      elements.cpuMeter.style.width = elements.memoryMeter.style.width = "0";
      elements.statsTime.textContent = state.resources.error || "Container not running";
    }

    elements.storage.replaceChildren();
    if (state.storage?.length) {
      state.storage.forEach((mount) => {
        const item = document.createElement("li");
        item.textContent = `${mount.destination} · ${mount.type}${mount.name ? ` · ${mount.name}` : ""}`;
        elements.storage.append(item);
      });
    } else {
      const item = document.createElement("li");
      item.textContent = "No container mounts available";
      elements.storage.append(item);
    }
    updateLifecycleButtons();
  }

  function updateLifecycleButtons() {
    const manageable = Boolean(currentState?.docker?.reachable && currentState?.docker?.local_context && currentState?.oracle?.service);
    const running = Boolean(currentState?.oracle?.running);
    elements.start.disabled = busy || !manageable || running;
    elements.stop.disabled = busy || !manageable || !running;
    elements.restart.disabled = busy || !manageable || !running;
  }

  function updateInputLabels() {
    elements.cpuOutput.textContent = elements.cpuNumber.value ? `${elements.cpuNumber.value} CPUs` : "Not configured";
    elements.memoryOutput.textContent = elements.memoryNumber.value ? `${elements.memoryNumber.value} GiB` : "Not configured";
  }

  async function readState({ preserveInputs = true } = {}) {
    try {
      const response = await fetch("/api/state", { cache: "no-store" });
      if (!response.ok) throw new Error(`Status request failed (${response.status})`);
      render(await response.json(), preserveInputs);
    } catch (error) {
      setNotice(error.message);
    }
  }

  async function post(path, payload) {
    const response = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-OraLab-CSRF": csrf },
      body: JSON.stringify(payload)
    });
    const result = await response.json().catch(() => ({ message: `Request failed (${response.status})` }));
    return { response, result };
  }

  async function applyResources(confirmRestart = false) {
    const cpus = Number.parseInt(elements.cpuNumber.value, 10);
    const memory = Number.parseInt(elements.memoryNumber.value, 10);
    setBusy(true, confirmRestart ? "Applying limits and restarting Oracle…" : "Validating resource limits…");
    try {
      const { response, result } = await post("/api/apply", { cpus, memory_gib: memory, confirm_restart: confirmRestart });
      if (response.status === 409 && result.requires_restart) {
        setBusy(false, "");
        elements.restartSummary.textContent = `CPU: ${currentState.resources.configured_cpus || "not set"} → ${cpus}; Memory: ${formatBytes(currentState.resources.configured_memory_bytes)} → ${memory} GiB.`;
        elements.dialog.showModal();
        return;
      }
      if (result.state) render(result.state);
      setNotice(result.message, result.ok);
    } catch (error) {
      setNotice(error.message);
    } finally {
      setBusy(false, "");
    }
  }

  async function lifecycle(action) {
    setBusy(true, `${action[0].toUpperCase()}${action.slice(1)}ing Oracle…`);
    try {
      const { result } = await post("/api/lifecycle", { action });
      if (result.state) render(result.state);
      setNotice(result.message, result.ok);
    } catch (error) {
      setNotice(error.message);
    } finally {
      setBusy(false, "");
    }
  }

  function bindPair(range, number) {
    range.addEventListener("input", () => { number.value = range.value; updateInputLabels(); });
    number.addEventListener("input", () => { range.value = number.value; updateInputLabels(); });
  }

  bindPair(elements.cpuRange, elements.cpuNumber);
  bindPair(elements.memoryRange, elements.memoryNumber);
  elements.form.addEventListener("submit", (event) => { event.preventDefault(); applyResources(false); });
  elements.refresh.addEventListener("click", () => readState({ preserveInputs: false }));
  elements.start.addEventListener("click", () => lifecycle("start"));
  elements.stop.addEventListener("click", () => lifecycle("stop"));
  elements.restart.addEventListener("click", () => lifecycle("restart"));
  elements.dialog.addEventListener("close", () => {
    if (elements.dialog.returnValue === "confirm") applyResources(true);
  });

  document.addEventListener("visibilitychange", () => {
    if (!document.hidden && !busy) readState();
  });
  setInterval(() => { if (!document.hidden && !busy) readState(); }, 5000);
  readState({ preserveInputs: false });
})();
