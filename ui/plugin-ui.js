const groups = [
  ["shop", "Shops"],
  ["craft", "Trades & crafts"],
  ["office", "Professional offices"],
  ["amenity", "Food, finance & services"],
  ["tourism", "Hospitality & attractions"],
  ["healthcare", "Healthcare"],
  ["leisure", "Commercial recreation"],
];

export async function mount(target, context) {
  const root = document.createElement("section");
  root.className = "lbd-app";
  root.innerHTML = `
    <style>
      .lbd-app{display:grid;gap:18px;color:var(--ink,#172622);font:14px/1.45 system-ui,sans-serif}.lbd-app *{box-sizing:border-box}
      .lbd-hero{padding:22px;border-radius:16px;background:linear-gradient(135deg,var(--green,#0c3c35),#17685b);color:#fff}.lbd-hero h1{margin:0 0 6px;font-size:1.55rem}.lbd-hero p{max-width:700px;margin:0;color:#d8eee8}
      .lbd-stats{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}.lbd-stat,.lbd-card{border:1px solid var(--line,#dddcd4);border-radius:13px;background:var(--panel,#fbfaf7)}.lbd-stat{padding:14px}.lbd-stat strong{display:block;font-size:1.35rem}.lbd-stat span,.lbd-muted{color:var(--muted,#667570)}
      .lbd-tabs{display:flex;gap:6px;flex-wrap:wrap}.lbd-tabs button{padding:8px 12px;border:1px solid var(--line,#dddcd4);border-radius:9px;background:var(--panel,#fff);color:inherit;cursor:pointer;font-weight:700}.lbd-tabs button[aria-selected=true]{background:var(--green,#0c3c35);border-color:var(--green,#0c3c35);color:#fff}
      .lbd-panel{display:none}.lbd-panel[data-active=true]{display:grid;gap:14px}.lbd-card{padding:18px}.lbd-card h2,.lbd-card h3{margin:0 0 6px}.lbd-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}.lbd-field{display:grid;gap:6px}.lbd-field label,.lbd-label{font-weight:700}.lbd-field input,.lbd-field textarea{width:100%;border:1px solid var(--line,#c8cdc9);border-radius:9px;padding:10px;background:var(--panel,#fff);color:inherit;font:inherit}.lbd-field textarea{min-height:145px;resize:vertical;font-family:ui-monospace,monospace}
      .lbd-location{display:flex;gap:12px;align-items:end;flex-wrap:wrap}.lbd-radius{display:grid;gap:4px;font-weight:700}.lbd-radius-control{display:flex;align-items:center;gap:6px}.lbd-radius input{width:86px;border:1px solid var(--line,#c8cdc9);border-radius:9px;padding:9px 10px;background:var(--panel,#fff);color:inherit;font:inherit}.lbd-checks{display:grid;grid-template-columns:1fr;gap:6px}.lbd-check{display:grid;grid-template-columns:20px minmax(0,1fr);gap:9px;align-items:center;min-height:40px;padding:8px 10px;border:1px solid var(--line,#dddcd4);border-radius:9px;background:color-mix(in srgb,var(--panel,#fff) 92%,transparent)}.lbd-check input{width:16px;height:16px;margin:0}.lbd-actions{display:flex;gap:9px;align-items:center;flex-wrap:wrap}.lbd-button{border:0;border-radius:9px;padding:10px 14px;background:var(--green,#0c3c35);color:#fff;font-weight:750;cursor:pointer;text-decoration:none}.lbd-button.secondary{border:1px solid var(--line,#c8cdc9);background:transparent;color:inherit}.lbd-button.danger{background:#a53a31}.lbd-button:disabled{opacity:.55;cursor:wait}
      .lbd-note{padding:11px 13px;border-left:3px solid #d19a31;background:#d19a3114;border-radius:7px}.lbd-jobs{display:grid;gap:9px}.lbd-job{padding:12px;border:1px solid var(--line,#dddcd4);border-radius:10px}.lbd-job-head{display:flex;justify-content:space-between;gap:12px}.lbd-job-head span{text-transform:capitalize}.lbd-progress{height:7px;margin:9px 0;border-radius:999px;background:#dfe5e1;overflow:hidden}.lbd-progress span{display:block;height:100%;background:#208b73}.lbd-job small{display:block;color:var(--muted,#667570)}.lbd-job-help{margin-top:7px}.lbd-job-actions{display:flex;gap:8px;margin-top:10px;flex-wrap:wrap}
      .lbd-search{display:grid;grid-template-columns:1fr auto auto;gap:8px}.lbd-search input{border:1px solid var(--line,#c8cdc9);border-radius:9px;padding:10px;background:var(--panel,#fff);color:inherit}.lbd-table-wrap{overflow:auto;border:1px solid var(--line,#dddcd4);border-radius:11px}.lbd-table{width:100%;border-collapse:collapse;min-width:850px}.lbd-table th,.lbd-table td{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line,#e6e6e1);vertical-align:top}.lbd-table th{font-size:.8rem;text-transform:uppercase;letter-spacing:.04em;color:var(--muted,#667570)}.lbd-table a{color:var(--green,#0c695b)}.lbd-empty{padding:24px;text-align:center;color:var(--muted,#667570)}
      .lbd-status{min-height:20px;color:var(--muted,#667570)}.lbd-status[data-kind=error]{color:#a53a31}.lbd-status[data-kind=success]{color:#16705e}.lbd-license{font-size:.84rem;color:var(--muted,#667570)}
      @media(max-width:800px){.lbd-stats{grid-template-columns:repeat(2,1fr)}.lbd-grid{grid-template-columns:1fr}.lbd-search{grid-template-columns:1fr}}
    </style>
    <header class="lbd-hero"><h1>Local Business Directory</h1><p>Build broad, reusable market coverage by sweeping many areas and business categories into one deduplicated directory.</p></header>
    <div class="lbd-stats"><div class="lbd-stat"><strong data-stat="businesses">—</strong><span>Businesses</span></div><div class="lbd-stat"><strong data-stat="cities">—</strong><span>Cities represented</span></div><div class="lbd-stat"><strong data-stat="categories">—</strong><span>Categories</span></div><div class="lbd-stat"><strong data-stat="updated">—</strong><span>Last refreshed</span></div></div>
    <nav class="lbd-tabs" aria-label="Directory views"><button type="button" data-tab="sweeps" aria-selected="true">Bulk sweeps</button><button type="button" data-tab="directory" aria-selected="false">Directory</button><button type="button" data-tab="settings" aria-selected="false">Data source</button></nav>
    <div class="lbd-panel" data-panel="sweeps" data-active="true"><div class="lbd-grid">
      <form class="lbd-card" data-form="sweep"><h2>Queue a broad sweep</h2><p class="lbd-muted">Add up to 50 areas. Every selected category is queried for every area, one request at a time.</p><div class="lbd-field"><label for="lbd-job-name">Sweep name</label><input id="lbd-job-name" name="name" placeholder="North Texas coverage — September"></div><div class="lbd-field"><label for="lbd-areas">Areas</label><textarea id="lbd-areas" name="areas" placeholder="Dallas core, 32.7767, -96.7970, 15\nFort Worth core, 32.7555, -97.3308, 15"></textarea><small class="lbd-muted">One per line: label, latitude, longitude, radius in km (0.25–25).</small></div><div class="lbd-location"><button class="lbd-button secondary" type="button" data-action="location">Use my location</button><label class="lbd-radius" for="lbd-location-radius">Location radius<span class="lbd-radius-control"><input id="lbd-location-radius" name="locationRadius" type="number" min="0.25" max="25" step="0.25" value="10"> km</span></label></div><fieldset style="border:0;padding:0;margin:14px 0"><legend class="lbd-label">Business coverage</legend><div class="lbd-checks" data-groups></div></fieldset><div class="lbd-note">Broad public-endpoint runs should be occasional and paced. For recurring regional coverage, configure a dedicated Overpass-compatible provider.</div><div class="lbd-actions" style="margin-top:14px"><button class="lbd-button" type="submit">Queue bulk sweep</button><span class="lbd-status" data-status="sweep"></span></div></form>
      <div class="lbd-card"><h2>Recent sweeps</h2><div class="lbd-jobs" data-jobs><div class="lbd-empty">No sweeps yet.</div></div></div>
    </div></div>
    <div class="lbd-panel" data-panel="directory"><div class="lbd-card"><div class="lbd-search"><input data-search placeholder="Search business, city, or website"><button class="lbd-button" data-action="search" type="button">Search</button><a class="lbd-button secondary" data-export>Export CSV</a></div><p class="lbd-status" data-status="directory"></p><div class="lbd-table-wrap"><table class="lbd-table"><thead><tr><th>Business</th><th>Category</th><th>Location</th><th>Contact</th><th>Source</th></tr></thead><tbody data-businesses></tbody></table></div><div class="lbd-actions" style="margin-top:12px"><button class="lbd-button secondary" data-action="previous" type="button">Previous</button><button class="lbd-button secondary" data-action="next" type="button">Next</button></div></div></div>
    <div class="lbd-panel" data-panel="settings"><form class="lbd-card" data-form="settings"><h2>Overpass-compatible data source</h2><p class="lbd-muted">The endpoint is administrator-selected and must use public HTTPS. The plugin never uses Nominatim or grid-based geocoding.</p><div class="lbd-grid"><div class="lbd-field"><label for="lbd-endpoint">Endpoint</label><input id="lbd-endpoint" name="endpoint" type="url" required></div><div class="lbd-field"><label for="lbd-interval">Seconds between requests</label><input id="lbd-interval" name="interval" type="number" min="1" max="60" required></div></div><div class="lbd-actions" style="margin-top:14px"><button class="lbd-button" type="submit">Save data source</button><span class="lbd-status" data-status="settings"></span></div></form></div>
    <footer class="lbd-license">Business data © <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noreferrer">OpenStreetMap contributors</a>, available under ODbL 1.0. Coverage and contact fields depend on community mapping and should be verified before operational use.</footer>`;
  target.replaceChildren(root);

  for (const [value, label] of groups) {
    const row = document.createElement("label"); row.className = "lbd-check";
    const input = document.createElement("input"); input.type = "checkbox"; input.name = "group"; input.value = value; input.checked = true;
    row.append(input, document.createTextNode(label)); root.querySelector("[data-groups]").append(row);
  }

  let directoryOffset = 0, directoryTotal = 0;
  const pageSize = 100, cleanups = [];
  const on = (element, event, listener) => { element.addEventListener(event, listener); cleanups.push(() => element.removeEventListener(event, listener)); };
  const setStatus = (name, text, kind = "") => { const element = root.querySelector(`[data-status="${name}"]`); element.textContent = text; element.dataset.kind = kind; };
  const errorMessage = (error) => error instanceof Error ? error.message : "Unexpected error";

  for (const button of root.querySelectorAll("[data-tab]")) on(button, "click", () => {
    for (const item of root.querySelectorAll("[data-tab]")) item.setAttribute("aria-selected", String(item === button));
    for (const panel of root.querySelectorAll("[data-panel]")) panel.dataset.active = String(panel.dataset.panel === button.dataset.tab);
    if (button.dataset.tab === "directory") loadDirectory();
  });

  on(root.querySelector("[data-action=location]"), "click", () => {
    if (!navigator.geolocation) return setStatus("sweep", "Location is unavailable in this browser.", "error");
    const radius = Number(root.querySelector("[name=locationRadius]").value);
    if (!Number.isFinite(radius) || radius < 0.25 || radius > 25) return setStatus("sweep", "Location radius must be between 0.25 and 25 km.", "error");
    navigator.geolocation.getCurrentPosition(({ coords }) => { root.querySelector("[name=areas]").value = `My area, ${coords.latitude.toFixed(6)}, ${coords.longitude.toFixed(6)}, ${radius}`; setStatus("sweep", `Location added with a ${radius} km radius.`, "success"); }, () => setStatus("sweep", "Location access was not granted.", "error"), { enableHighAccuracy: false, timeout: 10000 });
  });

  on(root.querySelector("[data-form=sweep]"), "submit", async (event) => {
    event.preventDefault(); const form = event.currentTarget, submit = form.querySelector("button[type=submit]");
    try {
      const areas = parseAreas(form.elements.areas.value), selectedGroups = [...form.querySelectorAll("[name=group]:checked")].map((item) => item.value);
      if (!selectedGroups.length) throw new Error("Select at least one business coverage group.");
      submit.disabled = true; setStatus("sweep", "Queueing sweep…");
      await context.http.json("/api/discovery-jobs", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: form.elements.name.value, areas, groups: selectedGroups }) });
      setStatus("sweep", "Bulk sweep queued.", "success"); context.notifications.show("Bulk business discovery sweep queued.", "success"); await Promise.all([loadJobs(), loadStats()]);
    } catch (error) { setStatus("sweep", errorMessage(error), "error"); } finally { submit.disabled = false; }
  });

  on(root.querySelector("[data-action=search]"), "click", () => { directoryOffset = 0; loadDirectory(); });
  on(root.querySelector("[data-search]"), "keydown", (event) => { if (event.key === "Enter") { directoryOffset = 0; loadDirectory(); } });
  on(root.querySelector("[data-action=previous]"), "click", () => { directoryOffset = Math.max(0, directoryOffset - pageSize); loadDirectory(); });
  on(root.querySelector("[data-action=next]"), "click", () => { if (directoryOffset + pageSize < directoryTotal) { directoryOffset += pageSize; loadDirectory(); } });
  on(root.querySelector("[data-form=settings]"), "submit", async (event) => {
    event.preventDefault(); const form = event.currentTarget;
    try { setStatus("settings", "Saving…"); await context.http.json("/api/settings", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ overpassEndpoint: form.elements.endpoint.value, minIntervalSeconds: Number(form.elements.interval.value) }) }); setStatus("settings", "Data source saved.", "success"); context.notifications.show("Business discovery data source updated.", "success"); }
    catch (error) { setStatus("settings", errorMessage(error), "error"); }
  });

  async function loadStats() {
    try { const stats = await context.http.json("/api/stats"); root.querySelector("[data-stat=businesses]").textContent = Number(stats.businesses || 0).toLocaleString(); root.querySelector("[data-stat=cities]").textContent = Number(stats.cities || 0).toLocaleString(); root.querySelector("[data-stat=categories]").textContent = Number(stats.categories || 0).toLocaleString(); root.querySelector("[data-stat=updated]").textContent = stats.lastUpdatedAt ? new Date(stats.lastUpdatedAt).toLocaleString() : "Never"; } catch { /* visibly unavailable */ }
  }
  async function loadJobs() { try { const { jobs } = await context.http.json("/api/discovery-jobs"); renderJobs(jobs || []); } catch (error) { root.querySelector("[data-jobs]").textContent = errorMessage(error); } }
  function renderJobs(jobs) {
    const container = root.querySelector("[data-jobs]"); container.replaceChildren();
    if (!jobs.length) { const empty = document.createElement("div"); empty.className = "lbd-empty"; empty.textContent = "No sweeps yet."; container.append(empty); return; }
    for (const job of jobs.slice(0, 12)) {
      const card = document.createElement("article"); card.className = "lbd-job"; const head = document.createElement("div"); head.className = "lbd-job-head";
      const title = document.createElement("strong"); title.textContent = job.name; const status = document.createElement("span"); status.textContent = job.status === "failed" ? "Needs retry" : job.status; head.append(title, status);
      const progress = document.createElement("div"); progress.className = "lbd-progress"; const bar = document.createElement("span"); bar.style.width = `${job.totalSteps ? Math.round(job.completedSteps / job.totalSteps * 100) : 0}%`; progress.append(bar);
      const details = document.createElement("small"); details.textContent = `${job.completedSteps}/${job.totalSteps} requests · ${Number(job.recordsSeen).toLocaleString()} found · ${Number(job.recordsCreated).toLocaleString()} new · ${Number(job.recordsUpdated).toLocaleString()} refreshed`; card.append(head, progress, details);
      if (job.lastError) { const failure = document.createElement("div"); failure.className = "lbd-status"; failure.dataset.kind = "error"; failure.textContent = job.lastError; card.append(failure); }
      if (job.status === "failed") {
        const help = document.createElement("small"); help.className = "lbd-job-help"; help.textContent = "Successful requests and saved businesses are preserved. Retry resumes at the failed request.";
        const actions = document.createElement("div"); actions.className = "lbd-job-actions"; const retry = document.createElement("button"); retry.type = "button"; retry.className = "lbd-button"; retry.textContent = "Retry remaining requests";
        retry.addEventListener("click", async () => { retry.disabled = true; try { await context.http.json(`/api/discovery-jobs/${encodeURIComponent(job.id)}/retry`, { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }); context.notifications.show("Failed discovery requests queued again.", "success"); await loadJobs(); } catch (error) { retry.disabled = false; context.notifications.show(errorMessage(error), "error"); } }); actions.append(retry); card.append(help, actions);
      }
      if (["queued", "running"].includes(job.status)) { const actions = document.createElement("div"); actions.className = "lbd-job-actions"; const cancel = document.createElement("button"); cancel.type = "button"; cancel.className = "lbd-button danger"; cancel.textContent = "Cancel"; cancel.addEventListener("click", async () => { cancel.disabled = true; try { await context.http.json(`/api/discovery-jobs/${encodeURIComponent(job.id)}/cancel`, { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }); await loadJobs(); } catch (error) { cancel.disabled = false; context.notifications.show(errorMessage(error), "error"); } }); actions.append(cancel); card.append(actions); }
      container.append(card);
    }
  }
  async function loadDirectory() {
    const query = root.querySelector("[data-search]").value.trim(), params = new URLSearchParams({ limit: String(pageSize), offset: String(directoryOffset) }); if (query) params.set("query", query); setStatus("directory", "Loading directory…");
    try { const result = await context.http.json(`/api/businesses?${params}`); directoryTotal = result.total || 0; renderBusinesses(result.businesses || []); const start = directoryTotal ? directoryOffset + 1 : 0, end = Math.min(directoryOffset + pageSize, directoryTotal); setStatus("directory", `${start.toLocaleString()}–${end.toLocaleString()} of ${Number(directoryTotal).toLocaleString()} businesses`); root.querySelector("[data-action=previous]").disabled = directoryOffset === 0; root.querySelector("[data-action=next]").disabled = directoryOffset + pageSize >= directoryTotal; root.querySelector("[data-export]").href = `${context.plugin.basePath}/api/businesses.csv${query ? `?query=${encodeURIComponent(query)}` : ""}`; }
    catch (error) { setStatus("directory", errorMessage(error), "error"); }
  }
  function renderBusinesses(businesses) {
    const body = root.querySelector("[data-businesses]"); body.replaceChildren(); if (!businesses.length) { const row = body.insertRow(), cell = row.insertCell(); cell.colSpan = 5; cell.className = "lbd-empty"; cell.textContent = "No matching businesses yet."; return; }
    for (const business of businesses) { const row = body.insertRow(), name = row.insertCell(), strong = document.createElement("strong"); strong.textContent = business.name; name.append(strong); if (business.website) { const link = document.createElement("a"); link.href = business.website; link.target = "_blank"; link.rel = "noreferrer"; link.textContent = "website"; name.append(" · ", link); } row.insertCell().textContent = business.primaryCategory || "—"; row.insertCell().textContent = [business.street, business.city, business.region, business.postalCode].filter(Boolean).join(", ") || `${business.latitude.toFixed(4)}, ${business.longitude.toFixed(4)}`; row.insertCell().textContent = business.phone || business.email || "—"; const source = row.insertCell(), sourceLink = document.createElement("a"); sourceLink.href = business.sourceUrl; sourceLink.target = "_blank"; sourceLink.rel = "noreferrer"; sourceLink.textContent = "OpenStreetMap"; source.append(sourceLink); }
  }
  async function loadSettings() { try { const { settings } = await context.http.json("/api/settings"); root.querySelector("[name=endpoint]").value = settings.overpassEndpoint; root.querySelector("[name=interval]").value = settings.minIntervalSeconds; } catch (error) { setStatus("settings", errorMessage(error), "error"); } }

  await Promise.all([loadStats(), loadJobs(), loadSettings(), loadDirectory()]);
  const timer = setInterval(() => { loadStats(); loadJobs(); }, 5000);
  return () => { clearInterval(timer); for (const cleanup of cleanups.splice(0)) cleanup(); root.remove(); };
}

function parseAreas(text) {
  const lines = text.split(/\r?\n/).map((line) => line.trim()).filter(Boolean); if (!lines.length) throw new Error("Add at least one area."); if (lines.length > 50) throw new Error("A sweep can include at most 50 areas.");
  return lines.map((line, index) => { const parts = line.split(",").map((part) => part.trim()); if (parts.length !== 4) throw new Error(`Area line ${index + 1} must have label, latitude, longitude, and radius.`); const latitude = Number(parts[1]), longitude = Number(parts[2]), radiusKm = Number(parts[3]); if (!Number.isFinite(latitude) || latitude < -90 || latitude > 90) throw new Error(`Area line ${index + 1} has an invalid latitude.`); if (!Number.isFinite(longitude) || longitude < -180 || longitude > 180) throw new Error(`Area line ${index + 1} has an invalid longitude.`); if (!Number.isFinite(radiusKm) || radiusKm < 0.25 || radiusKm > 25) throw new Error(`Area line ${index + 1} radius must be 0.25–25 km.`); return { label: parts[0], latitude, longitude, radiusMeters: Math.round(radiusKm * 1000) }; });
}
