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

Private-window detection is best effort. Exclude sensitive browsers or
profiles, or pause recording whenever privacy is uncertain.

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

The current end-to-end suite covers note, graph, search, conflict, history,
activity, and privacy workflows. See [`V1_STATUS.md`](V1_STATUS.md) for the
implementation status and verification history.
