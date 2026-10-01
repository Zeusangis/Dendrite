"use client";

import { useEffect, useState } from "react";
import type { NoteDetail } from "@/lib/types";
import { api } from "@/lib/api";

interface Props {
  noteId?: number; // when set, edit existing note
  onSave?: (idOrPath: number | string, title: string, content: string, revision: string) => Promise<void>;
  onCreate?: (title: string, content: string) => Promise<void>;
  onCancel: () => void;
}

export function EditorPanel({ noteId, onSave, onCreate, onCancel }: Props) {
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [loaded, setLoaded] = useState(!noteId);
  const [revision, setRevision] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setError(null);
    setLoaded(!noteId);
    if (!noteId) { setTitle(""); setContent(""); return; }
    api
      .note(noteId)
      .then((n: NoteDetail) => {
        if (!active) return;
        setRevision(n.revision);
        setTitle(n.title);
        setContent(n.content);
        setLoaded(true);
      })
      .catch((e) => { if (active) { setError(String(e)); setLoaded(true); } });
    return () => { active = false; };
  }, [noteId]);

  if (!loaded) return <div className="placeholder">Loading…</div>;
  const submit = async () => {
    setError(null);
		setBusy(true);
		try {
			if (noteId != null) {
				await onSave?.(noteId, title, content, revision);
			} else {
				await onCreate?.(title, content);
			}
		} catch (e) {
			setError(String(e));
		} finally {
			setBusy(false);
		}
	};

  return (
    <div className="editor">
      <h3>{noteId != null ? "Edit note" : "New note"}</h3>
      {error && <p role="alert" className="error">{error} Your draft is preserved. Copy it before reloading after a conflict.</p>}
      <input
        className="title"
        aria-label="Note title"
        disabled={busy}
        placeholder="Title"
        value={title}
        onChange={(e) => setTitle(e.target.value)}
      />
      <textarea
        className="body"
        aria-label="Markdown content"
        disabled={busy}
        placeholder={"Markdown body…\n\nLink other notes with [[Note Title]]\nTag with #tag"}
        value={content}
        onChange={(e) => setContent(e.target.value)}
        rows={14}
      />
      <div className="hint">
        Tip: type <code>[[</code> to link notes, <code>#tag</code> for tags.
        Connections are detected automatically.
      </div>
      <div className="btnrow">
        <button className="primary" disabled={busy || !title.trim()} onClick={submit}>
          {busy ? "Saving…" : noteId != null ? "Save" : "Create"}
        </button>
        <button disabled={busy} onClick={() => { if (!content || confirm("Discard this draft?")) onCancel(); }}>Cancel</button>
      </div>

      <style jsx>{`
        .editor {
          padding: 16px;
          display: flex;
          flex-direction: column;
          gap: 10px;
        }
        h3 {
          font-size: 14px;
        }
        .title {
          width: 100%;
          font-weight: 600;
        }
        .body {
          width: 100%;
          resize: vertical;
          min-height: 220px;
          font-family: ui-monospace, "SF Mono", Menlo, monospace;
          font-size: 13px;
          line-height: 1.5;
        }
        .hint {
          font-size: 12px;
          color: var(--text-dim);
        }
        code {
          color: var(--accent);
        }
        .btnrow {
          display: flex;
          gap: 8px;
        }
      `}</style>
    </div>
  );
}
