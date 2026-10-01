# Dendrite — Local-First Knowledge Graph

Markdown notes are the source of truth. A Go API synchronizes derived metadata
into SQLite; a Next.js/React canvas reveals wikilinks and explainable inferred
relationships. Everything runs locally. No activity collection or AI service is enabled.

## Run locally

Use two terminals, each starting at the repository root:

```bash
# Terminal 1
cd backend
go run ./cmd/server
```

```bash
# Terminal 2
cd frontend
npm ci
npm run dev
```

Open <http://127.0.0.1:3000>. Both services bind to loopback by default.
Edit files directly in [notes/](notes/) or use the editor. External changes are
scanned every five seconds; the graph refreshes every six seconds. **Sync**
refreshes immediately, including the selected note inspector.

The Go toolchain version is specified in [go.mod](backend/go.mod). The frontend
uses Next.js 15, React 18, TypeScript, and npm. See
[package.json](frontend/package.json) for dependency versions.

## V1 features

- Create, edit, rename, and delete nested Markdown notes. Renames replace the
  first real H1 without discarding front matter or duplicating headings.
- Parse `[[Title]]`, `[[Title|alias]]`, `#tags`, identifiers, and simple leading
  front matter such as `project: dendrite`. Links/tags in fenced code are ignored.
- Inspect outgoing links, backlinks, entities, related notes, and explanations.
- Pan, zoom, select nodes, click an edge to inspect it, drag to pin, and
  double-click to unpin. **Fit graph** recenters the layout.
- **Browse graph** supplies keyboard-accessible node and relationship buttons.
- Stable layout across polls, circle initialization, centering, bounded simulation
  settling, spatially bucketed repulsion, and responsive narrow-screen inspectors.
- Full-text search across titles, content, tags, entities, and projects, ranked
  using SQLite FTS5/BM25. Unquoted words are ANDed prefix queries; quoted input
  is an exact phrase. Settings/tools offers exact tag/project and path-prefix filters.
- Persisted automatic-edge visibility threshold and a working weak-link toggle.
- JSON vault export/import, individual Markdown import, overwrite confirmation,
  and persistent note undo/redo.
- Awaited operations, visible failures, retryable graph loading, preserved editor
  drafts after failed saves, and revision-based edit/delete conflicts.

## Data and safety

[notes/](notes/) stores Markdown. `data/dendrite.db` stores derived nodes,
relationships, FTS indexes, revisions, positions, settings, and **up to 100 local
note-change history entries**. History contains full Markdown snapshots so deleted
notes can be restored; treat the database as private, not disposable if history
or manual positions matter. Current note content is always read from disk.

Writes use same-directory temporary files and atomic rename; creation is exclusive
and cannot silently replace an existing path. Traversal, hidden paths, non-Markdown
paths, and symlink components are rejected. Background sync and API requests are
serialized. Node/edge IDs remain stable for surviving paths/pairs; edge discovery
timestamps no longer churn. Read failures are reported rather than interpreted as
file deletion. Identical content skips metadata/index rewrites.

The editor sends the revision from its original load. A changed file returns
HTTP 409 and keeps the draft visible. Undo/redo also compares exact snapshots and
refuses to overwrite external edits. New mutations clear redo history. History
covers application note creation, editing, deletion, and import, **not** external
edits, settings, or manual positioning. Undoing a deletion recreates a node and may
assign a new ID; its former layout metadata is not restored.

### Backup and import

Open **Settings & tools → Export backup**. The versioned JSON contains exact raw
Markdown, vault-relative paths, manual x/y positions, and the visibility setting.
Import accepts that JSON or one `.md` file. It merges into the vault; unmatched
existing notes are retained. Existing paths are rejected unless overwrite is
explicitly enabled. Imports are limited to 8 MiB of JSON and 2,000 files.

Backups do not include SQLite IDs, derived relationships, or history. Relationship
metadata is rebuilt. Import note content is undoable, but imported settings and
positions are not part of note undo/redo. Export regularly; multi-file operations
have best-effort rollback, not crash-atomic filesystem/database transactions.

## Relationship engine

Each unordered note pair accumulates a raw score, capped at 150, with reasons:

| Signal | Points |
| --- | --- |
| Wikilink in either direction | 40 |
| Shared entities | weighted, capped at 25 |
| Same nonempty front-matter project | 20 |
| Shared tags | 7.5 per tag, capped at 22.5 |
| Term-frequency cosine similarity > 0.08 | up to 15 |
| Co-mentions in distinct notes (>1) | 2.5 × (count − 1), capped at 10 |
| Edit timestamps within 2 / 7 days | 10 / 6 |
| Both edited within the last week | 5 |
| More than 3 co-mentions | 5 |

Wikilink pairs have `source: explicit` and `relationship_type: wikilink`;
other pairs are `automatic`/`related`. These graph edges are **undirected**;
actual link direction is available through outgoing links/backlinks. All scored
pairs are retained. The persisted default threshold is 25: automatic edges below
it are hidden unless **Show weak links** is checked; explicit edges remain visible.

Importance is `0.55 × normalized degree + 0.45 × normalized sum(log(1+strength))`,
counting each relationship once. Only explicit or above-threshold automatic edges
contribute. Isolated importance is zero; a minimum rendered radius keeps it visible.
Showing weak links does not change importance or the saved threshold.

## API

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/health` | liveness |
| GET / POST | `/api/notes` | list / create `{title, content, path?}` |
| GET | `/api/notes/:id` | exact content, revision, tags, links, backlinks, entities |
| PUT | `/api/notes/:id` | update `{title?, content?, revision?}` |
| DELETE | `/api/notes/:id?revision=...` | delete with optional revision check |
| GET | `/api/graph` | all nodes and all retained edges |
| GET | `/api/nodes/:id` | node |
| GET | `/api/nodes/:id/related` | neighbors and connecting explanations |
| PUT | `/api/nodes/:id/position` | `{x, y}`; both `null` to unpin |
| GET | `/api/edges/:id` | relationship details |
| GET | `/api/search?q=...&tag=...&project=...&path=...&limit=...` | ranked, filtered FTS |
| POST | `/api/sync` | rescan and refresh derived graph |
| GET / PUT | `/api/settings` | `{min_auto_edge_strength: 0..150}` |
| GET | `/api/export` | portable backup JSON |
| POST | `/api/import?overwrite=false` | import backup JSON |
| GET | `/api/history` | available undo/redo labels |
| POST | `/api/history/undo` | reverse last application note change |
| POST | `/api/history/redo` | reapply next undone note change |

Note endpoints also accept a vault-relative path in place of the numeric ID.
Unsupported methods return 405; mutation JSON is limited to 8 MiB. Custom API
clients should send revisions to avoid stale writes. Omitting revision deliberately
uses the current disk content as the expected state.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DENDRITE_ADDR` | `127.0.0.1:8080` | API listen address |
| `DENDRITE_ROOT` | repository root inferred from cwd | parent of `notes/` and `data/` |
| `DENDRITE_API_ORIGIN` | `http://127.0.0.1:8080` | Next.js API proxy target |

Frontend rewrites are configured in [next.config.js](frontend/next.config.js);
set the proxy origin before starting or building Next.js. Browser requests with
non-loopback origins are rejected. This is a single-user local tool, **not an
authenticated network service**: do not bind either service publicly.

## Tests

```bash
cd backend
go test -race ./...
go vet ./...
```

```bash
cd frontend
npm ci
npm run typecheck
npm run build
npm audit
# One-time Chromium install, kept inside project node_modules:
PLAYWRIGHT_BROWSERS_PATH=0 npx playwright install chromium
PLAYWRIGHT_BROWSERS_PATH=0 npm run test:e2e
```

Backend integration tests use temporary vaults and real SQLite databases. They
cover CRUD, stable sync and edge identity, backlinks, external edits, positions,
explicit/automatic reclassification, weak edges/settings, FTS/filters, backup,
conflicts, undo/redo, method rejection, and concurrent sync/requests. Parser and
engine tests cover content fidelity, safe paths, scoring, and importance.

The Chromium suite starts its own temporary vault/API on `18080` and frontend on
`13000`, never the user's notes. It exercises the first milestone, canvas edge
selection, drag/unpin persistence, rename, failed save preservation, filters,
settings, import, undo/redo, and mobile layout. Existing servers are not reused.
Override `DENDRITE_TEST_API_PORT` / `DENDRITE_TEST_UI_PORT` if occupied.

## Remaining boundaries

- Relationship scoring still examines O(n²) pairs; retaining weak temporal edges
  can create a dense graph. No large-vault benchmark or virtualized graph guarantee.
- Revision checks are optimistic, not a lock on external editors. Multi-process
  writes and filesystem races need stronger OS-level coordination for production use.
- Title-based wikilinks assume unique titles. Renaming H1 does not rewrite backlinks
  in other Markdown files or rename the path. Front matter is intentionally simple,
  not a full YAML parser.
- Undo/redo is for notes only. Import metadata is not included in its snapshots.
- UI is tested in Chromium only; comprehensive accessibility, Safari/Firefox,
  crash-recovery, and fault-injection testing remain.
- No semantic search, collectors, AI, or 3D. Those remain V2/V3 work.

See [V1 status](V1_STATUS.md) for the current delivery and verification summary.
