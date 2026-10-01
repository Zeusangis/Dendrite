"use client";

import { useCallback, useEffect, useState } from "react";
import type { NoteDetail, RelatedNode } from "@/lib/types";
import { api } from "@/lib/api";

interface Props {
  noteId: number;
  onEdit: () => void;
  onDelete: (idOrPath: number | string, revision: string) => Promise<void>;
  onOpenRelated: (id: number, edit?: boolean) => void;
}

export function InspectorPanel({ noteId, onEdit, onDelete, onOpenRelated }: Props) {
  const [note, setNote] = useState<NoteDetail | null>(null);
  const [related, setRelated] = useState<RelatedNode[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [expandedEdge, setExpandedEdge] = useState<number | null>(null);

  const load = useCallback(async () => {
    try {
      const [n, r] = await Promise.all([
        api.note(noteId),
        api.related(noteId),
      ]);
      setNote(n);
      setRelated(r);
      setError(null);
    } catch (e) {
      setError(String(e));
    }
  }, [noteId]);

  useEffect(() => {
    let active = true;
    setNote(null);
    setError(null);
    Promise.all([api.note(noteId), api.related(noteId)]).then(([n,r]) => { if (active) { setNote(n); setRelated(r); } }).catch(e => { if (active) setError(String(e)); });
    return () => { active = false; };
  }, [load]);

  if (error && !note) {
    return <div className="placeholder" role="alert">Error: {error}<button onClick={load}>Retry</button></div>;
  }
  if (!note) {
    return <div className="placeholder">Loading…</div>;
  }

  const created = new Date(note.created_at).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
  const edited = new Date(note.updated_at).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });

  return (
    <div className="inspector-content">
      <header>
        <h2>{note.title}</h2>
        {error && <p className="error" role="alert">{error}</p>}
        <div className="meta">
          Importance: {Math.round(note.importance * 100)}%
        </div>
        <div className="meta dim">
          Created {created} · Edited {edited}
        </div>
        <div className="btnrow">
          <button onClick={onEdit}>Edit</button>
          <button
            className="danger"
            disabled={busy}
            onClick={async () => {
              if (!confirm(`Delete "${note.title}"? You can undo this.`)) return;
              setBusy(true); setError(null);
              try { await onDelete(note.id, note.revision); } catch (e) { setError(String(e)); } finally { setBusy(false); }
            }}
          >
            Delete
          </button>
        </div>
      </header>

      {note.tags.length > 0 && (
        <section>
          <h3>Tags</h3>
          <div className="tags">
            {note.tags.map((t) => (
              <span key={t} className="tag">
                #{t}
              </span>
            ))}
          </div>
        </section>
      )}

      <section>
        <h3>Outgoing links</h3>
        {note.links.length === 0 ? (
          <p className="dim">None</p>
        ) : (
          <ul>
            {note.links.map((l) => (
              <li key={l} className="dim">
                {l}
              </li>
            ))}
          </ul>
        )}
      </section>

      {note.backlinks.length > 0 && (
        <section>
          <h3>Backlinks</h3>
          <ul>
            {note.backlinks.map((b) => (
              <li key={b.id}>
                <button className="linklike" onClick={() => onOpenRelated(b.id)}>
                  {b.title}
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section>
        <h3>Connected ({related.length})</h3>
        {related.length === 0 && <p className="dim">No relationships yet.</p>}
        <ul className="related">
          {related.map(({ node, edge, direction }) => (
            <li key={edge.id}>
              <button
                className="related-item"
                onClick={() => onOpenRelated(node.id)}
              >
                <span className="related-title">
                  {node.title}
                  <span className="dir"> ↔</span>
                </span>
                <span className="strength">
                  {Math.round(edge.strength)}
                  {edge.source === "automatic" ? " · auto" : ""}
                </span>
              </button>
              {edge.reason && (
                <div className="why">
                  <button
                    className="why-toggle"
                    onClick={() =>
                      setExpandedEdge(expandedEdge === edge.id ? null : edge.id)
                    }
                  >
                    Why? {expandedEdge === edge.id ? "▾" : "▸"}
                  </button>
                  {expandedEdge === edge.id && (
                    <ul className="why-list">
                      {edge.reason.split("; ").map((r, i) => (
                        <li key={i}>{r}</li>
                      ))}
                    </ul>
                  )}
                </div>
              )}
            </li>
          ))}
        </ul>
      </section>

      {note.entities.length > 0 && <section><h3>Entities</h3><div className="tags">{note.entities.map(entity => <span key={entity.name} className="tag" title={`${entity.kind} · weight ${entity.weight}`}>{entity.name}</span>)}</div></section>}

      <section>
        <h3>Content</h3>
        <pre className="content">{note.content}</pre>
      </section>

      <style jsx>{`
        .inspector-content {
          padding: 16px;
          display: flex;
          flex-direction: column;
          gap: 18px;
        }
        header h2 {
          font-size: 17px;
          margin-bottom: 6px;
        }
        .meta {
          font-size: 12px;
          color: var(--text-dim);
        }
        .dim {
          color: var(--text-dim);
        }
        .btnrow {
          display: flex;
          gap: 8px;
          margin-top: 10px;
        }
        .danger {
          color: var(--danger);
        }
        h3 {
          font-size: 11px;
          text-transform: uppercase;
          letter-spacing: 0.08em;
          color: var(--text-dim);
          margin-bottom: 6px;
        }
        section {
          border-top: 1px solid var(--border);
          padding-top: 12px;
        }
        .tags {
          display: flex;
          flex-wrap: wrap;
          gap: 6px;
        }
        .tag {
          background: var(--bg-hover);
          border: 1px solid var(--border);
          border-radius: 20px;
          padding: 2px 10px;
          font-size: 12px;
          color: var(--accent);
        }
        ul {
          list-style: none;
          display: flex;
          flex-direction: column;
          gap: 4px;
        }
        .related {
          gap: 8px;
        }
        .linklike {
          background: none;
          border: none;
          padding: 0;
          color: var(--accent);
          cursor: pointer;
        }
        .linklike:hover {
          text-decoration: underline;
        }
        .related-item {
          display: flex;
          justify-content: space-between;
          width: 100%;
          background: var(--bg-hover);
          border: 1px solid var(--border);
          padding: 6px 10px;
          border-radius: 6px;
        }
        .related-item:hover {
          border-color: var(--accent-dim);
        }
        .related-title {
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
        }
        .dir {
          color: var(--text-dim);
        }
        .strength {
          color: var(--text-dim);
          font-size: 12px;
          margin-left: 8px;
        }
        .why {
          margin: 4px 0 0 10px;
        }
        .why-toggle {
          background: none;
          border: none;
          padding: 0;
          color: var(--text-dim);
          font-size: 12px;
          cursor: pointer;
        }
        .why-toggle:hover {
          color: var(--accent);
        }
        .why-list {
          margin: 6px 0 0 4px;
          gap: 2px;
        }
        .why-list li {
          font-size: 12px;
          color: var(--text-dim);
        }
        .why-list li::before {
          content: "• ";
          color: var(--accent);
        }
        .content {
          font-size: 12px;
          line-height: 1.5;
          white-space: pre-wrap;
          word-break: break-word;
          color: var(--text-dim);
          max-height: 260px;
          overflow-y: auto;
          background: var(--bg);
          border: 1px solid var(--border);
          border-radius: 6px;
          padding: 10px;
        }
      `}</style>
    </div>
  );
}
