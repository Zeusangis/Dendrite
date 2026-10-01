// Types mirroring the Go backend API.

export interface Note {
  id: number;
  type: string;
  title: string;
  path: string;
  created_at: string;
  updated_at: string;
  importance: number;
  x: number | null;
  y: number | null;
}

export interface Edge {
  id: number;
  source_id: number;
  target_id: number;
  strength: number;
  relationship_type: string;
  source: string; // explicit | automatic | activity | ai
  reason?: string;
  created_at: string;
}

export interface GraphSnapshot {
  nodes: Note[];
  edges: Edge[];
}

export interface BacklinkInfo {
  id: number;
  title: string;
  path: string;
}

export interface NoteDetail extends Note {
  links: string[];
  backlinks: BacklinkInfo[];
  tags: string[];
  entities: { name: string; kind: string; weight: number }[];
  content: string;
  revision: string;
}

export interface RelatedNode {
  node: Note;
  edge: Edge;
  direction: "undirected";
}

export interface SearchHit {
  node: Note;
  snippet?: string;
  match_in: "title" | "content" | "tag" | "entity";
  score: number;
}

export interface ActivityConfig { enabled: boolean; window_titles: boolean; browser_pages: boolean; idle_seconds: number; retention_days: number; excluded_apps: string[]; excluded_domains: string[] }
export interface ActivityStatus { config: ActivityConfig; supported: boolean; warning: string; last_sample: string; browser_connected: boolean }
export interface ActivityUsage { node_id: number; title: string; type: string; total_seconds: number; active_seconds: number; sessions: number; last_seen: string }
export interface ActivitySession { id: number; app: string; app_id: string; window_title: string; domain: string; url: string; started_at: string; ended_at: string; total_seconds: number; active_seconds: number; samples: number }
export interface ActivitySummary { total_seconds: number; active_seconds: number; active_ratio: number; usage: ActivityUsage[]; recent: ActivitySession[] }
export interface ActivitySeriesApp { app: string; app_id: string; total_seconds: number; active_seconds: number }
export interface ActivitySeriesBucket { start: string; label: string; total_seconds: number; active_seconds: number; idle_seconds: number; context_switches: number; apps: ActivitySeriesApp[] }
export interface ActivitySeries { granularity: "day" | "week"; days: number; apps: string[]; app_ids: string[]; buckets: ActivitySeriesBucket[] }

export interface Settings { min_auto_edge_strength: number }
export interface HistoryState { undo: string; redo: string }
export interface BackupFile { path: string; content: string; x?: number | null; y?: number | null }
export interface Backup { version: number; files: BackupFile[]; settings?: Settings }
export interface SearchFilters { tag?: string; project?: string; path?: string }

export interface SyncResult {
  scanned: number;
  created: number;
  updated: number;
  deleted: number;
  edges: number;
  errors?: string[];
}
