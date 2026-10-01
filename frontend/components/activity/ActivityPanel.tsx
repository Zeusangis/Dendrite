"use client";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { ActivityConfig, ActivitySeries, ActivityStatus, ActivitySummary } from "@/lib/types";

export const duration = (seconds: number) => seconds >= 3600 ? `${(seconds / 3600).toFixed(1)} h` : seconds >= 60 ? `${Math.round(seconds / 60)} min` : `${Math.round(seconds)} s`;
const preciseDuration = (seconds: number) => `${duration(seconds)} (${seconds.toLocaleString(undefined, { maximumFractionDigits: 1 })} sec)`;
type ChartMetric = "apps" | "active" | "switches";
const appColors = ["#6d9ee8", "#47b881", "#d69e46", "#c579d4", "#78a8a8", "#8b93a7"];

function ActivityChart({ series, metric }: { series: ActivitySeries; metric: ChartMetric }) {
 const buckets = series.buckets;
 const chartTitle = metric === "apps" ? "App usage by period" : metric === "active" ? "Active and idle time by period" : "Context switches by period";
 const values = buckets.map(bucket => metric === "switches" ? bucket.context_switches : bucket.total_seconds);
 const max = Math.max(1, ...values);
 const left = 52, top = 12, width = 652, height = 190, bottom = top + height;
 const slot = width / Math.max(1, buckets.length), barWidth = Math.min(36, slot * 0.66);
 const labelStep = Math.max(1, Math.ceil(buckets.length / 10));
 const tickLabel = (value: number) => metric === "switches" ? String(Math.round(value)) : duration(value);
 const hasOther = buckets.some(bucket => bucket.apps.some(app => app.app_id === "other"));
 const names = metric === "apps" ? [...series.apps, ...(hasOther ? ["Other"] : [])] : metric === "active" ? ["Active", "Idle"] : [];
 const appIDs = metric === "apps" ? [...series.app_ids, ...(hasOther ? ["other"] : [])] : [];
 const colors = metric === "apps" ? appColors : ["#47b881", "#8b93a7"];
 const describe = (bucket: ActivitySeries["buckets"][number]) => metric === "switches"
  ? `${bucket.context_switches} context switches`
  : metric === "active" ? `${duration(bucket.active_seconds)} active, ${duration(bucket.idle_seconds)} idle`
  : bucket.apps.map(app => `${app.app}: ${duration(app.total_seconds)}`).join(", ") || "No recorded usage";

 return <div className="chart-wrap">
  <svg className="activity-chart" viewBox="0 0 720 250" role="img" aria-labelledby="activity-chart-title activity-chart-description">
   <title id="activity-chart-title">{chartTitle}</title>
   <desc id="activity-chart-description">{series.granularity === "day" ? "Daily" : "Weekly"} comparison across {buckets.length} periods. Exact values are available in the data table below.</desc>
   {[0, 0.5, 1].map(fraction => {
    const y = bottom - fraction * height;
    return <g key={fraction}><line x1={left} x2={left + width} y1={y} y2={y} stroke="var(--border)" strokeDasharray={fraction === 0 ? undefined : "3 4"} /><text x={left - 8} y={y + 4} textAnchor="end" className="axis-label">{tickLabel(max * fraction)}</text></g>;
   })}
   {buckets.map((bucket, index) => {
    const x = left + slot * index + (slot - barWidth) / 2;
    let y = bottom;
    const segments = metric === "apps"
     ? names.map((name, appIndex) => ({ name, value: bucket.apps.filter(app => app.app_id === appIDs[appIndex]).reduce((sum, app) => sum + app.total_seconds, 0) }))
     : metric === "active" ? [{ name: "Active", value: bucket.active_seconds }, { name: "Idle", value: bucket.idle_seconds }]
     : [{ name: "Context switches", value: bucket.context_switches }];
    return <g key={bucket.start}>
     <title>{bucket.label}: {describe(bucket)}</title>
     {segments.map((segment, segmentIndex) => {
      const segmentHeight = segment.value / max * height;
      y -= segmentHeight;
      return <rect key={metric === "apps" ? appIDs[segmentIndex] : segment.name} x={x} y={y} width={barWidth} height={Math.max(segmentHeight, segment.value > 0 ? 0.5 : 0)} fill={metric === "switches" ? appColors[0] : colors[segmentIndex % colors.length]} rx="2" />;
     })}
     {(index % labelStep === 0 || index === buckets.length - 1) && <text x={x + barWidth / 2} y={bottom + 20} textAnchor="middle" className="axis-label">{bucket.label}</text>}
    </g>;
   })}
  </svg>
  {names.length > 0 && <div className="chart-legend" aria-label="Chart legend">{names.map((name, index) => <span key={appIDs[index] ?? name}><i style={{ background: colors[index % colors.length] }} />{name}</span>)}</div>}
  <details className="chart-table"><summary>View chart data table</summary>
   <div className="table-scroll"><table><caption>{chartTitle} — {series.granularity === "day" ? "daily" : "weekly"}</caption><thead><tr><th scope="col">Period</th>{metric === "apps" ? <><th scope="col">App</th><th scope="col">Usage</th><th scope="col">Active</th></> : metric === "active" ? <><th scope="col">Active</th><th scope="col">Idle</th></> : <th scope="col">Context switches</th>}</tr></thead>
    <tbody>{buckets.flatMap(bucket => metric === "apps"
     ? (bucket.apps.length ? bucket.apps : [{ app: "No usage", total_seconds: 0, active_seconds: 0 }]).map((app, index) => <tr key={`${bucket.start}-${index}`}><th scope="row">{bucket.label}</th><td>{app.app}</td><td>{preciseDuration(app.total_seconds)}</td><td>{preciseDuration(app.active_seconds)}</td></tr>)
     : <tr key={bucket.start}><th scope="row">{bucket.label}</th>{metric === "active" ? <><td>{preciseDuration(bucket.active_seconds)}</td><td>{preciseDuration(bucket.idle_seconds)}</td></> : <td>{bucket.context_switches}</td>}</tr>)}</tbody>
   </table></div>
  </details>
 </div>;
}

export function ActivityPanel({ onClose, onOpen, onChanged }: { onClose: () => void; onOpen: (id: number) => void; onChanged: () => Promise<void> }) {
 const [status, setStatus] = useState<ActivityStatus | null>(null), [summary, setSummary] = useState<ActivitySummary | null>(null), [series, setSeries] = useState<ActivitySeries | null>(null), [draft, setDraft] = useState<ActivityConfig | null>(null);
 const [days, setDays] = useState(7), [granularity, setGranularity] = useState<"day" | "week">("day"), [metric, setMetric] = useState<ChartMetric>("apps");
 const [error, setError] = useState(""), [busy, setBusy] = useState(false), [token, setToken] = useState("");
 useEffect(() => {
  let active = true;
  const load = async () => {
   try {
    const [s, u, chart] = await Promise.all([api.activity(), api.activitySummary(days), api.activitySeries(days, granularity)]);
    if (active) { setStatus(s); setSummary(u); setSeries(chart); setDraft(d => d ?? s.config); }
   } catch (e) { if (active) setError(String(e)); }
  };
  load(); const timer = setInterval(load, 5000);
  return () => { active = false; clearInterval(timer); };
 }, [days, granularity]);
 const run = async (action: () => Promise<void>) => { setBusy(true); setError(""); try { await action(); } catch (e) { setError(String(e)); } finally { setBusy(false); } };
 const save = async (cfg: ActivityConfig) => { const s = await api.configureActivity(cfg); setStatus(s); setDraft(s.config); await onChanged(); };
 const max = Math.max(1, ...(summary?.usage.map(u => u.total_seconds) ?? []));
 return <section className="activity-panel" aria-label="Activity dashboard"><header><div><h2>Activity & automatic graph</h2><p>{status?.config.enabled ? "● Background recording enabled" : "○ Recording stopped"} · {status?.browser_connected ? "Browser connected" : "Browser extension not connected"}</p></div><button onClick={onClose}>Close activity</button></header>
  {error && <p role="alert" className="error">{error}</p>}
  {status && <div className="privacy"><button disabled={busy} onClick={() => run(() => save({ ...status.config, enabled: !status.config.enabled }))}>{status.config.enabled ? "Stop recording" : "Resume recording"}</button><span>Local only · no keystrokes, screenshots, or page contents · {status.config.retention_days}-day retention</span></div>}
  {status && !status.supported && <p className="error">Native foreground tracking requires macOS with cgo enabled.</p>}
  {status?.warning && <p role="status" className="warning">{status.warning}</p>}
  <div className="chart-controls">
   <label>Time range <select aria-label="Activity time range" value={days} onChange={e => setDays(Number(e.target.value))}><option value={7}>Last 7 days</option><option value={30}>Last 30 days</option><option value={90}>Last 90 days</option></select></label>
   <label>Group by <select aria-label="Chart granularity" value={granularity} onChange={e => setGranularity(e.target.value as "day" | "week")}><option value="day">Days</option><option value="week">Weeks</option></select></label>
   <div className="metric-tabs" role="group" aria-label="Chart comparison"><button aria-pressed={metric === "apps"} onClick={() => setMetric("apps")}>App usage</button><button aria-pressed={metric === "active"} onClick={() => setMetric("active")}>Active vs idle</button><button aria-pressed={metric === "switches"} onClick={() => setMetric("switches")}>Context switches</button></div>
  </div>
  {series && <section className="comparison" aria-label="Usage comparisons"><div className="comparison-heading"><div><h3>{metric === "apps" ? "App usage" : metric === "active" ? "Active and idle time" : "Context switches"}</h3><p>{granularity === "day" ? "Daily" : "Weekly"} totals · {days}-day range</p></div></div><ActivityChart series={series} metric={metric} />
   {granularity === "week" && <p className="chart-note">Weeks at the range boundaries may be partial; totals include only dates in the selected range.</p>}
   {metric === "switches" && <p className="chart-note">Switches count observed active app changes within one recording run; pauses and long gaps are excluded.</p>}
  </section>}
  {summary && <><div className="metrics"><div><strong>{duration(summary.total_seconds)}</strong><span>Foreground time</span></div><div><strong>{duration(summary.active_seconds)}</strong><span>Active time</span></div><div><strong>{Math.round(summary.active_ratio * 100)}%</strong><span>Active ratio</span></div><div><strong>{duration(Math.max(0, summary.total_seconds - summary.active_seconds))}</strong><span>Idle time</span></div></div>
   <h3>What you use</h3>{!summary.usage.length && <p>No recorded usage yet. Leave Dendrite&apos;s backend running and use your apps; the first interval appears after two samples.</p>}
   <div className="usage">{summary.usage.map(u => <button key={u.title} disabled={!u.node_id} onClick={() => onOpen(u.node_id)}><span>{u.title}</span><div className="bar"><div style={{ width: `${u.total_seconds / max * 100}%` }}><i style={{ width: `${u.total_seconds ? u.active_seconds / u.total_seconds * 100 : 0}%` }} /></div></div><small>{duration(u.active_seconds)} active / {duration(u.total_seconds)} total · {u.sessions} sessions</small></button>)}</div>
   <h3>Recent timeline</h3><div className="timeline">{summary.recent.map(s => <div key={s.id}><time>{new Date(s.started_at).toLocaleTimeString()}</time><strong>{s.app}</strong><span>{s.window_title || s.domain || "Application focus"}</span><small>{duration(s.active_seconds)} active / {duration(s.total_seconds)} total</small>{s.domain && <small>{s.domain}</small>}</div>)}</div></>}
  {draft && <details><summary>Privacy, retention & browser setup</summary><fieldset disabled={busy}><label><input type="checkbox" checked={draft.window_titles} onChange={e => setDraft({ ...draft, window_titles: e.target.checked })} />Record window titles (can contain private information)</label><label><input type="checkbox" checked={draft.browser_pages} onChange={e => setDraft({ ...draft, browser_pages: e.target.checked })} />Record browser domains and sanitized page URLs</label><label>Idle threshold (seconds)<input aria-label="Idle threshold" type="number" min={15} max={900} value={draft.idle_seconds} onChange={e => setDraft({ ...draft, idle_seconds: Number(e.target.value) })} /></label><label>Keep history (days)<input aria-label="Activity retention days" type="number" min={1} max={365} value={draft.retention_days} onChange={e => setDraft({ ...draft, retention_days: Number(e.target.value) })} /></label><label>Excluded app names or bundle IDs (one per line)<textarea aria-label="Excluded apps" value={draft.excluded_apps.join("\n")} onChange={e => setDraft({ ...draft, excluded_apps: e.target.value.split("\n") })} /></label><label>Excluded domains (includes subdomains)<textarea aria-label="Excluded domains" value={draft.excluded_domains.join("\n")} onChange={e => setDraft({ ...draft, excluded_domains: e.target.value.split("\n") })} /></label><button onClick={() => run(() => save(draft))}>Save tracking settings</button>
   <p>Exclusions affect future samples, not existing history. Pausing is remembered across restarts. Window titles require macOS Accessibility permission. Browser page metadata is best-effort natively; install the unpacked extension from browser-extension for Chromium browsers.</p><button onClick={() => run(async () => setToken((await api.activityPairing()).token))}>Show browser pairing token</button>{token && <textarea aria-label="Browser pairing token" readOnly value={token} />}<p>Paste this private token into the extension&apos;s options and enable it. Private tabs are skipped; only the active tab of the focused browser window is reported.</p><button className="danger" onClick={() => run(async () => { if (!confirm("Permanently delete all activity sessions and activity graph nodes? This cannot be undone.")) return; await api.clearActivity(); setSummary(await api.activitySummary(days)); setSeries(await api.activitySeries(days, granularity)); await onChanged(); })}>Delete activity history</button></fieldset></details>}
  <style jsx>{`.activity-panel{padding:20px;overflow:auto;flex:1;background:var(--bg);display:flex;flex-direction:column;gap:16px;min-width:0}header{display:flex;justify-content:space-between;align-items:flex-start;gap:12px}header p,.privacy span{font-size:12px;color:var(--text-dim);margin-top:5px}.privacy{display:flex;gap:12px;align-items:center;flex-wrap:wrap}.warning{padding:12px;border:1px solid #896328;background:#2b2417;font-size:12px}.chart-controls{display:flex;align-items:center;gap:12px;flex-wrap:wrap}.chart-controls label{display:flex;align-items:center;gap:8px;font-size:13px}.metric-tabs{display:flex;gap:6px;flex-wrap:wrap}.metric-tabs button[aria-pressed=true]{border-color:var(--accent);color:var(--accent)}.comparison{padding:16px;background:var(--bg-panel);border:1px solid var(--border);border-radius:8px;min-width:0}.comparison-heading h3{margin:0}.comparison-heading p,.chart-note{font-size:12px;color:var(--text-dim);margin:5px 0}.chart-wrap{min-width:0;overflow-x:auto}.activity-chart{display:block;width:100%;min-width:620px;height:auto;max-height:280px;overflow:visible}.axis-label{fill:var(--text-dim);font-size:10px}.chart-legend{display:flex;gap:14px;flex-wrap:wrap;font-size:12px;color:var(--text-dim)}.chart-legend span{display:flex;align-items:center;gap:5px}.chart-legend i{width:10px;height:10px;border-radius:2px}.chart-table{margin-top:10px;font-size:12px}.chart-table summary{color:var(--text-dim)}.table-scroll{overflow:auto;max-height:280px}.chart-table table{border-collapse:collapse;width:100%;text-align:left;margin-top:8px}.chart-table th,.chart-table td{border-bottom:1px solid var(--border);padding:6px 8px}.chart-table caption{text-align:left;margin-bottom:6px;font-weight:600}.metrics{display:grid;grid-template-columns:repeat(4,1fr);gap:12px}.metrics div{padding:16px;background:var(--bg-panel);border:1px solid var(--border);border-radius:8px;display:flex;flex-direction:column;gap:5px}.metrics strong{font-size:22px}.metrics span{font-size:12px;color:var(--text-dim)}h3{font-size:14px}.usage{display:flex;flex-direction:column;gap:8px}.usage button{text-align:left;display:grid;grid-template-columns:minmax(80px,160px) 1fr;gap:8px}.usage small{grid-column:1/-1;color:var(--text-dim)}.bar{background:var(--bg);border-radius:4px;overflow:hidden;height:12px;margin-top:4px}.bar>div{height:100%;background:#8b93a7}.bar i{display:block;height:100%;background:var(--accent)}.timeline{display:flex;flex-direction:column;gap:8px;max-height:300px;overflow:auto}.timeline>div{display:flex;gap:8px;flex-wrap:wrap;padding:10px;border:1px solid var(--border);border-radius:6px}.timeline span{flex:1;min-width:140px;overflow-wrap:anywhere}.timeline small,.timeline time{color:var(--text-dim);font-size:12px}details{border:1px solid var(--border);padding:12px;border-radius:8px}summary{cursor:pointer}fieldset{border:0;display:flex;flex-direction:column;gap:12px;padding-top:16px}label{display:flex;align-items:center;gap:8px;flex-wrap:wrap;font-size:13px}textarea{width:100%;min-height:70px}fieldset p{font-size:12px;color:var(--text-dim);line-height:1.5}.danger{color:var(--danger)}input[type=number]{width:100px}select{background:var(--bg-panel);color:var(--text);padding:6px;border:1px solid var(--border);border-radius:6px}@media(max-width:760px){.metrics{grid-template-columns:repeat(2,1fr)}.activity-panel{padding:12px}header h2{font-size:18px}.comparison{padding:10px}}`}</style>
 </section>;
}
