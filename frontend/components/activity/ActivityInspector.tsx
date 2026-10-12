"use client";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { ActivityNodeDetail } from "@/lib/types";
import { duration } from "./ActivityPanel";
export function ActivityInspector({
  nodeId,
  onOpen,
  onChanged,
}: {
  nodeId: number;
  onOpen: (id: number) => void;
  onChanged?: () => Promise<void> | void;
}) {
  const [detail, setDetail] = useState<ActivityNodeDetail | null>(null), [error, setError] = useState("");
  const [revision, setRevision] = useState(0), [busy, setBusy] = useState(false);
  const node = detail?.node, usage = detail;
  const related = [...(detail?.related_notes ?? []), ...(detail?.related_nodes ?? [])];
  useEffect(() => {
    let active = true;
    setDetail(null);
    const load = () =>
      api.activityNode(nodeId)
        .then(d => {
          if (active) {
            setDetail(d);
            setError("");
          }
        })
        .catch((e) => {
          if (active) setError(String(e));
        });
    load();
    const timer = setInterval(load, 5000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [nodeId, revision]);
  const mute = async (otherId: number) => {
    setBusy(true);
    try {
      await api.muteConnection(nodeId, otherId);
      setRevision(n => n + 1);
      setError("");
      await onChanged?.();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="activity-inspector">
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {node && usage ? (
        <>
          <h2>{node.title}</h2>
          <p>{node.type} · automatically generated</p>
          <dl>
            {detail?.domain && <><dt>Domain</dt><dd>{detail.domain}</dd></>}
            {detail?.url && <><dt>Sanitized URL</dt><dd><a href={detail.url} target="_blank" rel="noreferrer">{detail.url}</a></dd></>}
            <dt>Applications</dt><dd>{detail?.browsers.join(", ")}</dd>
            <dt>First seen</dt><dd>{detail && new Date(detail.first_seen).toLocaleString()}</dd>
            <dt>Total foreground time</dt>
            <dd>{duration(usage.total_seconds)}</dd>
            <dt>Active time</dt>
            <dd>{duration(usage.active_seconds)}</dd>
            <dt>Active ratio</dt>
            <dd>
              {Math.round(
                usage.total_seconds
                  ? (usage.active_seconds / usage.total_seconds) * 100
                  : 0,
              )}
              %
            </dd>
            <dt>Sessions</dt>
            <dd>{usage.sessions}</dd>
            <dt>Last used</dt>
            <dd>{new Date(usage.last_seen).toLocaleString()}</dd>
          </dl>
          <p>
            Active means system input was recent, not proof of attention or
            productivity.
          </p>
          <h3>Related pages, applications & notes</h3>
          {!related.length && <p>No retained connections.</p>}
          {related.map(({ node: n, edge }) => (
            <section key={edge.id}>
              <button onClick={() => onOpen(n.id)}>
                {n.title} · {Math.round(edge.strength)}/150
              </button>
              <p>{edge.source} · {edge.relationship_type}</p>
              <ul>
                {edge.reason?.split("; ").map((reason) => (
                  <li key={reason}>{reason}</li>
                ))}
              </ul>
              {edge.source === "activity" && (
                <button disabled={busy} onClick={() => mute(n.id)}>
                  Mute this connection
                </button>
              )}
            </section>
          ))}
          {!!detail?.muted_connections && (
            <p>
              {detail.muted_connections} muted connection
              {detail.muted_connections === 1 ? "" : "s"} hidden here. Restore them
              from the Activity Connections tab.
            </p>
          )}
        </>
      ) : (
        <p>Loading usage…</p>
      )}
      <style jsx>{`
        .activity-inspector {
          padding: 20px;
          display: flex;
          flex-direction: column;
          gap: 14px;
          overflow-wrap: anywhere;
        }
        h2 {
          font-size: 18px;
        }
        h3 {
          font-size: 14px;
        }
        p,
        dt,
        li {
          font-size: 12px;
          color: var(--text-dim);
        }
        dl {
          display: grid;
          grid-template-columns: 1fr 1fr;
          gap: 8px;
        }
        dd {
          text-align: right;
        }
        section {
          border-top: 1px solid var(--border);
          padding-top: 10px;
        }
        section button {
          width: 100%;
          text-align: left;
        }
        ul {
          padding-left: 18px;
          margin-top: 6px;
          display: flex;
          flex-direction: column;
          gap: 4px;
        }
      `}</style>
    </div>
  );
}
