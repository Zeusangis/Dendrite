const ids = ["enabled", "app", "token", "excludedDomains"];
(async () => {
 const cfg = await chrome.storage.local.get([...ids, "lastStatus"]);
 for (const id of ids) { const element = document.getElementById(id); if (id === "enabled") element.checked = cfg[id] || false; else if (cfg[id] !== undefined) element.value = cfg[id]; }
 document.getElementById("status").textContent = cfg.lastStatus || "Disabled until you enable and pair.";
})();
document.getElementById("save").addEventListener("click", async () => {
 const cfg = Object.fromEntries(ids.map(id => [id, id === "enabled" ? document.getElementById(id).checked : document.getElementById(id).value]));
 if (cfg.enabled && !cfg.token.trim()) { document.getElementById("status").textContent = "A pairing token is required."; return; }
 await chrome.storage.local.set(cfg);
 chrome.runtime.sendMessage("report");
 document.getElementById("status").textContent = cfg.enabled ? "Enabled. Check Dendrite for connection status." : "Browser recording stopped.";
});
