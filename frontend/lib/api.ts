import type { ActivityConfig, ActivitySeries, ActivityStatus, ActivitySummary, ActivityUsage, Backup, Edge, GraphSnapshot, HistoryState, Note, NoteDetail, RelatedNode, SearchFilters, SearchHit, Settings, SyncResult } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
 const res = await fetch(path, { ...init, headers: { "Content-Type": "application/json", ...init?.headers }, cache: "no-store" });
 if (!res.ok) { let msg = `${res.status} ${res.statusText}`; try { const body = await res.json(); if (body?.error) msg = body.error; } catch {} throw new Error(msg); }
 return res.json() as Promise<T>;
}
const json = (method: string, body?: unknown): RequestInit => ({ method, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
const noteURL = (id: number | string) => `/api/notes/${encodeURIComponent(String(id))}`;
export const api = {
 graph: () => request<GraphSnapshot>("/api/graph"),
 node: (id: number) => request<Note>(`/api/nodes/${id}`),
 activity: () => request<ActivityStatus>("/api/activity"),
 configureActivity: (config: ActivityConfig) => request<ActivityStatus>("/api/activity/config",json("PUT",config)),
 activitySummary: (days: number) => request<ActivitySummary>(`/api/activity/summary?days=${days}`),
 activitySeries: (days: number, granularity: "day" | "week") => request<ActivitySeries>(`/api/activity/series?days=${days}&granularity=${granularity}`),
 nodeActivity: (id: number) => request<ActivityUsage>(`/api/nodes/${id}/activity`),
 activityPairing: () => request<{token: string}>("/api/activity/pairing"),
 clearActivity: () => request<{deleted: boolean}>("/api/activity/data?confirm=true",json("DELETE")),
 notes: () => request<Note[]>("/api/notes"),
 note: (id: number | string) => request<NoteDetail>(noteURL(id)),
 createNote: (title: string, content: string) => request<Note>("/api/notes", json("POST", { title, content })),
 updateNote: (id: number | string, title: string, content: string, revision: string) => request<Note>(noteURL(id), json("PUT", { title, content, revision })),
 deleteNote: (id: number | string, revision?: string) => request<{ deleted: boolean }>(noteURL(id) + (revision ? `?revision=${encodeURIComponent(revision)}` : ""), json("DELETE")),
 related: (id: number) => request<RelatedNode[]>(`/api/nodes/${id}/related`),
 edge: (id: number) => request<Edge>(`/api/edges/${id}`),
 setPosition: (id: number, x: number | null, y: number | null) => request<{ ok: boolean }>(`/api/nodes/${id}/position`, json("PUT", { x, y })),
 search: (q: string, filters: SearchFilters = {}) => request<SearchHit[]>(`/api/search?${new URLSearchParams({ q, ...filters })}`),
 sync: () => request<SyncResult>("/api/sync", json("POST")),
 settings: () => request<Settings>("/api/settings"),
 saveSettings: (settings: Settings) => request<Settings>("/api/settings", json("PUT", settings)),
 history: () => request<HistoryState>("/api/history"),
 undo: () => request<{ ok: boolean }>("/api/history/undo", json("POST")),
 redo: () => request<{ ok: boolean }>("/api/history/redo", json("POST")),
 export: () => request<Backup>("/api/export"),
 import: (backup: Backup, overwrite = false) => request<{ imported: number }>(`/api/import?overwrite=${overwrite}`, json("POST", backup)),
};
