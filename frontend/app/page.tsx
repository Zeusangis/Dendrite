"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { Backup, Edge, HistoryState, Note, SearchFilters, SearchHit, Settings } from "@/lib/types";
import { api } from "@/lib/api";
import { GraphCanvas, type GraphHandle } from "@/components/graph/GraphCanvas";
import { InspectorPanel } from "@/components/inspector/InspectorPanel";
import { EditorPanel } from "@/components/editor/EditorPanel";
import { ActivityPanel } from "@/components/activity/ActivityPanel";
import { ActivityInspector } from "@/components/activity/ActivityInspector";
import type { ActivityStatus } from "@/lib/types";

export default function Home() {
 const [search,setSearch]=useState("");const [hits,setHits]=useState<SearchHit[]>([]);
 const [filters,setFilters]=useState<SearchFilters>({tag:"",project:"",path:""});const [searching,setSearching]=useState(false);
 const [selectedId,setSelectedId]=useState<number|null>(null);const [selectedEdge,setSelectedEdge]=useState<Edge|null>(null);
 const [editing,setEditing]=useState(false);const [creating,setCreating]=useState(false);const [tools,setTools]=useState(false);
 const [showWeak,setShowWeak]=useState(false);const [settings,setSettings]=useState<Settings>({min_auto_edge_strength:25});const [threshold,setThreshold]=useState("25");
 const [history,setHistory]=useState<HistoryState>({undo:"",redo:""});const [revision,setRevision]=useState(0);
 const [status,setStatus]=useState("Loading…");const [error,setError]=useState("");const [busy,setBusy]=useState(false);
 const [backup,setBackup]=useState<Backup|null>(null);const [overwrite,setOverwrite]=useState(false);const [edgeNotes,setEdgeNotes]=useState<Note[]>([]);
 const graphRef=useRef<GraphHandle>(null);
 const [activityView,setActivityView]=useState(false),[activityStatus,setActivityStatus]=useState<ActivityStatus|null>(null),[selectedType,setSelectedType]=useState("note");
 useEffect(()=>{let active=true;const load=()=>api.activity().then(s=>{if(active)setActivityStatus(s);}).catch(e=>{if(active)setError(String(e));});load();const timer=setInterval(load,5000);return()=>{active=false;clearInterval(timer);};},[]);
 useEffect(()=>{let active=true;setSelectedType("");if(selectedId!=null)api.node(selectedId).then(n=>{if(active)setSelectedType(n.type);}).catch(e=>{if(active)setError(String(e));});return()=>{active=false;};},[selectedId,revision]);
 const dirty=editing||creating;
 useEffect(()=>{let active=true;Promise.all([api.settings(),api.history()]).then(([s,h])=>{if(active){setSettings(s);setThreshold(String(s.min_auto_edge_strength));setHistory(h);}}).catch(e=>{if(active)setError(String(e));});return()=>{active=false;};},[]);
 useEffect(()=>{
  let active=true;const hasQuery=!!search.trim()||Object.values(filters).some(Boolean);setHits([]);if(!hasQuery){setSearching(false);return;}
  setSearching(true);const timer=setTimeout(()=>{api.search(search,filters).then(r=>{if(active)setHits(r);}).catch(e=>{if(active)setError(`Search failed: ${String(e)}`);}).finally(()=>{if(active)setSearching(false);});},250);
  return()=>{active=false;clearTimeout(timer);};
 },[search,filters,revision]);
 useEffect(()=>{if(!selectedEdge){setEdgeNotes([]);return;}let active=true;
  Promise.all([api.edge(selectedEdge.id),api.graph().then(s=>s.nodes)]).then(([edge,list])=>{if(active){setSelectedEdge(edge);setEdgeNotes(list);}}).catch(e=>{if(active){setError(String(e));setSelectedEdge(null);}});
  return()=>{active=false;};
 // IDs remain stable across unchanged syncs; revision refreshes explanations.
 // eslint-disable-next-line react-hooks/exhaustive-deps
 },[selectedEdge?.id,revision]);
 const leaveDraft=()=>!dirty||confirm("Discard the open draft?");
 const selectNode=useCallback((id:number|null)=>{if(dirty&&!confirm("Discard the open draft?"))return;setSelectedId(id);setSelectedEdge(null);setEditing(false);setCreating(false);},[dirty]);
 const selectEdge=useCallback((edge:Edge|null)=>{if(dirty&&!confirm("Discard the open draft?"))return;setSelectedEdge(edge);setSelectedId(null);setEditing(false);setCreating(false);},[dirty]);
 const focusNode=(id:number)=>{if(!leaveDraft())return;setSelectedId(id);setSelectedEdge(null);setEditing(false);setCreating(false);setActivityView(false);graphRef.current?.focusNode(id);};
 const refresh=useCallback(async()=>{setRevision(n=>n+1);const [h]=await Promise.all([api.history(),graphRef.current?.refresh()]);setHistory(h);},[]);
 const handleCreate=async(title:string,content:string)=>{const note=await api.createNote(title,content);setCreating(false);setSelectedId(note.id);setSelectedEdge(null);setStatus(`Created “${note.title}”`);try{await refresh();graphRef.current?.focusNode(note.id);}catch(e){setError(`Created, but refresh failed: ${String(e)}`);}};
 const handleSave=async(id:number|string,title:string,content:string,rev:string)=>{await api.updateNote(id,title,content,rev);setEditing(false);setStatus("Saved");try{await refresh();}catch(e){setError(`Saved, but refresh failed: ${String(e)}`);}};
 const handleDelete=async(id:number|string,rev:string)=>{await api.deleteNote(id,rev);setSelectedId(null);setEditing(false);setStatus("Deleted — Undo is available");try{await refresh();}catch(e){setError(`Deleted, but refresh failed: ${String(e)}`);}};
 const run=async(action:()=>Promise<void>)=>{setBusy(true);setError("");try{await action();}catch(e){setError(String(e));}finally{setBusy(false);}};
 const exportBackup=async()=>{const data=await api.export();const url=URL.createObjectURL(new Blob([JSON.stringify(data,null,2)],{type:"application/json"}));const a=document.createElement("a");a.href=url;a.download=`dendrite-${new Date().toISOString().slice(0,10)}.json`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);setStatus(`Exported ${data.files.length} Markdown notes`);};
 const readImport=async(file:File)=>{
  if(file.size>8*1024*1024)throw new Error("Import limit is 8 MiB");
  if(file.name.toLowerCase().endsWith(".md")){setBackup({version:1,files:[{path:file.name,content:await file.text()}]});}
  else{const parsed=JSON.parse(await file.text()) as Backup;if(parsed.version!==1||!Array.isArray(parsed.files))throw new Error("Expected a Dendrite backup JSON or a Markdown file");setBackup(parsed);}
  setOverwrite(false);
 };
 const hasQuery=!!search.trim()||Object.values(filters).some(Boolean);
 return <div className="app">
  <header className="topbar"><div className="brand"><strong>Dendrite</strong><span className="dim">Local knowledge graph</span></div>
   <div className="searchwrap"><input aria-label="Search notes" className="search" placeholder={'Search words or "exact phrase"…'} value={search} onChange={e=>setSearch(e.target.value)}/>
    {hasQuery&&<div className="results" role="region" aria-label="Search results">{searching?<p>Searching…</p>:!hits.length?<p>No matching notes.</p>:hits.map(h=><button key={h.node.id} className="result" onClick={()=>{focusNode(h.node.id);setSearch("");setFilters({tag:"",project:"",path:""});}}><strong>{h.node.title}</strong><small>{h.match_in} · {h.node.path}</small>{h.snippet&&<span>{h.snippet}</span>}</button>)}</div>}
   </div>
   <div className="actions"><button disabled={busy} onClick={()=>{if(!leaveDraft())return;setCreating(true);setEditing(false);setSelectedEdge(null);}}>＋ New note</button>
    <button disabled={busy} onClick={()=>run(async()=>{const r=await api.sync();await refresh();setStatus(`Synced ${r.scanned} notes and ${r.edges} relationships`);})}>Sync</button>
    <button aria-expanded={activityView} onClick={()=>{if(!leaveDraft())return;setCreating(false);setEditing(false);setSelectedId(null);setSelectedEdge(null);setActivityView(!activityView);}}>Activity {activityStatus?.config.enabled?"●":"○"}</button>
    <button aria-expanded={tools} onClick={()=>setTools(!tools)}>Settings & tools</button></div>
  </header>
  {error&&<div className="errorbar" role="alert">{error}<button onClick={()=>setError("")}>Dismiss</button></div>}
  {tools&&<section className="tools" aria-label="Settings and vault tools">
   <fieldset disabled={busy}><legend>Relationship visibility</legend><label>Automatic edge threshold (0–150)<input aria-label="Automatic edge threshold" type="number" min="0" max="150" value={threshold} onChange={e=>setThreshold(e.target.value)}/></label>
    <button onClick={()=>run(async()=>{const value=Number(threshold);if(threshold.trim()===""||!Number.isFinite(value)||value<0||value>150)throw new Error("Choose a threshold from 0 to 150");const s=await api.saveSettings({min_auto_edge_strength:value});setSettings(s);await refresh();setStatus("Settings saved");})}>Save settings</button><p className="dim">Weak relationships are retained. Explicit links are always visible.</p></fieldset>
   <fieldset disabled={busy}><legend>Search filters</legend>{(["tag","project","path"] as const).map(key=><label key={key}>{key}<input aria-label={`Filter by ${key}`} value={filters[key]??""} onChange={e=>setFilters(f=>({...f,[key]:e.target.value}))}/></label>)}<button onClick={()=>setFilters({tag:"",project:"",path:""})}>Clear filters</button></fieldset>
   <fieldset disabled={busy}><legend>Portable vault backup</legend><button onClick={()=>run(exportBackup)}>Export backup</button><label>Import Markdown or backup<input aria-label="Import vault" type="file" accept=".json,.md" onChange={e=>{const file=e.target.files?.[0];if(file)run(()=>readImport(file));e.target.value="";}}/></label>
    {backup&&<div><p>Ready to import {backup.files.length} notes.</p><label><input type="checkbox" checked={overwrite} onChange={e=>setOverwrite(e.target.checked)}/>Allow overwriting existing paths</label><button onClick={()=>run(async()=>{if(overwrite&&!confirm("Overwrite matching notes? Export a backup first if needed."))return;const r=await api.import(backup,overwrite);setBackup(null);await refresh();const s=await api.settings();setSettings(s);setThreshold(String(s.min_auto_edge_strength));setStatus(`Imported ${r.imported} notes`);})}>Import {backup.files.length} notes</button><button onClick={()=>setBackup(null)}>Cancel import</button></div>}
    <p className="dim">Includes raw Markdown, graph positions, and settings. Treat backups as private.</p></fieldset>
  </section>}
  <main className="workspace">{activityView?<ActivityPanel onClose={()=>setActivityView(false)} onOpen={focusNode} onChanged={async()=>{setActivityStatus(await api.activity());await refresh();}}/>:<><aside className="sidebar"><label className="toggle"><input type="checkbox" checked={showWeak} onChange={e=>setShowWeak(e.target.checked)}/>Show weak links</label>
   <div className="tracking"><small>{activityStatus?.config.enabled?"● Recording activity":"○ Activity stopped"}</small><button disabled={!activityStatus} onClick={()=>run(async()=>{if(!activityStatus)return;setActivityStatus(await api.configureActivity({...activityStatus.config,enabled:!activityStatus.config.enabled}));})}>{activityStatus?.config.enabled?"Stop recording":"Resume recording"}</button></div>
   <div className="legend"><strong>Importance</strong><p>● Small: isolated</p><p>⬤ Large: well connected</p><p className="dim">55% connections + 45% strength.<br/>Solid edges: wikilinks.<br/>Dashed edges: inferred.<br/>Green ring: pinned.</p></div>
   <div className="history"><button disabled={busy||dirty||!history.undo} title={history.undo} onClick={()=>run(async()=>{await api.undo();setSelectedId(null);setSelectedEdge(null);await refresh();setStatus("Undone");})}>Undo</button><button disabled={busy||dirty||!history.redo} title={history.redo} onClick={()=>run(async()=>{await api.redo();setSelectedId(null);setSelectedEdge(null);await refresh();setStatus("Redone");})}>Redo</button><small>{history.undo&&`Undo: ${history.undo}`}</small></div>
   <div className="status" role="status">{busy?"Working…":status}</div></aside>
   <GraphCanvas ref={graphRef} selectedId={selectedId} selectedEdgeId={selectedEdge?.id??null} onSelect={selectNode} onSelectEdge={selectEdge} showWeak={showWeak} threshold={settings.min_auto_edge_strength} onStatus={setStatus}/>
   <aside className={`inspector ${creating||editing||selectedId!=null||selectedEdge?"open":""}`}>
    {(creating||editing||selectedId!=null||selectedEdge)&&<button className="close-panel" disabled={busy} onClick={()=>{if(!leaveDraft())return;setSelectedId(null);setSelectedEdge(null);setCreating(false);setEditing(false); }}>Close panel</button>}
    {creating?<EditorPanel key="create" onCreate={handleCreate} onCancel={()=>setCreating(false)}/>:editing&&selectedId!=null?<EditorPanel key={`edit-${selectedId}`} noteId={selectedId} onSave={handleSave} onCancel={()=>setEditing(false)}/>:selectedEdge?<div className="edge-panel"><h2>Relationship</h2><div className="edge-endpoints">{[selectedEdge.source_id,selectedEdge.target_id].map(id=><button key={id} onClick={()=>focusNode(id)}>{edgeNotes.find(n=>n.id===id)?.title??`Node ${id}`}</button>)}</div><p><strong>{Math.round(selectedEdge.strength)} / 150</strong> · {selectedEdge.source}</p><p className="dim">{selectedEdge.relationship_type} · discovered {new Date(selectedEdge.created_at).toLocaleString()}</p><h3>Why these notes are related</h3><ul>{selectedEdge.reason?.split("; ").map(reason=><li key={reason}>{reason}</li>)}</ul><p className="dim">Relationships are undirected; outgoing wikilinks and backlinks appear in note details.</p></div>:selectedId!=null&&selectedType&&selectedType!=="note"?<ActivityInspector key={selectedId} nodeId={selectedId} onOpen={focusNode}/>:selectedId!=null&&selectedType==="note"?<InspectorPanel key={`${selectedId}-${revision}`} noteId={selectedId} onEdit={()=>setEditing(true)} onDelete={handleDelete} onOpenRelated={focusNode}/>:<div className="placeholder"><p>Select a node or edge to inspect it.</p><p className="dim">Drag to pin, double-click to unpin, scroll to zoom. Browse graph offers keyboard navigation.</p></div>}
   </aside></>}</main>
  <style jsx>{`.app{display:flex;flex-direction:column;height:100dvh}.topbar{display:flex;align-items:center;gap:16px;padding:10px 16px;background:var(--bg-panel);border-bottom:1px solid var(--border);flex-wrap:wrap}.brand{display:flex;flex-direction:column;white-space:nowrap}.brand strong{font-size:18px}.brand span{font-size:11px}.searchwrap{position:relative;flex:1;min-width:180px;max-width:550px}.search{width:100%}.actions{display:flex;gap:8px;margin-left:auto;flex-wrap:wrap}.results{position:absolute;top:38px;left:0;right:0;background:var(--bg-panel);border:1px solid var(--border);border-radius:8px;z-index:50;max-height:400px;overflow:auto;box-shadow:0 8px 24px #0008}.results p{padding:12px}.result{display:flex;flex-direction:column;gap:4px;width:100%;border:0;border-bottom:1px solid var(--border);border-radius:0;text-align:left;background:transparent;padding:10px}.result small,.result span{color:var(--text-dim);font-size:12px}.result span{overflow:hidden;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical}.workspace{display:flex;flex:1;min-height:0;position:relative}.sidebar{width:190px;flex:none;border-right:1px solid var(--border);background:var(--bg-panel);padding:12px;display:flex;flex-direction:column;gap:18px;overflow:auto}.toggle{display:flex;gap:8px;align-items:center}.tracking{display:flex;flex-direction:column;gap:6px}.tracking small{font-size:11px;color:var(--text-dim)}.legend{font-size:12px;line-height:1.8}.history{display:flex;flex-wrap:wrap;gap:6px}.history small{width:100%;color:var(--text-dim)}.status{margin-top:auto;font-size:12px;color:var(--text-dim)}.inspector{width:340px;flex:none;border-left:1px solid var(--border);background:var(--bg-panel);overflow:auto}.placeholder,.edge-panel{padding:20px;display:flex;flex-direction:column;gap:14px}.edge-panel h2{font-size:18px}.edge-panel h3{font-size:13px}.edge-panel ul{padding-left:18px;display:flex;flex-direction:column;gap:8px}.edge-endpoints{display:flex;gap:8px;flex-wrap:wrap}.dim{color:var(--text-dim);font-size:12px}.close-panel{margin:10px 16px 0}.errorbar{background:#331b24;color:var(--danger);padding:10px 16px;display:flex;gap:12px;justify-content:space-between;align-items:center}.tools{display:flex;gap:16px;flex-wrap:wrap;padding:16px;background:var(--bg-panel);border-bottom:1px solid var(--border);max-height:45vh;overflow:auto}.tools fieldset{flex:1;min-width:220px;display:flex;flex-direction:column;align-items:flex-start;gap:8px;border:1px solid var(--border);padding:12px;border-radius:6px}.tools label{display:flex;gap:8px;align-items:center;flex-wrap:wrap;font-size:12px}.tools input:not([type=checkbox]){max-width:190px}.tools legend{padding:0 6px}.tools p{font-size:12px;line-height:1.5}@media(max-width:1000px){.sidebar{width:155px}.inspector{width:300px}.brand span{display:none}}@media(max-width:760px){.topbar{gap:8px;padding:10px}.actions{margin-left:0}.searchwrap{max-width:none}.sidebar{position:absolute;bottom:8px;left:8px;width:auto;max-width:calc(100% - 16px);z-index:2;border:1px solid var(--border);border-radius:8px;padding:8px;gap:6px}.legend,.history small{display:none}.history{display:inline-flex}.status{max-width:190px}.inspector{display:none}.inspector.open{display:block;position:absolute;right:0;top:0;bottom:0;width:min(340px,88vw);z-index:5;box-shadow:-8px 0 24px #0008}.tools{max-height:40vh}}`}</style>
 </div>;
}
