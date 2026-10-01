async function report() {
 const { enabled = false, token = "", app = "Google Chrome", excludedDomains = "" } = await chrome.storage.local.get(["enabled", "token", "app", "excludedDomains"]);
 if (!enabled || !token) return;
 let hint = { app, focused: false, private: false, url: "", title: "" };
 try {
  const win = await chrome.windows.getLastFocused();
  if (win.focused && !win.incognito) {
   const [tab] = await chrome.tabs.query({ active: true, windowId: win.id });
   if (tab && !tab.incognito && tab.url) {
    const url = new URL(tab.url);
    if (url.protocol === "http:" || url.protocol === "https:") {
     const excluded = excludedDomains.split(/\n/).map(s => s.trim().toLowerCase()).filter(Boolean).some(d => url.hostname === d || url.hostname.endsWith(`.${d}`));
     if (!excluded) {
      url.username = url.password = url.search = url.hash = "";
      hint = { app, focused: true, private: false, url: url.toString(), title: (tab.title || "").slice(0, 1000) };
     }
    }
   }
  }
  const response = await fetch("http://127.0.0.1:8080/api/activity/browser", { method: "POST", headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` }, body: JSON.stringify(hint) });
  await chrome.storage.local.set({ lastStatus: response.ok ? "Connected to Dendrite" : `Dendrite returned ${response.status}` });
 } catch { await chrome.storage.local.set({ lastStatus: "Dendrite backend is not reachable" }); }
}
chrome.tabs.onActivated.addListener(report);
chrome.tabs.onUpdated.addListener((_id, info, tab) => { if (tab.active && (info.url || info.title || info.status === "complete")) report(); });
chrome.windows.onFocusChanged.addListener(report);
chrome.alarms.onAlarm.addListener(report);
chrome.runtime.onInstalled.addListener(() => chrome.alarms.create("dendrite", { periodInMinutes: 0.5 }));
chrome.runtime.onStartup.addListener(() => chrome.alarms.create("dendrite", { periodInMinutes: 0.5 }));
chrome.action.onClicked.addListener(() => chrome.runtime.openOptionsPage());
chrome.runtime.onMessage.addListener(message => { if (message === "report") report(); });
// Refresh while the worker is awake; alarms wake it after suspension.
setInterval(report, 5000);
