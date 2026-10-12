"use client";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { ActivityNodeDetail, ActivityStatus, ActivityWebsiteResult, MutedConnection, Note } from "@/lib/types";
import { duration } from "./ActivityPanel";

const nodeLabel = (node: Note | undefined, path: string) => node?.title || path;

export function WebsiteExplorer({ days, status, connections, revision, onOpen, onChanged }: { days: number; status: ActivityStatus | null; connections: boolean; revision: number; onOpen: (id: number) => void; onChanged: () => void }) {
 const [kind,setKind]=useState<"domains"|"pages">("domains"),[sort,setSort]=useState<"active"|"recent"|"duration">("active");
 const [search,setSearch]=useState(""),[offset,setOffset]=useState(0),[result,setResult]=useState<ActivityWebsiteResult|null>(null),[detail,setDetail]=useState<ActivityNodeDetail|null>(null),[error,setError]=useState("");
 const [muted,setMuted]=useState<MutedConnection[]>([]),[busy,setBusy]=useState(false);
 useEffect(()=>{
  if(!connections){setMuted([]);return;}
  let active=true;
  api.mutedConnections().then(r=>{if(active)setMuted(r.muted);}).catch(e=>{if(active)setError(String(e));});
  return()=>{active=false;};
 },[connections,revision]);
 useEffect(()=>{
  let active=true;let sequence=0;
  // Only the list is reset here: muting bumps the revision, and clearing the
  // selection would close the very panel the user just acted in.
  setResult(null);
  const load=async()=>{const current=++sequence;try{const r=await api.activityWebsites(kind,{days,limit:25,offset,sort,search});if(active&&current===sequence){setResult(r);setError("");}}catch(e){if(active&&current===sequence)setError(String(e));}};
  const delay=setTimeout(load,200),timer=setInterval(load,5000);
  return()=>{active=false;clearTimeout(delay);clearInterval(timer);};
 },[days,kind,offset,sort,search,revision]);
 useEffect(()=>{setDetail(null);},[kind,search,days]);
 const inspect=async(id:number)=>{try{setDetail(await api.activityNode(id));setError("");}catch(e){setError(String(e));}};
 const setHidden=async(sourceId:number,targetId:number,hidden:boolean)=>{
  setBusy(true);
  try{
   if(hidden)await api.unmuteConnection(sourceId,targetId);else await api.muteConnection(sourceId,targetId);
   setMuted((await api.mutedConnections()).muted);
   if(detail)await inspect(detail.node.id);
   setError("");
   onChanged();
  }catch(e){setError(String(e));}finally{setBusy(false);}
 };
 const clearMute=async(m:MutedConnection)=>{
  if(!m.a?.id||!m.b?.id)return;
  await setHidden(m.a.id,m.b.id,true);
 };
 const items=(kind==="domains"?result?.domains:result?.pages)??[];
 return <section className="websites" aria-label={connections?"Activity connections":"Website activity"}>
  <h3>{connections?"Explore automatic connections":"Websites you use"}</h3>
  <div className="filters">
   <label>Show <select aria-label="Website type" value={kind} onChange={e=>{setKind(e.target.value as typeof kind);setOffset(0);}}><option value="domains">Domains</option><option value="pages">Pages</option></select></label>
   <label>Sort <select aria-label="Website sort" value={sort} onChange={e=>{setSort(e.target.value as typeof sort);setOffset(0);}}><option value="active">Most active</option><option value="recent">Most recent</option><option value="duration">Total duration</option></select></label>
   <input aria-label="Search website activity" placeholder="Search title or domain…" value={search} onChange={e=>{setSearch(e.target.value);setOffset(0);}} />
  </div>
  {error&&<p role="alert">{error}</p>}
  {!result?<p role="status">Loading websites…</p>:!items.length?<p>{!status?.config.browser_pages?"Browser page tracking is disabled. Enable it in privacy settings to collect websites.":!status.config.enabled?"Recording is stopped. Retained websites appear here when available.":"No website activity in this range. Native browser metadata is best effort; pair the Chromium extension for reliable focused-tab reporting."}</p>:
   <ul className="website-list">{items.map(w=><li key={w.domain+w.url}>
    <button disabled={!w.node_id} onClick={()=>connections?inspect(w.node_id):onOpen(w.node_id)}>{w.title}</button>
    <small>{w.domain} · {w.browsers.join(", ")}</small>
    {w.url&&<span className="url">{w.url}</span>}
    <small>{duration(w.active_seconds)} active / {duration(w.total_seconds)} total · {w.sessions} sessions · {w.pages} {kind==="domains"?"pages":"page"} · {w.connections} connections</small>
    <small>Last seen {new Date(w.last_seen).toLocaleString()}</small>
    {!connections&&<button disabled={!w.node_id} onClick={()=>inspect(w.node_id)}>Explain connections</button>}
   </li>)}</ul>}
  {result&&result.total>0&&<nav aria-label="Website pagination"><button disabled={offset===0} onClick={()=>setOffset(Math.max(0,offset-25))}>Previous websites</button><span>{offset+1}–{Math.min(offset+25,result.total)} of {result.total}</span><button disabled={offset+25>=result.total} onClick={()=>setOffset(offset+25)}>Next websites</button></nav>}
  {detail&&<section className="connections" aria-label="Website connection explanations"><h4>{detail.node.title}</h4><button onClick={()=>onOpen(detail.node.id)}>Inspect activity node</button>
   {[...detail.related_notes,...detail.related_nodes].map(({node,edge})=><article key={edge.id}><button onClick={()=>onOpen(node.id)}>{node.title}</button><p>{edge.source} · {edge.relationship_type} · {Math.round(edge.strength)}/150</p><p>{edge.reason}</p>{edge.source==="activity"&&<button disabled={busy} onClick={()=>setHidden(detail.node.id,node.id,false)}>Mute this connection</button>}</article>)}
   {!detail.connections&&<p>No retained relationships for this website.</p>}
   {!!detail.muted_connections&&<p>{detail.muted_connections} muted connection{detail.muted_connections===1?"":"s"} hidden for this node. Restore below.</p>}
  </section>}
  {connections&&!!muted.length&&<section className="connections" aria-label="Muted connections"><h4>Muted connections ({muted.length})</h4><p>Muted connections stay hidden from the graph and lists until restored. Usage totals and history are unchanged.</p>
   {muted.map(m=><article key={m.a_path+m.b_path}><p>{nodeLabel(m.a,m.a_path)} ↔ {nodeLabel(m.b,m.b_path)}</p>{m.a?.id&&m.b?.id?<button disabled={busy} onClick={()=>clearMute(m)}>Restore this connection</button>:<p>Node no longer retained; this site stays hidden if visited again.</p>}</article>)}
  </section>}
  <style jsx>{`.websites{display:flex;flex-direction:column;gap:12px;min-width:0}.filters,nav{display:flex;gap:10px;align-items:center;flex-wrap:wrap}.filters label{font-size:12px}.filters input{flex:1;min-width:140px}.website-list{list-style:none;display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,280px),1fr));gap:12px;padding:0}.website-list li,.connections{padding:14px;border:1px solid var(--border);border-radius:8px;background:var(--bg-panel);display:flex;flex-direction:column;gap:7px;min-width:0}.website-list button{text-align:left}.website-list small,.url,article p,nav{font-size:12px;color:var(--text-dim);overflow-wrap:anywhere}.connections article{padding-top:10px;border-top:1px solid var(--border)}select{background:var(--bg-panel);color:var(--text);border:1px solid var(--border);padding:6px}`}</style>
 </section>;
}
