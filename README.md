# Dendrite

Dendrite is a local-first knowledge graph for Markdown notes. Your Markdown
vault remains the source of truth while Dendrite builds a searchable,
explainable graph of notes, links, and related activity.

Everything runs locally:

- **Backend:** Go API, SQLite persistence, file synchronization, and optional
  macOS activity collection
- **Frontend:** Next.js and React graph interface
- **Vault:** Markdown files in [`notes/`](notes/)
- **Derived data:** Local SQLite database in `data/`

The application binds to loopback addresses by default and is intended for
single-user local use.

## Features

### Notes and knowledge graph

- Create, edit, rename, delete, and organize nested Markdown notes
- Parse wikilinks, aliases, tags, entities, and simple front matter
- Browse backlinks, related notes, and relationship explanations
- Search by full text, tag, project, and path
- Drag, pin, and unpin graph nodes
- Automatically synchronize edits made outside the application

### Data management

- Export and import portable JSON backups
- Undo and redo application-managed note changes
- Preserve raw Markdown content during synchronization and backup
- Keep derived graph and activity data outside the Markdown vault

### Optional activity graph

On macOS, Dendrite can collect application usage, idle time, and optional
focused-window or browser-tab context. Activity can be paused from the
application and is disabled at startup with:

```bash
export DENDRITE_ACTIVITY_AUTOSTART=false
```

The collector does not record keystrokes, screenshots, page contents, or input
event contents. Review the privacy controls before enabling browser metadata.

## Requirements

- Go `1.27.1` or compatible version, specified in [`backend/go.mod`](backend/go.mod)
- Node.js and npm
- macOS Accessibility permission for focused-window and browser metadata
- Xcode Command Line Tools for native macOS activity collection

The macOS requirements are only needed for the optional native activity
collector and login service. Notes, graph, search, and the web application can
run without them.

## Quick start

Run the backend and frontend in separate terminals. Start both terminals from
the repository root:

### Terminal 1 — backend

```bash
cd backend
go run ./cmd/server
```

### Terminal 2 — frontend

```bash
cd frontend
npm ci
npm run dev
```

Open <http://127.0.0.1:3000>.

If your terminal is already in `backend/`, use `cd ../frontend` to reach the
frontend directory, or open a new terminal at the repository root.

The backend listens on `127.0.0.1:8080` by default. The frontend proxies API
requests to that address during development.

## Browser extension (optional)

The unpacked Chromium extension reports active-tab metadata to the local
backend. It does not send page contents or keystrokes.

1. Open Chrome, Brave, or Edge extension settings.
2. Enable **Developer mode** and choose **Load unpacked**.
3. Select [`browser-extension/`](browser-extension/).
4. In Dendrite, open **Activity → Privacy, retention & browser setup** and
   reveal the pairing token.
5. Paste the token into the extension options, select the browser name, and
   explicitly enable recording.

### Website activity and automatic connections

Open **Activity → Websites** to browse retained domains and pages, search by
page title or domain, sort by active time/recent use/total duration, and inspect
browser names, durations, sessions, last seen, and connection counts. **Connections**
shows the evidence behind browser → domain → page relationships and matching
Markdown notes. The graph's **Graph filters & legend** hides activity edges or
browser nodes without deleting notes, and can scope browsing to domains, pages,
or activity-connected notes. Existing daily/weekly comparisons remain in **Overview**.

The backend is authoritative for both native and extension metadata. It accepts
HTTP(S) URLs only and removes credentials, query strings, and fragments; hostnames,
default ports, and empty root paths are canonicalized. Other trailing slashes are
preserved because they may distinguish resources. Page contents are never read.
**URL paths and window/page titles may still contain private information.**

Note connections use conservative, case-insensitive title/tag/entity phrases or
exact host evidence. Generic words such as Home, Google, Login, and New Tab are
ignored; explicit Go-language concepts such as `golang` also recognize Go tutorials
and `go.dev`. Reasons identify the concept, evidence type, active minutes, and
sessions. Nearby usage alone does not imply a shared subject.

One relationship is displayed per unordered pair. Explicit wikilinks take
precedence over activity evidence, then ordinary inferred links, then AI evidence.
Other producers' evidence is retained so it can reappear when the winning evidence
expires. Rebuilds preserve surviving node positions and edge identities.

**Muting one connection.** Every automatic activity connection can be muted from
**Websites → Explain connections**, from **Connections**, or from the activity node
inspector. A muted connection disappears from the graph, from the website and
connection lists, and from connection counts, while its usage totals, sessions, and
Markdown notes are untouched. Muted connections are listed under **Activity →
Connections** with a restore control, so hiding a connection is always reversible.
Mutes are keyed by stable node identity rather than row IDs, so they survive graph
rebuilds; a site stays hidden if you visit it again after its activity was pruned.
Deleting activity history keeps your mutes. Muting applies only to activity-authored
connections: explicit wikilinks and ordinary inferred note relationships are never
affected by it.

Privacy settings are enforced on the backend, including subdomain exclusions.
**Saving exclusions deletes matching retained sessions**, and disabling browser
metadata or window titles strips that metadata from retained history. Projections
are rebuilt immediately. Pausing preserves retained history; retention continues
while paused. Deleting activity history preserves Markdown notes and their links.
These privacy deletions cannot be undone.

| Browser/source | Website metadata and limitations |
| --- | --- |
| macOS native sampler | Application/idle tracking; focused titles need Accessibility permission. Safari/Chromium document URLs are best effort through Accessibility; Firefox and browsers that omit that attribute may provide titles only. |
| Chrome, Brave, Edge extension | Focused HTTP(S) tab only, explicit enable/pairing required. Configure the matching browser name. Incognito tabs/windows, unfocused windows, internal URLs, and extension exclusions send suppression hints. |
| Safari / Firefox extensions | This Chromium Manifest V3 extension is not packaged for Safari or Firefox. |
| Other platforms | Native collection is unavailable. Extension hints enrich native foreground samples; they do not independently generate timed sessions. |

Native private-window detection is **best effort** based on available window metadata.
A browser that omits privacy markers can defeat native detection. Extension suppression
hints expire after 40 seconds; a disconnected/suspended extension cannot guarantee
continued native suppression. Exclude sensitive browsers/profiles or pause recording
whenever privacy is uncertain. No permission or private-browsing mode is enabled automatically.

The extension coalesces duplicate tab events, refreshes hints every five seconds
while its worker is awake, and uses a 30-second wake alarm. Backend failures back
off from five seconds to one minute with a four-second request timeout; safe status
messages show paused recording or connection errors without displaying the token.

### Local activity API

- `GET /api/activity/recent`: privacy-safe sessions
- `GET /api/activity/domains` and `/pages`: aggregated website usage
- `GET /api/activity/nodes/:id`: sanitized metadata, usage and up to 100 explained relationships
- `GET /api/activity/muted`: hidden connections with resolved nodes where they still exist
- `POST /api/activity/muted`: mute a pair by `source_id` and `target_id` (idempotent)
- `DELETE /api/activity/muted`: restore by `source_id`/`target_id`, or by `a_path`/`b_path` once a node is no longer retained
- Existing `/summary`, `/series`, and `/api/edges/:id` remain compatible

List parameters: `days` (up to 365, constrained by retention), `limit` (default 25,
maximum 100), `offset`, `domain` (includes subdomains), `app` (name/bundle ID),
`search`, and `sort=recent|duration|active`. Results have stable tie ordering.
Pairing tokens are required only for extension ingestion and must remain private.

## Optional macOS login service

The per-user [`LaunchAgent manager`](scripts/macos-login-agent.sh) can start
the backend automatically when you log in. It does not use `sudo`, modify
system-wide locations, or install itself automatically.

```bash
bash scripts/macos-login-agent.sh install
bash scripts/macos-login-agent.sh status
bash scripts/macos-login-agent.sh logs
bash scripts/macos-login-agent.sh stop
bash scripts/macos-login-agent.sh start
bash scripts/macos-login-agent.sh restart
bash scripts/macos-login-agent.sh uninstall
```

The service uses the checkout containing the script and keeps logs under
`~/Library/Logs/Dendrite/`. Keep the checkout, `notes/`, and `data/`
directories in place while it is installed.

Run the manager's safety tests without installing a service:

```bash
bash scripts/macos-login-agent.test.sh
```

## Configuration

| Variable | Description |
| --- | --- |
| `DENDRITE_ROOT` | Vault and data root used by the backend |
| `DENDRITE_ACTIVITY_AUTOSTART=false` | Disable activity recording at startup |
| `DENDRITE_ADDR` | Override the backend listen address |

Activity retention, exclusions, graph visibility, and browser pairing are
configured from the application. Treat the local SQLite database and pairing
token as private.

## Development and verification

Run backend checks from `backend/`:

```bash
go test -race ./...
go vet ./...
```

Run frontend checks from `frontend/`:

```bash
npm run typecheck
npm run build
```

Run the end-to-end suite:

```bash
npm run test:e2e
```

If Playwright browsers are not installed yet:

```bash
npx playwright install chromium
```

Run extension checks from the repository root:

```bash
node --test browser-extension/background.test.cjs
```

The website end-to-end workflow seeds **synthetic** Wikipedia, GeeksforGeeks,
GitHub, Go, and excluded-domain sessions in a temporary database. It does not visit
real sites or enable native recording. Manual checks with a live browser and OS
permissions remain separate from this automated verification.

Privacy review checklist:
- Confirm enabled metadata fields and avoid sensitive titles/URL paths.
- Configure app/domain exclusions before recording.
- Confirm extension pairing and private-window behavior for your actual browser.
- Verify pause, retention, and clear on disposable data before using real history.
- Keep the local database, pairing token, backups, and logs private.

The current end-to-end suite covers note, graph, search, conflict, history,
activity, and privacy workflows. See [`V1_STATUS.md`](V1_STATUS.md) for the
implementation status and verification history.
