let inFlight = null;
let pending = false;
let lastPayload = "";
let lastSent = 0;

async function sendReport() {
 const cfg = await chrome.storage.local.get(["enabled", "token", "app", "excludedDomains", "retryAt", "failures"]);
 const { enabled = false, token = "", app = "Google Chrome", excludedDomains = "", retryAt = 0, failures = 0 } = cfg;
 if (!enabled || !token) return;
 if (Date.now() < retryAt) return;
 let hint = { app, focused: false, private: true, url: "", title: "" };
 try {
  const win = await chrome.windows.getLastFocused();
  if (win.focused && !win.incognito) {
   const [tab] = await chrome.tabs.query({ active: true, windowId: win.id });
   if (tab && !tab.incognito && tab.url) {
    const url = new URL(tab.url);
    if (url.protocol === "http:" || url.protocol === "https:") {
     const excluded = excludedDomains.split(/\n/).map(s => s.trim().toLowerCase().replace(/^\.+|\.+$/g, "")).filter(Boolean).some(d => url.hostname === d || url.hostname.endsWith(`.${d}`));
     if (!excluded) {
      url.username = url.password = url.search = url.hash = "";
      hint = { app, focused: true, private: false, url: url.toString(), title: (tab.title || "").slice(0, 1000) };
     }
    }
   }
  }
 } catch {
  // Observation failure suppresses native fallback instead of reusing a stale tab.
 }
 const body = JSON.stringify(hint);
 // Coalesce duplicate tab events, but heartbeat at five seconds to keep backend
 // hints fresh. A changed private/focus context is never deduplicated.
 if (body === lastPayload && Date.now() - lastSent < 2000) return;
 try {
  const response = await fetch("http://127.0.0.1:8080/api/activity/browser", {
   method: "POST", headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` }, body,
   signal: AbortSignal.timeout(4000),
  });
  if (!response.ok) throw new Error(`Dendrite returned ${response.status}`);
  const state = await response.json();
  lastPayload = body; lastSent = Date.now();
  await chrome.storage.local.set({ failures: 0, retryAt: 0, lastStatus: state.recording === false ? "Connected; Dendrite recording is stopped" : "Connected to Dendrite" });
 } catch (error) {
  const count = Math.min(failures + 1, 6);
  const delay = Math.min(60000, 5000 * 2 ** (count - 1));
  await chrome.storage.local.set({ failures: count, retryAt: Date.now() + delay, lastStatus: error.message?.startsWith("Dendrite returned ") ? error.message : "Dendrite backend is not reachable" });
 }
}
function report() {
 pending = true;
 if (inFlight) return inFlight;
 inFlight = (async () => {
  try { while (pending) { pending = false; await sendReport(); } }
  finally { inFlight = null; }
 })();
 return inFlight;
}
chrome.tabs.onActivated.addListener(report);
chrome.tabs.onUpdated.addListener((_id, info, tab) => { if (tab.active && (info.url || info.title || info.status === "complete")) report(); });
chrome.windows.onFocusChanged.addListener(report);
chrome.alarms.onAlarm.addListener(alarm => { if (alarm.name === "dendrite") report(); });
const ensureAlarm = () => chrome.alarms.create("dendrite", { periodInMinutes: 0.5 });
chrome.runtime.onInstalled.addListener(ensureAlarm);
chrome.runtime.onStartup.addListener(ensureAlarm);
chrome.action.onClicked.addListener(() => chrome.runtime.openOptionsPage());
chrome.runtime.onMessage.addListener(message => { if (message === "report") report(); });
chrome.storage.onChanged.addListener((changes, area) => {
 if (area === "local" && ["enabled", "token", "app", "excludedDomains"].some(key => key in changes)) {
  lastPayload = "";
  chrome.storage.local.set({ failures: 0, retryAt: 0 }).then(report);
 }
});
// Alarms wake suspended workers; the interval refreshes hints while awake.
setInterval(report, 5000);
