"use client";

import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from "react";
import type { Edge, Note } from "@/lib/types";
import { api } from "@/lib/api";

export interface GraphHandle { focusNode: (id: number) => void; refresh: () => Promise<void>; fit: () => void }
interface SimNode extends Omit<Note, "x" | "y"> { x: number; y: number; vx: number; vy: number; fx: number | null; fy: number | null }
interface SimEdge { edge: Edge; a: SimNode; b: SimNode }
interface Props { selectedId: number | null; selectedEdgeId: number | null; onSelect: (id: number | null) => void; onSelectEdge: (edge: Edge | null) => void; showWeak: boolean; threshold: number; onStatus: (s: string) => void }
const radius = (n: SimNode) => 8 + n.importance * 16;
export const GraphCanvas = forwardRef<GraphHandle, Props>(function GraphCanvas({ selectedId, selectedEdgeId, onSelect, onSelectEdge, showWeak, threshold, onStatus }, ref) {
 const canvasRef = useRef<HTMLCanvasElement>(null);
 const wrapRef = useRef<HTMLDivElement>(null);
 const nodes = useRef(new Map<number, SimNode>());
 const edges = useRef<SimEdge[]>([]);
 const view = useRef({ scale: 1, tx: 0, ty: 0 });
 const initialized = useRef(false);
 const active = useRef(false);
 const sequence = useRef(0);
 const settle = useRef(0);
 const pending = useRef<number | null>(null);
 const hover = useRef<number | null>(null);
 const signature = useRef("");
 const [list, setList] = useState<Note[]>([]);
 const [edgeList, setEdgeList] = useState<Edge[]>([]);
 const [loading, setLoading] = useState(true);
 const [error, setError] = useState("");
 const props = useRef({ selectedId, selectedEdgeId, onSelect, onSelectEdge, showWeak, threshold, onStatus });
 props.current = { selectedId, selectedEdgeId, onSelect, onSelectEdge, showWeak, threshold, onStatus };
 const drag = useRef<{ id: number; node: SimNode | null; edge: Edge | null; startX: number; startY: number; lastX: number; lastY: number; moved: boolean } | null>(null);
 const visible = (e: Edge) => props.current.showWeak || e.source === "explicit" || e.strength >= props.current.threshold;
 const fit = useCallback(() => {
  const wrap = wrapRef.current; const all = [...nodes.current.values()]; if (!wrap || !all.length) return;
  const minX = Math.min(...all.map(n => n.x)) - 70, maxX = Math.max(...all.map(n => n.x)) + 70;
  const minY = Math.min(...all.map(n => n.y)) - 70, maxY = Math.max(...all.map(n => n.y)) + 70;
  const scale = Math.min(1.5, Math.max(0.1, Math.min(wrap.clientWidth / (maxX - minX), wrap.clientHeight / (maxY - minY)) * 0.9));
  view.current = { scale, tx: wrap.clientWidth / 2 - (minX + maxX) / 2 * scale, ty: wrap.clientHeight / 2 - (minY + maxY) / 2 * scale };
 }, []);
 const focus = useCallback((id: number) => {
  const n = nodes.current.get(id), wrap = wrapRef.current;
  if (!n || !wrap) { pending.current = id; return; }
  view.current = { scale: 1.2, tx: wrap.clientWidth / 2 - n.x * 1.2, ty: wrap.clientHeight / 2 - n.y * 1.2 };
  pending.current = null;
 }, []);
 const load = useCallback(async () => {
  const seq = ++sequence.current;
  try {
   const snap = await api.graph(); if (!active.current || seq !== sequence.current) return;
   const byID = new Map<number, SimNode>();
   snap.nodes.forEach((n, i) => {
    const old = nodes.current.get(n.id), angle = i / Math.max(1, snap.nodes.length) * 2 * Math.PI;
    const x = n.x ?? old?.x ?? 160 * Math.cos(angle), y = n.y ?? old?.y ?? 160 * Math.sin(angle);
    const sim = { ...n, x, y, vx: old?.vx ?? 0, vy: old?.vy ?? 0, fx: n.x, fy: n.y };
    // Never replace an in-progress drag with an older poll result.
    byID.set(n.id, drag.current?.node?.id === n.id ? drag.current.node : sim);
   });
   nodes.current = byID;
   edges.current = snap.edges.flatMap(edge => { const a = byID.get(edge.source_id), b = byID.get(edge.target_id); return a && b ? [{ edge, a, b }] : []; });
   const next = JSON.stringify(snap); if (next !== signature.current) { settle.current = 180; signature.current = next; }
   setList(snap.nodes); setEdgeList(snap.edges); setLoading(false); setError("");
   if (!initialized.current && snap.nodes.length) { fit(); initialized.current = true; }
   if (pending.current != null) focus(pending.current);
   props.current.onStatus(`${snap.nodes.length} notes · ${snap.edges.length} relationships`);
  } catch (e) { if (active.current && seq === sequence.current) { setError(String(e)); setLoading(false); props.current.onStatus(`Graph refresh failed: ${String(e)}`); } throw e; }
 }, [fit, focus]);
 useImperativeHandle(ref, () => ({ focusNode: focus, refresh: load, fit }), [focus, load, fit]);
 useEffect(() => {
  active.current = true; load().catch(() => {}); const timer = setInterval(() => { load().catch(() => {}); }, 6000);
  return () => { active.current = false; sequence.current++; clearInterval(timer); };
 }, [load]);
 useEffect(() => { settle.current = 120; }, [showWeak, threshold]);

 useEffect(() => {
  const wrap = wrapRef.current; if (!wrap) return;
  const observer = new ResizeObserver(entries => {
   const { width, height } = entries[0].contentRect;
   const old = (wrap as HTMLDivElement & { previousSize?: { w: number; h: number } }).previousSize;
   if (old) { view.current.tx += (width - old.w) / 2; view.current.ty += (height - old.h) / 2; }
   (wrap as HTMLDivElement & { previousSize?: { w: number; h: number } }).previousSize = { w: width, h: height };
  }); observer.observe(wrap); return () => observer.disconnect();
 }, []);

 useEffect(() => {
  let raf = 0;
  const loop = () => {
   const canvas = canvasRef.current, wrap = wrapRef.current;
   if (!canvas || !wrap) { raf = requestAnimationFrame(loop); return; }
   if (document.hidden) { raf=requestAnimationFrame(loop); return; }
   const all = [...nodes.current.values()];
   if (settle.current > 0) {
    settle.current--;
    // Spatial buckets bound repulsion for large vaults instead of all-pairs.
    const buckets = new Map<string, SimNode[]>(), cell = 240;
    for (const n of all) { const key = `${Math.floor(n.x/cell)},${Math.floor(n.y/cell)}`; const group = buckets.get(key) ?? []; group.push(n); buckets.set(key,group); }
    for (const a of all) {
     const gx = Math.floor(a.x/cell), gy = Math.floor(a.y/cell);
     for (let ix = -1; ix <= 1; ix++) for (let iy = -1; iy <= 1; iy++) {
      for (const b of buckets.get(`${gx+ix},${gy+iy}`) ?? []) {
       if (a.id >= b.id) continue;
       let dx = b.x-a.x, dy = b.y-a.y; if (dx === 0 && dy === 0) { dx = 1; dy = 0.5; }
       const d2 = Math.max(dx*dx+dy*dy, 25), d = Math.sqrt(d2), force = 2400/d2;
       a.vx -= dx/d*force; a.vy -= dy/d*force; b.vx += dx/d*force; b.vy += dy/d*force;
      }
     }
    }
    for (const { a, b, edge } of edges.current) {
     if (!visible(edge)) continue;
     const dx=b.x-a.x, dy=b.y-a.y, d=Math.max(1,Math.hypot(dx,dy)), f=(d-135)*0.012*Math.min(edge.strength/60,2);
     a.vx+=dx/d*f; a.vy+=dy/d*f; b.vx-=dx/d*f; b.vy-=dy/d*f;
    }
    for (const n of all) {
     if (n.fx!=null && n.fy!=null) { n.x=n.fx; n.y=n.fy; n.vx=n.vy=0; }
     else { n.vx=(n.vx-n.x*0.0005)*0.86; n.vy=(n.vy-n.y*0.0005)*0.86; n.x+=n.vx; n.y+=n.vy; }
    }
   }
   const dpr=window.devicePixelRatio||1,w=wrap.clientWidth,h=wrap.clientHeight;
   if(canvas.width!==Math.round(w*dpr)||canvas.height!==Math.round(h*dpr)){canvas.width=Math.round(w*dpr);canvas.height=Math.round(h*dpr);}
   const ctx=canvas.getContext("2d");if(!ctx){raf=requestAnimationFrame(loop);return;}
   ctx.setTransform(dpr,0,0,dpr,0,0);ctx.clearRect(0,0,w,h);ctx.save();ctx.translate(view.current.tx,view.current.ty);ctx.scale(view.current.scale,view.current.scale);
   for(const {edge,a,b} of edges.current){
    if(!visible(edge))continue;const selected=props.current.selectedEdgeId===edge.id||props.current.selectedId===a.id||props.current.selectedId===b.id;
    ctx.strokeStyle=selected?"#8db9ff":`rgba(110,168,254,${Math.min(0.08+edge.strength/200,0.6)})`;ctx.lineWidth=selected?2.5:1;
    ctx.setLineDash(edge.source==="explicit"?[]:[4,4]);ctx.beginPath();ctx.moveTo(a.x,a.y);ctx.lineTo(b.x,b.y);ctx.stroke();ctx.setLineDash([]);
    if(selected){ctx.font="11px system-ui";ctx.fillStyle="#dde3ee";ctx.textAlign="center";ctx.fillText(`${Math.round(edge.strength)}/150`,(a.x+b.x)/2,(a.y+b.y)/2-5);}
   }
   for(const n of all){const selected=props.current.selectedId===n.id;
    ctx.beginPath();ctx.arc(n.x,n.y,radius(n),0,Math.PI*2);ctx.fillStyle=selected?"#8db9ff":hover.current===n.id?"#6ea8fe":n.type==="app"?"#c179f2":n.type==="domain"?"#26a69a":n.type==="page"?"#e6aa45":n.type==="window"?"#8c79bd":"#3b5bdb";ctx.fill();
    if(selected||n.fx!=null){ctx.strokeStyle=selected?"#fff":"#51cf66";ctx.lineWidth=2;ctx.stroke();}
    ctx.fillStyle="#dde3ee";ctx.font="11px system-ui";ctx.textAlign="center";ctx.fillText(n.title,n.x,n.y+radius(n)+15);
   }
   ctx.restore();raf=requestAnimationFrame(loop);
  };raf=requestAnimationFrame(loop);return()=>cancelAnimationFrame(raf);
 }, []);
 const world=(x:number,y:number)=>{const r=canvasRef.current!.getBoundingClientRect(),v=view.current;return{x:(x-r.left-v.tx)/v.scale,y:(y-r.top-v.ty)/v.scale};};
 const pick=(x:number,y:number)=>{const p=world(x,y);return[...nodes.current.values()].find(n=>Math.hypot(n.x-p.x,n.y-p.y)<radius(n)+6)??null;};
 const pickEdge=(x:number,y:number)=>{
  const p=world(x,y);let best:Edge|null=null,bestD=8/view.current.scale;
  for(const {edge,a,b} of edges.current){if(!visible(edge))continue;const dx=b.x-a.x,dy=b.y-a.y,len=dx*dx+dy*dy;if(!len)continue;const t=Math.max(0,Math.min(1,((p.x-a.x)*dx+(p.y-a.y)*dy)/len));const d=Math.hypot(p.x-a.x-t*dx,p.y-a.y-t*dy);if(d<bestD){bestD=d;best=edge;}}
  return best;
 };
 const selectNode=(id:number)=>{props.current.onSelect(id);};
 const release=async(e:React.PointerEvent<HTMLCanvasElement>)=>{
  const current=drag.current;if(!current||current.id!==e.pointerId)return;drag.current=null;
  if(current.node&&current.moved){const n=current.node;try{await api.setPosition(n.id,n.fx,n.fy);}catch(err){props.current.onStatus(`Position save failed: ${String(err)}`);load().catch(()=>{});}}
  else if(!current.moved){if(current.node)selectNode(current.node.id);else{props.current.onSelectEdge(current.edge);}}
 };
 return <div ref={wrapRef} className="graphwrap">
  <canvas ref={canvasRef} aria-label="Interactive knowledge graph" onPointerDown={e=>{
   if(e.button!==0)return;e.currentTarget.setPointerCapture(e.pointerId);const node=pick(e.clientX,e.clientY);
   drag.current={id:e.pointerId,node,edge:node?null:pickEdge(e.clientX,e.clientY),startX:e.clientX,startY:e.clientY,lastX:e.clientX,lastY:e.clientY,moved:false};
  }} onPointerMove={e=>{
   hover.current=pick(e.clientX,e.clientY)?.id??null;const d=drag.current;if(!d||d.id!==e.pointerId)return;
   if(Math.hypot(e.clientX-d.startX,e.clientY-d.startY)>4)d.moved=true;
   if(d.moved){if(d.node){const p=world(e.clientX,e.clientY);d.node.x=d.node.fx=p.x;d.node.y=d.node.fy=p.y;settle.current=120;}else{view.current.tx+=e.clientX-d.lastX;view.current.ty+=e.clientY-d.lastY;}}
   d.lastX=e.clientX;d.lastY=e.clientY;
  }} onPointerUp={release} onPointerCancel={()=>{drag.current=null;load().catch(()=>{});}} onWheel={e=>{
   const r=e.currentTarget.getBoundingClientRect(),v=view.current,mx=e.clientX-r.left,my=e.clientY-r.top,s=Math.min(4,Math.max(0.1,v.scale*Math.exp(-e.deltaY*0.001)));
   v.tx=mx-(mx-v.tx)*s/v.scale;v.ty=my-(my-v.ty)*s/v.scale;v.scale=s;
  }} onDoubleClick={async e=>{const n=pick(e.clientX,e.clientY);if(!n)return;try{await api.setPosition(n.id,null,null);n.fx=n.fy=null;settle.current=180;}catch(err){props.current.onStatus(`Unpin failed: ${String(err)}`);}}}/>
  <div className="graph-tools"><button onClick={fit}>Fit graph</button><details><summary>Browse graph ({list.length})</summary><div className="browse">
   {list.map(n=><button key={n.id} onClick={()=>{selectNode(n.id);focus(n.id);}}>{n.title} {n.type!=="note"?`(${n.type})`:""}</button>)}
   {edgeList.filter(visible).map(e=><button key={`edge-${e.id}`} onClick={()=>{onSelectEdge(e);}}>Inspect relationship: {list.find(n=>n.id===e.source_id)?.title} ↔ {list.find(n=>n.id===e.target_id)?.title}</button>)}
  </div></details></div>
  {(loading||error||!list.length)&&<div className="graph-message" role="status">{loading?"Loading graph…":error?<> {error} <button onClick={()=>{load().catch(()=>{});}}>Retry</button></>:"Create a note to start your graph."}</div>}
  <style jsx>{`.graphwrap{flex:1;position:relative;min-width:0;min-height:260px;overflow:hidden;background:radial-gradient(circle at 30% 20%,rgba(59,91,219,.08),transparent 60%),var(--bg)}canvas{width:100%;height:100%;display:block;touch-action:none;cursor:grab}.graph-tools{position:absolute;left:12px;top:12px;display:flex;gap:8px;align-items:flex-start}details{background:var(--bg-panel);border:1px solid var(--border);border-radius:6px;padding:6px;max-width:300px}summary{cursor:pointer;font-size:12px}.browse{display:flex;flex-direction:column;gap:4px;max-height:250px;overflow:auto;margin-top:8px}.browse button{text-align:left;font-size:12px}.graph-message{position:absolute;left:20px;bottom:20px;background:var(--bg-panel);padding:12px;border-radius:8px;max-width:90%}`}</style>
 </div>;
});
