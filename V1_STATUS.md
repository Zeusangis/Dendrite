# Dendrite V1 Status

**Updated:** 2026-10-01

The original audit's nine recommended corrections have been implemented, alongside
V1 search and polish features. This is a much more complete local single-user V1,
not a claim of production readiness or completion of V2/V3.

## Delivered

### Notes and integrity

- Lossless raw Markdown reads, preserving front matter and avoiding duplicate H1s.
- Title changes replace the first real H1; filenames and incoming link text are
  intentionally left unchanged.
- Exclusive creation prevents slug/path collision overwrites.
- Same-directory atomic writes, strict vault paths, and symlink rejection.
- Simple project front matter, aliases, deduplication, tags, entities, real H1
  extraction, and fenced-code exclusion for links/tags.
- Revision-based stale-save/delete rejection with HTTP 409.
- Frontend create/save/delete await requests and preserve failed-save drafts.

### Sync and relationships

- Serialized API/background synchronization and stable node upsert IDs.
- Stable canonical unordered edge pairs, IDs, and discovery timestamps.
- Explicit wikilinks stored as explicit/wikilink, including correct reclassification
  when a link is removed; stale derived relationships are reconciled.
- Duplicate co-mention pass removed; repeated-frequency bonuses are explained.
- Weak edges are retained and can actually be revealed by the UI.
- Persisted 0–150 automatic visibility threshold; explicit edges always visible.
- Importance matches documentation: 55% normalized degree + 45% normalized
  log-weighted strength, counting each above-threshold/explicit pair once.
- Content hashing avoids reindexing unchanged notes; time-based scores refresh
  at least once per minute. Sync/read failures are surfaced.

### Graph and UI

- Correct React forwarded imperative graph handle; immediate refresh after mutations.
- Deterministic circle initialization after data load, viewport centering, fit/focus,
  preserved layout across polls, and bounded force settling.
- Spatially bucketed frontend repulsion and hidden-tab rendering suppression.
- Real segment-distance canvas edge hit testing plus dedicated edge inspector/API.
- Pointer-based pan/drag with capture and persisted positions; unpin saves nulls.
- Keyboard-accessible node/edge browsing, responsive layout, and narrow-screen panels.
- Inspector refresh after edits/sync, safe loading state, errors/retries, draft discard
  confirmation, entities, tags, backlinks, and relationship explanations.

### Search, tools, and safety

- SQLite FTS5/BM25 indexing for titles/content/tags/entities/projects.
- ANDed word-prefix and quoted phrase queries; exact tag/project and path-prefix filters.
- Settings UI/API, raw Markdown JSON backup/export, JSON merge import and single
  Markdown import, validation and explicit overwrite opt-in.
- Backup includes manual positions and visibility settings, not IDs/history.
- Persistent undo/redo for note CRUD and import (100 change entries), conflict checks
  against external edits, redo invalidation, and best-effort rollback.
- Strict API method handling, bounded JSON bodies, loopback defaults, origin checks,
  HTTP timeouts, graceful server shutdown, and configurable root/proxy origins.
- Approved upgrade from Next.js 14 to 15.5.27; patched PostCSS override.

## Verified

Passed against the final implementation:

- `cd backend && go test -race ./...`
- `cd backend && go vet ./...`
- `cd frontend && npm run typecheck`
- `cd frontend && npm run build`
- `cd frontend && npm audit` — zero vulnerabilities reported.
- `cd frontend && PLAYWRIGHT_BROWSERS_PATH=0 npm run test:e2e`

Backend temporary-vault/SQLite integration tests cover CRUD, sync, backlinks,
external edits, search/filters/phrases, graph/edge retrieval, position persistence,
unpin, source reclassification, weak-edge retention, thresholds, raw content,
rename, collisions, stale writes, backup/import, undo/redo, origin/method rejection,
and concurrent sync/requests. Parser/vault and engine/importance scoring tests pass.

The isolated Chromium suite covers create two notes → link → sync/graph → direct
canvas edge inspection, rename, drag/unpin persistence, conflict draft preservation,
search filters, settings, Markdown import, undo/redo, and mobile width with no
horizontal overflow or browser runtime errors. Desktop/mobile screenshots were
also inspected in the browser panel. The suite uses temporary notes, not the
user's vault. No user note files were modified by validation.

## Remaining limitations

These are deliberate limits or further hardening work, not silently completed:

- Backend relationship pair scoring remains O(n²). Temporal weak edges can make
  large vaults dense; large-scale benchmarks and candidate pruning remain.
- Optimistic conflict checks do not lock external editors. Multi-file/SQLite
  mutations are not crash-atomic; rollback is best-effort. Stronger OS-level
  coordination and failure-injection/crash-recovery testing remain.
- Note undo/redo does not restore positions/settings, including imported metadata.
  Restoring a deleted note may produce a new ID and does not recover old graph pins.
- Export does not include change history. History contains private Markdown
  snapshots, including deleted content, in the local database.
- Title-based wikilinks assume unique titles; rename does not automatically rewrite
  incoming wikilinks or rename filenames. Front matter parsing is not full YAML.
- Search is lexical FTS, not semantic search or an arbitrary query language.
- Coverage is Chromium plus Go integration tests. Safari/Firefox, comprehensive
  accessibility, sustained performance, and broader fault injection remain.
- Local single-user service only; no network authentication or public deployment.

## Added after V1: requested activity-tracking slice

- macOS Cocoa/CoreGraphics foreground-app and idle sampler; authorized Accessibility
  window-title/document-URL sampling, explicit permission warnings, unsupported
  platform fallback, and no keystroke/screenshot/page-content capture.
- Initially enabled with the user's opt-in; persistent Stop/Resume, privacy toggles,
  app/domain exclusions, URL redaction, lock/private suppression, bounded intervals,
  sleep-gap exclusion, retention and explicit irreversible data deletion.
- SQLite sessions and automatic app/window/domain/page graph projections with
  evidence-based strengths, context switches, note-concept relationships, and usage
  inspectors. No generated activity content is written into Markdown notes.
- Activity dashboard: total/active/idle time, ratio, daily and Monday-based weekly
  comparisons for app usage, active/idle time, and context switches; selectable
  ranges, accessible exact-data tables, recent timeline, privacy controls, and pairing.
- Opt-in Chromium MV3 integration: authenticated local hints, focused active tabs
  only, private suppression, domain exclusions, and no content scripts.
- Backend deterministic activity and API integration tests, extension privacy tests,
  and a Chromium dashboard workflow for day/week charts, metric selection, and privacy
  controls. Tests disable live recording.

Native sampling compiles and deterministic workflows pass; live macOS permission
configuration and real extension installation were not performed. The chart-specific
Playwright workflow was added, but could not run in this checkout because its
Chromium headless executable is not installed (launch failed before test execution).
Background
tracking runs while the backend is running; an optional per-user macOS LaunchAgent
can start it at Aqua login and is never installed automatically. Private-window
detection without the extension is best-effort, and full history aggregation is not
optimized for large datasets. Active ratio is an input-recency estimate, not
productivity. The safe install/uninstall workflow is documented in the README. The Bash safety tests pass without installing a service; actual
`launchd` bootstrap, login/logout, Accessibility grant, and uninstall are not
exercised outside a user-approved macOS session.

## Still deferred: remaining V2/V3

File/terminal/Git activity ingestion, PDFs/media/calendar ingestion, AI extraction/
discovery/summaries/queries, semantic search, and optional 3D remain unimplemented.

## Assessment

The core V1 workflow, the original audit fixes, and the requested V1 search/polish
capabilities are implemented and tested. Production hardening and future V2/V3
capabilities remain separate work. See [README](README.md) for setup, APIs,
verification commands, and backup/history caveats.
