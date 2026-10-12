# Website-aware automatic connections

## Acceptance criteria

- Shared backend normalization accepts HTTP(S) only, removes userinfo/query/fragment, canonicalizes host/default ports/root paths and known browser identities, and enforces length/privacy/exclusion rules for native samples, extension hints, retained reads, and graph projections.
- Stable app/domain/page/window identities, bounded explained relationships, conservative title/tag/entity/domain matches, no generic/weak proximity matches.
- One canonical unordered edge per pair; explicit > activity > automatic > ai. Replacements preserve edge IDs/positions and restore lower-priority evidence when the winner disappears.
- Retention and exclusions remove stale activity projections, including while paused; clear preserves Markdown notes and note links.
- GET /api/activity/{recent,domains,pages,nodes/:id} provides sanitized retained data, server-bounded pagination, stable sorting, filters, and relationship explanations. Existing endpoints remain compatible.
- Dashboard exposes Overview/Websites/Connections, website lists/search/sort/pagination, privacy settings, and detail navigation. Inspector exposes safe URLs, browser, first/last seen, durations, sessions, related nodes/notes and reasons. Graph can hide activity edges/browser nodes or scope to domains/pages/activity-connected notes, with labeled source styles.
- Extension suppresses private/unfocused/internal/excluded contexts, coalesces duplicate events, refreshes hints periodically, backs off unavailable backends, and never logs pairing tokens.
- Backend race/vet, frontend typecheck/build, extension and isolated Playwright suites pass. Local preview uses temporary data with native recording disabled; real browsing/permissions are not enabled for testing.

## Constraints and resources

- Keep existing Go/SQLite + Next.js/React architecture; no third-party service or chart dependency.
- `creating-implementation-plan/SKILL.md` is not installed in searched account skill locations. No `.github/modernize/**/*` or constitution files were found.
- Preserve the pre-existing formatting change in ActivityInspector.tsx. Do not touch the running user backend on port 8080 or real activity data.
- Native private-window detection remains best effort; document platform/browser limitations rather than promising a guarantee the OS API cannot provide.

## Validation

Use deterministic sessions and temporary SQLite/vault fixtures for Wikipedia, GeeksforGeeks, GitHub, Go documentation, excluded/private contexts, pauses, gaps, retention, edge conflicts, and graph position/idempotency. Exercise website endpoints and user-facing browser navigation/filters/clear on isolated servers. Report any unavailable browser/permission-dependent checks explicitly.
